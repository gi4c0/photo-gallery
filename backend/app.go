package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
)

type app struct {
	db        *sql.DB
	photosDir string
	sessions  *scs.SessionManager
}

func newSessionManager(db *sql.DB, secure bool) *scs.SessionManager {
	sm := scs.New()
	sm.Store = sqlite3store.New(db)
	sm.Lifetime = 14 * 24 * time.Hour
	sm.Cookie.Name = "session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = secure
	// Lax keeps the cookie off cross-site POST/PATCH/DELETE requests (CSRF protection).
	sm.Cookie.SameSite = http.SameSiteLaxMode
	return sm
}

func (a *app) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Logger, middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		// Studio (photographer) API: session cookie auth.
		r.Group(func(r chi.Router) {
			r.Use(a.sessions.LoadAndSave)
			r.With(httprate.Limit(10, time.Minute, httprate.WithKeyFuncs(keyByRemoteAddr))).
				Post("/auth/login", a.login)
			r.Post("/auth/logout", a.logout)

			r.Group(func(r chi.Router) {
				r.Use(a.requireUser)
				r.Get("/auth/me", a.me)
				r.Get("/galleries", a.listGalleries)
				r.Post("/galleries", a.createGallery)
				r.Route("/galleries/{galleryID}", func(r chi.Router) {
					r.Use(a.loadOwnedGallery)
					r.Get("/", a.getGallery)
					r.Patch("/", a.updateGallery)
					r.Delete("/", a.deleteGallery)
					r.Post("/restart-period", a.restartPeriod)
					r.Post("/rotate-link", a.rotateLink)
					r.Post("/photos", a.uploadPhotos)
					r.Put("/photos/order", a.reorderPhotos)
					r.Post("/photos/delete", a.deletePhotos)
					r.Get("/photos/{photoID}/thumb", a.serveThumb)
					r.Get("/photos/{photoID}/original", a.serveOriginal)
				})
			})
		})

		// Client API: the secret slug in the shared link is the only credential.
		r.Route("/g/{slug}", func(r chi.Router) {
			r.Use(a.loadSharedGallery)
			r.Get("/", a.clientGallery)
			r.Get("/download.zip", a.clientDownloadAll)
			r.Get("/photos/{photoID}/thumb", a.serveThumb)
			r.Get("/photos/{photoID}/original", a.serveOriginal)
			r.Get("/photos/{photoID}/download", a.clientDownloadPhoto)
		})
	})
	return r
}

// keyByRemoteAddr rate-limits by the TCP peer address. It ignores
// X-Forwarded-For, which any client can forge. Behind a reverse proxy all
// clients share the proxy's address.
func keyByRemoteAddr(r *http.Request) (string, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr, nil
	}
	return host, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

// decodeJSON reads a small JSON body into dst. It writes the error response
// and returns false when the body is invalid.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
		}
		return false
	}
	return true
}
