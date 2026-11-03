package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gosimple/slug"
)

const dateLayout = "2006-01-02"

type gallery struct {
	ID           string
	OwnerID      string
	Name         string
	ShotDate     string
	SharedOn     string
	Months       int
	Slug         string
	CoverPhotoID sql.NullString
}

// expiresOn is the first day downloads are closed. ok is false when there is no limit.
func (g *gallery) expiresOn() (day time.Time, ok bool) {
	if g.Months == 0 {
		return time.Time{}, false
	}
	start, err := time.ParseInLocation(dateLayout, g.SharedOn, time.Local)
	if err != nil {
		// shared_on is always written by this server; treat corruption as expired.
		return time.Time{}, true
	}
	return start.AddDate(0, g.Months, 0), true
}

func (g *gallery) downloadsOpen(now time.Time) bool {
	exp, ok := g.expiresOn()
	return !ok || now.Before(exp)
}

const galleryCols = `id, owner_id, name, shot_date, shared_on, months, slug, cover_photo_id`

func scanGallery(row interface{ Scan(...any) error }) (gallery, error) {
	var g gallery
	err := row.Scan(&g.ID, &g.OwnerID, &g.Name, &g.ShotDate, &g.SharedOn, &g.Months, &g.Slug, &g.CoverPhotoID)
	if errors.Is(err, sql.ErrNoRows) {
		return g, errNotFound
	}
	return g, err
}

func (a *app) saveGallery(ctx context.Context, g gallery) error {
	_, err := a.db.ExecContext(ctx, `
		UPDATE galleries SET name = ?, shot_date = ?, shared_on = ?, months = ?, slug = ?, cover_photo_id = ?
		WHERE id = ?`,
		g.Name, g.ShotDate, g.SharedOn, g.Months, g.Slug, g.CoverPhotoID, g.ID)
	return err
}

// loadOwnedGallery puts the {galleryID} gallery in the context if the signed-in user owns it.
func (a *app) loadOwnedGallery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, err := scanGallery(a.db.QueryRowContext(r.Context(),
			`SELECT `+galleryCols+` FROM galleries WHERE id = ? AND owner_id = ?`,
			chi.URLParam(r, "galleryID"), userID(r)))
		if errors.Is(err, errNotFound) {
			writeError(w, http.StatusNotFound, "gallery not found")
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxGallery, &g)))
	})
}

func galleryFrom(r *http.Request) *gallery {
	return r.Context().Value(ctxGallery).(*gallery)
}

// ---- JSON shapes ----

type photoOut struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ThumbURL    string `json:"thumbUrl"`              // ~800px, for grids
	WebURL      string `json:"webUrl"`                // ~2048px, for the lightbox
	OriginalURL string `json:"originalUrl,omitempty"` // studio only
	DownloadURL string `json:"downloadUrl,omitempty"` // client only, while downloads are open
}

type galleryOut struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	ShotDate      string     `json:"shotDate"`
	SharedOn      string     `json:"sharedOn"`
	Months        int        `json:"months"`
	ExpiresOn     string     `json:"expiresOn,omitempty"`
	DownloadsOpen bool       `json:"downloadsOpen"`
	Slug          string     `json:"slug"`
	CoverPhotoID  string     `json:"coverPhotoId,omitempty"`
	CoverThumbURL string     `json:"coverThumbUrl,omitempty"`
	PhotoCount    int        `json:"photoCount"`
	Photos        []photoOut `json:"photos,omitzero"`
}

func studioPhotoBase(galleryID string) string { return "/api/galleries/" + galleryID + "/photos/" }

func toGalleryOut(g *gallery, coverID string, count int, now time.Time) galleryOut {
	out := galleryOut{
		ID: g.ID, Name: g.Name, ShotDate: g.ShotDate, SharedOn: g.SharedOn, Months: g.Months,
		DownloadsOpen: g.downloadsOpen(now), Slug: g.Slug, CoverPhotoID: coverID, PhotoCount: count,
	}
	if exp, ok := g.expiresOn(); ok {
		out.ExpiresOn = exp.Format(dateLayout)
	}
	if coverID != "" {
		out.CoverThumbURL = studioPhotoBase(g.ID) + coverID + "/thumb"
	}
	return out
}

// effectiveCover is the chosen cover, or the first photo when none is chosen.
func effectiveCover(g *gallery, photos []photo) string {
	if g.CoverPhotoID.Valid {
		return g.CoverPhotoID.String
	}
	if len(photos) > 0 {
		return photos[0].ID
	}
	return ""
}

func (a *app) writeStudioGallery(w http.ResponseWriter, r *http.Request, status int, g *gallery) {
	photos, err := a.galleryPhotos(r.Context(), g.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	out := toGalleryOut(g, effectiveCover(g, photos), len(photos), time.Now())
	out.Photos = make([]photoOut, 0, len(photos))
	for _, p := range photos {
		out.Photos = append(out.Photos, p.studioOut(g.ID))
	}
	writeJSON(w, status, out)
}

// ---- input validation ----

func cleanName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("Give the gallery a name.")
	}
	if utf8.RuneCountInString(s) > 200 {
		return "", errors.New("Gallery name must be at most 200 characters.")
	}
	return s, nil
}

func checkDate(s string) error {
	if s == "" {
		return nil
	}
	if _, err := time.Parse(dateLayout, s); err != nil {
		return errors.New("Photoshoot date must be YYYY-MM-DD.")
	}
	return nil
}

