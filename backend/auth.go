package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxGallery
)

const sessionUserKey = "userID"

type user struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	IsAdmin bool   `json:"isAdmin"`
}

// dummyHash is compared against when the email is unknown, so a failed login
// takes the same time whether or not the account exists.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)

var errRegistrationClosed = errors.New("registration closed")

func cleanEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	at := strings.LastIndexByte(s, '@')
	if len(s) > 254 || at < 1 || at == len(s)-1 || strings.ContainsAny(s, " \t\r\n") {
		return "", errors.New("Enter a valid email address.")
	}
	return s, nil
}

func hashPassword(pw string) ([]byte, error) {
	// bcrypt only uses the first 72 bytes and Go rejects longer input.
	if len(pw) < 8 || len(pw) > 72 {
		return nil, errReject{"Password must be 8 to 72 characters."}
	}
	return bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
}

// insertUser adds an account. With firstOnly, it fails with
// errRegistrationClosed unless the users table is empty; the check and the
// insert share one transaction so two racing sign-ups cannot both win.
func (a *app) insertUser(ctx context.Context, email, password string, isAdmin, firstOnly bool) (user, error) {
	email, err := cleanEmail(email)
	if err != nil {
		return user{}, errReject{err.Error()}
	}
	hash, err := hashPassword(password)
	if err != nil {
		return user{}, err
	}
	u := user{ID: uuid.NewString(), Email: email, IsAdmin: isAdmin}
	err = withTx(ctx, a.db, func(tx *sql.Tx) error {
		if firstOnly {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				return errRegistrationClosed
			}
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO users (id, email, password_hash, is_admin) VALUES (?, ?, ?, ?)
			 ON CONFLICT(email) DO NOTHING`, u.ID, u.Email, hash, u.IsAdmin)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errReject{"An account with this email already exists."}
		}
		return nil
	})
	return u, err
}

func (a *app) registrationOpen(ctx context.Context) (bool, error) {
	var exists bool
	err := a.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users)`).Scan(&exists)
	return !exists, err
}

// registrationStatus tells the frontend whether to show first-run sign-up.
func (a *app) registrationStatus(w http.ResponseWriter, r *http.Request) {
	open, err := a.registrationOpen(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"open": open})
}

// register creates the first account as admin and signs it in. It works only
// while no account exists.
func (a *app) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		StudioName string `json:"studioName"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	u, err := a.insertUser(r.Context(), in.Email, in.Password, true, true)
	var rej errReject
	switch {
	case errors.Is(err, errRegistrationClosed):
		writeError(w, http.StatusForbidden, "Registration is closed. Ask an admin for an account.")
		return
	case errors.As(err, &rej):
		writeError(w, http.StatusBadRequest, rej.msg)
		return
	case err != nil:
		serverError(w, err)
		return
	}
	if name := strings.TrimSpace(in.StudioName); name != "" {
		if _, err := a.db.ExecContext(r.Context(),
			`UPDATE settings SET studio_name = ? WHERE id = 1`, truncateRunes(name, 100)); err != nil {
			serverError(w, err)
			return
		}
	}
	if !a.startSession(w, r, u.ID) {
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (a *app) startSession(w http.ResponseWriter, r *http.Request, userID string) bool {
	// New token on privilege change prevents session fixation.
	if err := a.sessions.RenewToken(r.Context()); err != nil {
		serverError(w, err)
		return false
	}
	a.sessions.Put(r.Context(), sessionUserKey, userID)
	return true
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	email := strings.TrimSpace(in.Email)
	if email == "" || in.Password == "" {
		writeError(w, http.StatusBadRequest, "Enter your email and password.")
		return
	}

	var u user
	var hash []byte
	err := a.db.QueryRowContext(r.Context(),
		`SELECT id, email, is_admin, password_hash FROM users WHERE email = ?`, email).
		Scan(&u.ID, &u.Email, &u.IsAdmin, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		hash = dummyHash
	} else if err != nil {
		serverError(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(in.Password)) != nil || u.ID == "" {
		writeError(w, http.StatusUnauthorized, "Email or password is incorrect.")
		return
	}
	if !a.startSession(w, r, u.ID) {
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.sessions.Destroy(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}

// changePassword lets a signed-in user replace their own password.
func (a *app) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	u := currentUser(r)
	var hash []byte
	if err := a.db.QueryRowContext(r.Context(),
		`SELECT password_hash FROM users WHERE id = ?`, u.ID).Scan(&hash); err != nil {
		serverError(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(in.CurrentPassword)) != nil {
		writeError(w, http.StatusBadRequest, "Current password is incorrect.")
		return
	}
	newHash, err := hashPassword(in.NewPassword)
	var rej errReject
	if errors.As(err, &rej) {
		writeError(w, http.StatusBadRequest, rej.msg)
		return
	} else if err != nil {
		serverError(w, err)
		return
	}
	if _, err := a.db.ExecContext(r.Context(),
		`UPDATE users SET password_hash = ? WHERE id = ?`, newHash, u.ID); err != nil {
		serverError(w, err)
		return
	}
	// Rotate this session's token; other devices keep their sessions until they expire.
	if !a.startSession(w, r, u.ID) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requireUser loads the signed-in user. Deleted users lose access at once.
func (a *app) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := a.sessions.GetString(r.Context(), sessionUserKey)
		if id == "" {
			writeError(w, http.StatusUnauthorized, "sign in required")
			return
		}
		u := user{ID: id}
		err := a.db.QueryRowContext(r.Context(),
			`SELECT email, is_admin FROM users WHERE id = ?`, id).Scan(&u.Email, &u.IsAdmin)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "sign in required")
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxUser, u)))
	})
}

func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin {
			writeError(w, http.StatusForbidden, "admin only")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func currentUser(r *http.Request) user {
	u, _ := r.Context().Value(ctxUser).(user)
	return u
}

func userID(r *http.Request) string { return currentUser(r).ID }
