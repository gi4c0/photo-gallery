package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type ctxKey int

const (
	ctxUserID ctxKey = iota
	ctxGallery
)

const sessionUserKey = "userID"

// dummyHash is compared against when the email is unknown, so a failed login
// takes the same time whether or not the account exists.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)

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

	var id string
	var hash []byte
	err := a.db.QueryRowContext(r.Context(),
		`SELECT id, password_hash FROM users WHERE email = ?`, email).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		hash = dummyHash
	} else if err != nil {
		serverError(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(in.Password)) != nil || id == "" {
		writeError(w, http.StatusUnauthorized, "Email or password is incorrect.")
		return
	}

	// New token on privilege change prevents session fixation.
	if err := a.sessions.RenewToken(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	a.sessions.Put(r.Context(), sessionUserKey, id)
	writeJSON(w, http.StatusOK, map[string]string{"email": email})
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.sessions.Destroy(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) me(w http.ResponseWriter, r *http.Request) {
	var email string
	err := a.db.QueryRowContext(r.Context(),
		`SELECT email FROM users WHERE id = ?`, userID(r)).Scan(&email)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"email": email})
}

func (a *app) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := a.sessions.GetString(r.Context(), sessionUserKey)
		if id == "" {
			writeError(w, http.StatusUnauthorized, "sign in required")
			return
		}
		var exists bool
		err := a.db.QueryRowContext(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)`, id).Scan(&exists)
		if err != nil {
			serverError(w, err)
			return
		}
		if !exists {
			writeError(w, http.StatusUnauthorized, "sign in required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxUserID, id)))
	})
}

func userID(r *http.Request) string {
	id, _ := r.Context().Value(ctxUserID).(string)
	return id
}
