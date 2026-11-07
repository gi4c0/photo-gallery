package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/heic"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	maxFileBytes   = 95 << 20 // per photo; stays under the 100 MB request cap of Cloudflare's free plan
	maxUploadBytes = 4 << 30  // per upload request
	maxPixels      = 120e6    // guards against decompression bombs
	thumbSidePx    = 800
	webSidePx      = 2048
	jpegQuality    = 82
)

type photo struct {
	ID          string
	GalleryID   string
	Name        string
	Ext         string
	ContentType string
	Size        int64
	Width       int
	Height      int
}

func (p photo) studioOut(galleryID string) photoOut {
	base := studioPhotoBase(galleryID) + p.ID
	return photoOut{
		ID: p.ID, Name: p.Name, Width: p.Width, Height: p.Height,
		ThumbURL: base + "/thumb", WebURL: base + "/web", OriginalURL: base + "/original",
	}
}

func (p photo) clientOut(slug string, downloadsOpen bool) photoOut {
	base := "/api/g/" + slug + "/photos/" + p.ID
	o := photoOut{
		ID: p.ID, Name: p.Name, Width: p.Width, Height: p.Height,
		ThumbURL: base + "/thumb", WebURL: base + "/web",
	}
	if downloadsOpen {
		o.DownloadURL = base + "/download"
	}
	return o
}

const photoCols = `id, gallery_id, name, ext, content_type, size, width, height`

func scanPhoto(row interface{ Scan(...any) error }) (photo, error) {
	var p photo
	err := row.Scan(&p.ID, &p.GalleryID, &p.Name, &p.Ext, &p.ContentType, &p.Size, &p.Width, &p.Height)
	if errors.Is(err, sql.ErrNoRows) {
		return p, errNotFound
	}
	return p, err
}

