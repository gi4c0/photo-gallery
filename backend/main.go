// Command server is the backend for the client photo gallery.
//
// Configuration (environment variables):
//
//	PHOTOS_DIR      required. Directory where uploaded photos and thumbnails are stored.
//	DB_PATH         SQLite database file. Default: gallery.db
//	ADDR            Listen address. Default: :8080
//	ADMIN_EMAIL     With ADMIN_PASSWORD: on startup, create this studio account
//	ADMIN_PASSWORD  or reset its password.
//	COOKIE_SECURE   Send the session cookie only over HTTPS. Default: true.
//	                Set to false for local development over plain HTTP.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

type config struct {
	addr          string
	dbPath        string
	photosDir     string
	adminEmail    string
	adminPassword string
	cookieSecure  bool
}

func loadConfig() (config, error) {
	c := config{
		addr:          envOr("ADDR", ":8080"),
		dbPath:        envOr("DB_PATH", "gallery.db"),
		photosDir:     os.Getenv("PHOTOS_DIR"),
		adminEmail:    os.Getenv("ADMIN_EMAIL"),
		adminPassword: os.Getenv("ADMIN_PASSWORD"),
		cookieSecure:  true,
	}
	if c.photosDir == "" {
		return c, errors.New("PHOTOS_DIR must be set")
	}
	if v := os.Getenv("COOKIE_SECURE"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return c, errors.New("COOKIE_SECURE must be true or false")
		}
		c.cookieSecure = b
	}
	return c, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(cfg.photosDir, 0o750); err != nil {
		log.Fatalf("create photos dir: %v", err)
	}

	db, err := openDB(cfg.dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if cfg.adminEmail != "" && cfg.adminPassword != "" {
		if err := ensureUser(context.Background(), db, cfg.adminEmail, cfg.adminPassword); err != nil {
			log.Fatalf("ensure admin user: %v", err)
		}
	}

	a := newApp(db, cfg.photosDir, cfg.cookieSecure)
	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           a.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No Read/WriteTimeout: large uploads and ZIP downloads can take minutes.
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("listening on %s, photos in %s", cfg.addr, cfg.photosDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// newApp wires the shared dependencies used by all handlers.
func newApp(db *sql.DB, photosDir string, cookieSecure bool) *app {
	return &app{db: db, photosDir: photosDir, sessions: newSessionManager(db, cookieSecure)}
}
