package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gosimple/slug"
)

// loadSharedGallery resolves the secret {slug} of a client link.
func (a *app) loadSharedGallery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, err := scanGallery(a.db.QueryRowContext(r.Context(),
			`SELECT `+galleryCols+` FROM galleries WHERE slug = ?`, chi.URLParam(r, "slug")))
		if errors.Is(err, errNotFound) {
			// Only misses count toward the limit, so real clients loading many photos are unaffected.
			key, _ := keyByClientIP(r)
			if a.linkMisses.OnLimit(w, r, key) {
				writeError(w, http.StatusTooManyRequests, "too many requests")
				return
			}
			writeError(w, http.StatusNotFound, "This link is no longer active.")
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
		// Shared links must not leak through the Referer header or search engines.
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Robots-Tag", "noindex")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxGallery, &g)))
	})
}

func (a *app) clientGallery(w http.ResponseWriter, r *http.Request) {
	g := galleryFrom(r)
	photos, err := a.galleryPhotos(r.Context(), g.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	open := g.downloadsOpen(time.Now())
	prefix := "/api/g/" + g.Slug
	out := struct {
		Name           string     `json:"name"`
		ShotDate       string     `json:"shotDate"`
		CoverPhotoID   string     `json:"coverPhotoId,omitempty"`
		DownloadsOpen  bool       `json:"downloadsOpen"`
		ExpiresOn      string     `json:"expiresOn,omitempty"`
		DownloadAllURL string     `json:"downloadAllUrl,omitempty"`
		Photos         []photoOut `json:"photos"`
	}{
		Name: g.Name, ShotDate: g.ShotDate, CoverPhotoID: effectiveCover(g, photos),
		DownloadsOpen: open, Photos: make([]photoOut, 0, len(photos)),
	}
	if exp, ok := g.expiresOn(); ok {
		out.ExpiresOn = exp.Format(dateLayout)
	}
	if open && len(photos) > 0 {
		out.DownloadAllURL = prefix + "/download.zip"
	}
	for _, p := range photos {
		out.Photos = append(out.Photos, p.clientOut(g.Slug, open))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) requireDownloadsOpen(w http.ResponseWriter, g *gallery) bool {
	if g.downloadsOpen(time.Now()) {
		return true
	}
	writeError(w, http.StatusForbidden, "Downloads for this gallery have closed. Contact your photographer to reopen downloads.")
	return false
}

func (a *app) clientDownloadPhoto(w http.ResponseWriter, r *http.Request) {
	if !a.requireDownloadsOpen(w, galleryFrom(r)) {
		return
	}
	p, ok := a.photoFromURL(w, r)
	if !ok {
		return
	}
	if startsDownload(r) {
		a.recordDownload(r.Context(), `UPDATE photos SET downloads = downloads + 1, last_downloaded_at = `+nowUTC+` WHERE id = ?`, p.ID)
	}
	a.serveFile(w, r, a.originalPath(p), p.ContentType, attachment(p.Name))
}

// startsDownload reports whether r begins a download. Range requests that
// resume one from a later offset return false, so a resumed download counts once.
func startsDownload(r *http.Request) bool {
	rg := r.Header.Get("Range")
	return r.Method == http.MethodGet && (rg == "" || strings.HasPrefix(rg, "bytes=0-"))
}

// recordDownload runs a statistics update. A failure is logged and never blocks the download.
func (a *app) recordDownload(ctx context.Context, query string, id string) {
	if _, err := a.db.ExecContext(ctx, query, id); err != nil {
		log.Printf("record download: %v", err)
	}
}

// clientDownloadAll streams every original as one ZIP, in gallery order.
func (a *app) clientDownloadAll(w http.ResponseWriter, r *http.Request) {
	g := galleryFrom(r)
	if !a.requireDownloadsOpen(w, g) {
		return
	}
	photos, err := a.galleryPhotos(r.Context(), g.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	if len(photos) == 0 {
		writeError(w, http.StatusNotFound, "gallery has no photos")
		return
	}
	a.recordDownload(r.Context(), `UPDATE galleries SET zip_downloads = zip_downloads + 1, zip_last_downloaded_at = `+nowUTC+` WHERE id = ?`, g.ID)

	zipName := slug.Make(g.Name)
	if zipName == "" {
		zipName = "gallery"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", attachment(zipName+".zip"))

	zw := zip.NewWriter(w)
	names := uniqueNames(photos)
	for i, p := range photos {
		if err := a.addToZip(zw, a.originalPath(p), names[i]); err != nil {
			// Headers are sent; the client sees a truncated ZIP.
			log.Printf("zip gallery %s: %v", g.ID, err)
			return
		}
	}
	if err := zw.Close(); err != nil {
		log.Printf("zip gallery %s: %v", g.ID, err)
	}
}

func (a *app) addToZip(zw *zip.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	// Photos are already compressed; Store avoids wasting CPU.
	dst, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: st.ModTime()})
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, f)
	return err
}

// uniqueNames returns ZIP entry names, adding " (2)", " (3)"... to duplicates.
func uniqueNames(photos []photo) []string {
	used := make(map[string]bool, len(photos))
	out := make([]string, len(photos))
	for i, p := range photos {
		name := p.Name
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		for n := 2; used[strings.ToLower(name)]; n++ {
			name = fmt.Sprintf("%s (%d)%s", stem, n, ext)
		}
		used[strings.ToLower(name)] = true
		out[i] = name
	}
	return out
}
