package main

import (
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Studio branding, shown on the login page, studio header and client pages.

type settings struct {
	StudioName  string `json:"studioName"`
	AccentColor string `json:"accentColor"`
}

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func (a *app) loadSettings(r *http.Request) (settings, error) {
	var s settings
	err := a.db.QueryRowContext(r.Context(),
		`SELECT studio_name, accent_color FROM settings WHERE id = 1`).Scan(&s.StudioName, &s.AccentColor)
	return s, err
}

// getSettings is public: the login and client pages need the branding.
func (a *app) getSettings(w http.ResponseWriter, r *http.Request) {
	s, err := a.loadSettings(r)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (a *app) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		StudioName  *string `json:"studioName"`
		AccentColor *string `json:"accentColor"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	s, err := a.loadSettings(r)
	if err != nil {
		serverError(w, err)
		return
	}
	if in.StudioName != nil {
		s.StudioName = truncateRunes(strings.TrimSpace(*in.StudioName), 100)
	}
	if in.AccentColor != nil {
		if !hexColor.MatchString(*in.AccentColor) {
			writeError(w, http.StatusBadRequest, "Accent color must look like #1C1B19.")
			return
		}
		s.AccentColor = strings.ToUpper(*in.AccentColor)
	}
	if _, err := a.db.ExecContext(r.Context(),
		`UPDATE settings SET studio_name = ?, accent_color = ? WHERE id = 1`, s.StudioName, s.AccentColor); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func truncateRunes(s string, n int) string {
	for utf8.RuneCountInString(s) > n {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}