func (a *app) galleryPhotos(ctx context.Context, galleryID string) ([]photo, error) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT `+photoCols+` FROM photos WHERE gallery_id = ? ORDER BY position`, galleryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []photo
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (a *app) photoByID(ctx context.Context, galleryID, photoID string) (photo, error) {
	return scanPhoto(a.db.QueryRowContext(ctx,
		`SELECT `+photoCols+` FROM photos WHERE gallery_id = ? AND id = ?`, galleryID, photoID))
}

// ---- file layout under PHOTOS_DIR/<galleryID>/:
// <photoID><ext> (original, untouched), <photoID>_thumb.jpg, <photoID>_web.jpg ----

func (a *app) galleryDir(galleryID string) string { return filepath.Join(a.photosDir, galleryID) }

func (a *app) originalPath(p photo) string {
	return filepath.Join(a.galleryDir(p.GalleryID), p.ID+p.Ext)
}

func (a *app) thumbPath(p photo) string {
	return filepath.Join(a.galleryDir(p.GalleryID), p.ID+"_thumb.jpg")
}

func (a *app) webPath(p photo) string {
	return filepath.Join(a.galleryDir(p.GalleryID), p.ID+"_web.jpg")
}

func (a *app) removePhotoFiles(p photo) {
	for _, path := range []string{a.originalPath(p), a.thumbPath(p), a.webPath(p)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("remove %s: %v", path, err)
		}
	}
}

// sniffImage identifies the format from the file's first bytes, ignoring the
// client's claimed type and extension.
func sniffImage(head []byte) (contentType, ext string, ok bool) {
	switch http.DetectContentType(head) {
	case "image/jpeg":
		return "image/jpeg", ".jpg", true
	case "image/png":
		return "image/png", ".png", true
	}
	// HEIC/HEIF: ISO-BMFF "ftyp" box with a HEIF brand.
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		switch string(head[8:12]) {
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1":
			return "image/heic", ".heic", true
		}
	}
	return "", "", false
}

// decodeImage decodes an upload, applying EXIF/HEIF rotation.
func decodeImage(path, contentType string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var cfg image.Config
	if contentType == "image/heic" {
		cfg, err = heic.DecodeConfig(f)
	} else {
		cfg, _, err = image.DecodeConfig(f)
	}
	if err != nil {
		return nil, err
	}
	if float64(cfg.Width)*float64(cfg.Height) > maxPixels {
		return nil, errors.New("image dimensions too large")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if contentType == "image/heic" {
		return heic.Decode(f)
	}
	return imaging.Decode(f, imaging.AutoOrientation(true))
}

// makeDerivatives writes the thumbnail and web-size JPEGs and returns the
// display size of the original. HEIC originals thus get browser-friendly versions.
func (a *app) makeDerivatives(p photo) (w, h int, err error) {
	img, err := decodeImage(a.originalPath(p), p.ContentType)
	if err != nil {
		return 0, 0, err
	}
	for _, d := range []struct {
		path string
		side int
	}{{a.thumbPath(p), thumbSidePx}, {a.webPath(p), webSidePx}} {
		// Fit never enlarges; small images are saved at their own size.
		if err := imaging.Save(imaging.Fit(img, d.side, d.side, imaging.Linear), d.path, imaging.JPEGQuality(jpegQuality)); err != nil {
			return 0, 0, err
		}
	}
	b := img.Bounds()
	return b.Dx(), b.Dy(), nil
}

func cleanFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, name)
	name = truncateRunes(name, 200)
	if name == "" || name == "." || name == "/" {
		name = "photo"
	}
	return name
}

type rejectedFile struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// errReject marks a problem with one file; the rest of the upload continues.
type errReject struct {
	msg string
}

func (e errReject) Error() string { return e.msg }

// uploadPhotos accepts multipart/form-data with one or more "files" parts.
// It streams each file to disk; valid files are added even if others are rejected.
// Clients should send one file per request: that gives per-file progress, and
// after a network failure only the failed files need resending.
func (a *app) uploadPhotos(w http.ResponseWriter, r *http.Request) {
	g := galleryFrom(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "expected multipart/form-data with \"files\" parts")
		return
	}
	if err := os.MkdirAll(a.galleryDir(g.ID), 0o750); err != nil {
		serverError(w, err)
		return
	}

	added := []photoOut{}
	rejected := []rejectedFile{}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "upload too large")
			} else {
				writeError(w, http.StatusBadRequest, "malformed multipart body")
			}
			return
		}
		if part.FormName() != "files" || part.FileName() == "" {
			part.Close()
			continue
		}
		name := cleanFileName(part.FileName())
		p, err := a.savePhoto(r.Context(), g.ID, name, part)
		part.Close()
		var rej errReject
		switch {
		case errors.As(err, &rej):
			rejected = append(rejected, rejectedFile{Name: name, Error: rej.msg})
		case err != nil:
			serverError(w, err)
			return
		default:
			added = append(added, p.studioOut(g.ID))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"photos": added, "rejected": rejected})
}

func (a *app) savePhoto(ctx context.Context, galleryID, name string, src io.Reader) (photo, error) {
	head := make([]byte, 512)
	n, err := io.ReadFull(src, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return photo{}, err
	}
	head = head[:n]
	ct, ext, ok := sniffImage(head)
	if !ok {
		return photo{}, errReject{"Only JPG, PNG or HEIC images can be uploaded."}
	}

	p := photo{ID: uuid.NewString(), GalleryID: galleryID, Name: name, Ext: ext, ContentType: ct}
	tmp, err := os.CreateTemp(a.galleryDir(galleryID), ".upload-*")
	if err != nil {
		return photo{}, err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	p.Size, err = io.Copy(tmp, io.MultiReader(bytes.NewReader(head), io.LimitReader(src, maxFileBytes+1-int64(n))))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return photo{}, err
	}
	if p.Size > maxFileBytes {
		return photo{}, errReject{fmt.Sprintf("File is larger than %d MB.", maxFileBytes>>20)}
	}
	if err := os.Rename(tmp.Name(), a.originalPath(p)); err != nil {
		return photo{}, err
	}

	p.Width, p.Height, err = a.makeDerivatives(p)
	if err != nil {
		a.removePhotoFiles(p)
		return photo{}, errReject{"The image could not be read: " + err.Error()}
	}

	err = withTx(ctx, a.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO photos (`+photoCols+`, position)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?,
				(SELECT COALESCE(MAX(position), 0) + 1 FROM photos WHERE gallery_id = ?))`,
			p.ID, p.GalleryID, p.Name, p.Ext, p.ContentType, p.Size, p.Width, p.Height, galleryID); err != nil {
			return err
		}
		// The first uploaded photo becomes the default cover.
		_, err := tx.ExecContext(ctx,
			`UPDATE galleries SET cover_photo_id = ? WHERE id = ? AND cover_photo_id IS NULL`, p.ID, galleryID)
		return err
	})
	if err != nil {
		a.removePhotoFiles(p)
		return photo{}, err
	}
	return p, nil
}

