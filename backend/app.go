package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
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
	// linkMisses limits failed client-link lookups per IP to resist token guessing.
	linkMisses *httprate.RateLimiter
}

func newSessionManager(db *sql.DB, secure bool) *scs.SessionManager {
	sm := scs.New()
	sm.Store = sqlite3store.New(db)
	sm.Lifetime = 14 * 24 * time.Hour
	sm.Cookie.Name = "session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = secure
	sm.Cookie.SameSite = http.SameSiteLaxMode
	return sm
}

func (a *app) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, logRequests, middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/settings", a.getSettings)

		// Studio API: session cookie auth.
		r.Group(func(r chi.Router) {
			r.Use(a.sessions.LoadAndSave)
			authLimit := httprate.Limit(10, time.Minute, httprate.WithKeyFuncs(keyByClientIP))
			r.Get("/register", a.registrationStatus)
			r.With(authLimit).Post("/register", a.register)
			r.With(authLimit).Post("/login", a.login)
			r.Post("/logout", a.logout)

			r.Group(func(r chi.Router) {
				r.Use(a.requireUser)
				r.Get("/me", a.me)
				r.With(authLimit).Post("/password", a.changePassword)

				r.Group(func(r chi.Router) {
					r.Use(requireAdmin)
					r.Patch("/settings", a.updateSettings)
					r.Get("/users", a.listUsers)
					r.Post("/users", a.createUser)
					r.Delete("/users/{userID}", a.deleteUser)
				})

				r.Get("/galleries", a.listGalleries)
				r.Post("/galleries", a.createGallery)
				r.Route("/galleries/{galleryID}", func(r chi.Router) {
					r.Use(a.loadOwnedGallery)
					r.Get("/", a.getGallery)
					r.Patch("/", a.updateGallery)
					r.Delete("/", a.deleteGallery)
					r.Patch("/cover", a.setCover)
					r.Post("/restart-download-period", a.restartPeriod)
					r.Post("/rotate-link", a.rotateLink)
					r.Post("/photos", a.uploadPhotos)
					r.Delete("/photos", a.deletePhotos)
					r.Patch("/photos/order", a.reorderPhotos)
					r.Get("/photos/{photoID}/thumb", a.serveThumb)
					r.Get("/photos/{photoID}/web", a.serveWeb)
					r.Get("/photos/{photoID}/original", a.serveOriginal)
				})
			})
		})

		// Client API: the secret token in the shared link is the only credential.
		// Originals are reachable only through the expiry-checked download routes.
		r.Route("/g/{slug}", func(r chi.Router) {
			r.Use(a.loadSharedGallery)
			r.Get("/", a.clientGallery)
			r.Get("/download.zip", a.clientDownloadAll)
			r.Get("/photos/{photoID}/thumb", a.serveThumb)
			r.Get("/photos/{photoID}/web", a.serveWeb)
			r.Get("/photos/{photoID}/download", a.clientDownloadPhoto)
		})
	})

	// Rejects cross-origin state-changing browser requests (CSRF), using
	// Sec-Fetch-Site / Origin headers.
	return http.NewCrossOriginProtection().Handler(r)
}

// logRequests logs each request with client-link tokens redacted: the token
// is the gallery's credential and must not end up in logs.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		log.Printf("%s %s %d %dB %s", r.Method, redactPath(r.URL.Path), ww.Status(), ww.BytesWritten(), time.Since(start).Round(time.Millisecond))
	})
}

func redactPath(p string) string {
	const prefix = "/api/g/"
	if !strings.HasPrefix(p, prefix) {
		return p
	}
	rest := p[len(prefix):]
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return prefix + "***" + rest[i:]
	}
	return prefix + "***"
}

// keyByClientIP rate-limits by client IP. It uses the TCP peer address,
// except when the peer is loopback: then the request came through the local
// reverse proxy, which must set X-Real-IP to the client's address and
// overwrite any value the client sent. X-Forwarded-For is never trusted,
// because clients can append to it.
func keyByClientIP(r *http.Request) (string, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr, nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if real := net.ParseIP(r.Header.Get("X-Real-IP")); real != nil {
			return real.String(), nil
		}
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
