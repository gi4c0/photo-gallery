package main

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
)

// Admin-only account management. Each user sees and manages only their own
// galleries; admins additionally manage accounts and studio settings.

func (a *app) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT id, email, is_admin FROM users ORDER BY created_at, email`)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()
	out := []user{}
	for rows.Next() {
		var u user
		if err := rows.Scan(&u.ID, &u.Email, &u.IsAdmin); err != nil {
			serverError(w, err)
			return
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"isAdmin"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	u, err := a.insertUser(r.Context(), in.Email, in.Password, in.IsAdmin, false)
	var rej errReject
	if errors.As(err, &rej) {
		writeError(w, http.StatusBadRequest, rej.msg)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

// deleteUser removes an account with all its galleries and photo files.
// Admins cannot delete themselves, so at least one admin always remains.
func (a *app) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userID")
	if id == userID(r) {
		writeError(w, http.StatusBadRequest, "You cannot delete your own account.")
		return
	}
	var galleryIDs []string
	err := withTx(r.Context(), a.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(r.Context(), `SELECT id FROM galleries WHERE owner_id = ?`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var gid string
			if err := rows.Scan(&gid); err != nil {
				rows.Close()
				return err
			}
			galleryIDs = append(galleryIDs, gid)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		res, err := tx.ExecContext(r.Context(), `DELETE FROM users WHERE id = ?`, id) // galleries and photos cascade
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNotFound
		}
		return nil
	})
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	for _, gid := range galleryIDs {
		if err := os.RemoveAll(a.galleryDir(gid)); err != nil {
			log.Printf("delete gallery %s files: %v", gid, err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