// reorderPhotos sets the order; photoIds must list every photo exactly once.
func (a *app) reorderPhotos(w http.ResponseWriter, r *http.Request) {
	g := galleryFrom(r)
	var in struct {
		PhotoIDs []string `json:"photoIds"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	errMismatch := errors.New("mismatch")
	err := withTx(r.Context(), a.db, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM photos WHERE gallery_id = ?`, g.ID).Scan(&count); err != nil {
			return err
		}
		if count != len(in.PhotoIDs) {
			return errMismatch
		}
		seen := make(map[string]bool, len(in.PhotoIDs))
		for i, id := range in.PhotoIDs {
			if seen[id] {
				return errMismatch
			}
			seen[id] = true
			res, err := tx.ExecContext(r.Context(),
				`UPDATE photos SET position = ? WHERE gallery_id = ? AND id = ?`, i+1, g.ID, id)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n != 1 {
				return errMismatch
			}
		}
		return nil
	})
	if errors.Is(err, errMismatch) {
		writeError(w, http.StatusBadRequest, "photoIds must list every photo in the gallery exactly once")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	a.writeStudioGallery(w, r, http.StatusOK, g)
}

// deletePhotos removes the listed photos (DELETE with a JSON body). Unknown IDs are ignored.
func (a *app) deletePhotos(w http.ResponseWriter, r *http.Request) {
	g := galleryFrom(r)
	var in struct {
		PhotoIDs []string `json:"photoIds"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	var deleted []photo
	err := withTx(r.Context(), a.db, func(tx *sql.Tx) error {
		for _, id := range in.PhotoIDs {
			p, err := scanPhoto(tx.QueryRowContext(r.Context(),
				`DELETE FROM photos WHERE gallery_id = ? AND id = ? RETURNING `+photoCols, g.ID, id))
			if errors.Is(err, errNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			deleted = append(deleted, p)
		}
		// Deleting the cover (ON DELETE SET NULL) falls back to the first remaining photo.
		_, err := tx.ExecContext(r.Context(), `
			UPDATE galleries SET cover_photo_id =
				(SELECT id FROM photos WHERE gallery_id = ? ORDER BY position LIMIT 1)
			WHERE id = ? AND cover_photo_id IS NULL`, g.ID, g.ID)
		return err
	})
	if err != nil {
		serverError(w, err)
		return
	}
	for _, p := range deleted {
		a.removePhotoFiles(p)
	}
	// Reload to reflect the cover change.
	g2, err := scanGallery(a.db.QueryRowContext(r.Context(),
		`SELECT `+galleryCols+` FROM galleries WHERE id = ?`, g.ID))
	if err != nil {
		serverError(w, err)
		return
	}
	a.writeStudioGallery(w, r, http.StatusOK, &g2)
}

// ---- serving files (shared by studio and client routes; gallery comes from context) ----

func (a *app) photoFromURL(w http.ResponseWriter, r *http.Request) (photo, bool) {
	p, err := a.photoByID(r.Context(), galleryFrom(r).ID, chi.URLParam(r, "photoID"))
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "photo not found")
		return p, false
	}
	if err != nil {
		serverError(w, err)
		return p, false
	}
	return p, true
}

func (a *app) serveFile(w http.ResponseWriter, r *http.Request, path, contentType, disposition string) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "file missing")
			return
		}
		serverError(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if disposition != "" {
		w.Header().Set("Content-Disposition", disposition)
	}
	http.ServeContent(w, r, "", st.ModTime(), f)
}

func (a *app) serveThumb(w http.ResponseWriter, r *http.Request) {
	p, ok := a.photoFromURL(w, r)
	if !ok {
		return
	}
	a.serveFile(w, r, a.thumbPath(p), "image/jpeg", "")
}

func (a *app) serveWeb(w http.ResponseWriter, r *http.Request) {
	p, ok := a.photoFromURL(w, r)
	if !ok {
		return
	}
	a.serveFile(w, r, a.webPath(p), "image/jpeg", "")
}

func (a *app) serveOriginal(w http.ResponseWriter, r *http.Request) {
	p, ok := a.photoFromURL(w, r)
	if !ok {
		return
	}
	a.serveFile(w, r, a.originalPath(p), p.ContentType, "")
}

func attachment(filename string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": filename})
}