func checkMonths(m int) error {
	if m < 0 || m > 120 {
		return errors.New("Download period must be between 0 (no limit) and 120 months.")
	}
	return nil
}

// newSlug returns a readable, unguessable link token, e.g. "ana-ribeiro-<26 chars>".
// The random part carries 128 bits; the link is the client's only credential.
func newSlug(name string) string {
	base := slug.Make(name)
	if len(base) > 40 {
		base = strings.TrimRight(base[:40], "-")
	}
	if base == "" {
		base = "gallery"
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error
	return base + "-" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}

func today() string { return time.Now().Format(dateLayout) }

// ---- handlers ----

func (a *app) listGalleries(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `
		SELECT `+galleryCols+`,
			(SELECT id FROM photos p WHERE p.gallery_id = g.id ORDER BY position LIMIT 1),
			(SELECT COUNT(*) FROM photos p WHERE p.gallery_id = g.id)
		FROM galleries g WHERE owner_id = ? ORDER BY g.rowid DESC`, userID(r))
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()

	now := time.Now()
	out := []galleryOut{}
	for rows.Next() {
		var g gallery
		var first sql.NullString
		var count int
		if err := rows.Scan(&g.ID, &g.OwnerID, &g.Name, &g.ShotDate, &g.SharedOn, &g.Months, &g.Slug,
			&g.CoverPhotoID, &first, &count); err != nil {
			serverError(w, err)
			return
		}
		cover := first.String
		if g.CoverPhotoID.Valid {
			cover = g.CoverPhotoID.String
		}
		out = append(out, toGalleryOut(&g, cover, count, now))
	}
	if err := rows.Err(); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) createGallery(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		ShotDate string `json:"shotDate"`
		Months   int    `json:"months"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	name, err := cleanName(in.Name)
	if err == nil {
		err = checkDate(in.ShotDate)
	}
	if err == nil {
		err = checkMonths(in.Months)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	g := gallery{
		ID: uuid.NewString(), OwnerID: userID(r), Name: name, ShotDate: in.ShotDate,
		SharedOn: today(), Months: in.Months, Slug: newSlug(name),
	}
	_, err = a.db.ExecContext(r.Context(), `
		INSERT INTO galleries (`+galleryCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`,
		g.ID, g.OwnerID, g.Name, g.ShotDate, g.SharedOn, g.Months, g.Slug)
	if err != nil {
		serverError(w, err)
		return
	}
	a.writeStudioGallery(w, r, http.StatusCreated, &g)
}

func (a *app) getGallery(w http.ResponseWriter, r *http.Request) {
	a.writeStudioGallery(w, r, http.StatusOK, galleryFrom(r))
}

func (a *app) updateGallery(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     *string `json:"name"`
		ShotDate *string `json:"shotDate"`
		Months   *int    `json:"months"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	g := *galleryFrom(r)
	if in.Name != nil {
		name, err := cleanName(*in.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		g.Name = name
	}
	if in.ShotDate != nil {
		if err := checkDate(*in.ShotDate); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		g.ShotDate = *in.ShotDate
	}
	if in.Months != nil {
		if err := checkMonths(*in.Months); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		g.Months = *in.Months
	}
	if err := a.saveGallery(r.Context(), g); err != nil {
		serverError(w, err)
		return
	}
	a.writeStudioGallery(w, r, http.StatusOK, &g)
}

func (a *app) setCover(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PhotoID string `json:"photoId"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	g := *galleryFrom(r)
	if _, err := a.photoByID(r.Context(), g.ID, in.PhotoID); errors.Is(err, errNotFound) {
		writeError(w, http.StatusBadRequest, "cover photo is not in this gallery")
		return
	} else if err != nil {
		serverError(w, err)
		return
	}
	g.CoverPhotoID = sql.NullString{String: in.PhotoID, Valid: true}
	if err := a.saveGallery(r.Context(), g); err != nil {
		serverError(w, err)
		return
	}
	a.writeStudioGallery(w, r, http.StatusOK, &g)
}

// restartPeriod starts the download period again from today.
func (a *app) restartPeriod(w http.ResponseWriter, r *http.Request) {
	g := *galleryFrom(r)
	g.SharedOn = today()
	if err := a.saveGallery(r.Context(), g); err != nil {
		serverError(w, err)
		return
	}
	a.writeStudioGallery(w, r, http.StatusOK, &g)
}

// rotateLink replaces the client link; the old one stops working at once.
func (a *app) rotateLink(w http.ResponseWriter, r *http.Request) {
	g := *galleryFrom(r)
	g.Slug = newSlug(g.Name)
	if err := a.saveGallery(r.Context(), g); err != nil {
		serverError(w, err)
		return
	}
	a.writeStudioGallery(w, r, http.StatusOK, &g)
}

func (a *app) deleteGallery(w http.ResponseWriter, r *http.Request) {
	g := galleryFrom(r)
	// Photos rows cascade. Files go after the commit so a failed delete keeps them.
	if _, err := a.db.ExecContext(r.Context(), `DELETE FROM galleries WHERE id = ?`, g.ID); err != nil {
		serverError(w, err)
		return
	}
	if err := os.RemoveAll(a.galleryDir(g.ID)); err != nil {
		log.Printf("delete gallery %s files: %v", g.ID, err)
	}
	w.WriteHeader(http.StatusNoContent)
}
