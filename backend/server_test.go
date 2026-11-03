package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type testClient struct {
	t    *testing.T
	base string
	http *http.Client
}

func (c *testClient) do(method, path string, body io.Reader, contentType string, out any) int {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		c.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil && resp.StatusCode < 300 {
		if err := json.Unmarshal(data, out); err != nil {
			c.t.Fatalf("%s %s: decode %q: %v", method, path, data, err)
		}
	}
	return resp.StatusCode
}

func (c *testClient) json(method, path string, in, out any) int {
	c.t.Helper()
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	return c.do(method, path, body, "application/json", out)
}

func pngBytes(t *testing.T, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, 0, color.RGBA{200, 100, 50, 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestStudioAndClientFlow(t *testing.T) {
	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureUser(context.Background(), db, "studio@example.com", "secret-pass"); err != nil {
		t.Fatal(err)
	}
	photosDir := filepath.Join(dir, "photos")
	srv := httptest.NewServer(newApp(db, photosDir, false).routes())
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	studio := &testClient{t: t, base: srv.URL, http: &http.Client{Jar: jar}}
	anon := &testClient{t: t, base: srv.URL, http: &http.Client{}}

	// Auth.
	if s := studio.json("GET", "/api/galleries", nil, nil); s != 401 {
		t.Fatalf("unauthenticated list: %d", s)
	}
	if s := studio.json("POST", "/api/auth/login", map[string]string{"email": "studio@example.com", "password": "wrong"}, nil); s != 401 {
		t.Fatalf("bad password: %d", s)
	}
	if s := studio.json("POST", "/api/auth/login", map[string]string{"email": "Studio@Example.com", "password": "secret-pass"}, nil); s != 200 {
		t.Fatalf("login: %d", s)
	}

	// Create gallery.
	var g galleryOut
	if s := studio.json("POST", "/api/galleries", map[string]any{"name": "Sofia & Tomás", "shotDate": "2026-09-05", "months": 3}, &g); s != 201 {
		t.Fatalf("create: %d", s)
	}
	if g.ExpiresOn == "" || !g.DownloadsOpen || len(g.Slug) < 20 {
		t.Fatalf("unexpected gallery: %+v", g)
	}
	if s := studio.json("POST", "/api/galleries", map[string]any{"name": "  "}, nil); s != 400 {
		t.Fatalf("empty name: %d", s)
	}

	// Upload two PNGs and one non-image.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, f := range []struct {
		name string
		data []byte
	}{{"a.png", pngBytes(t, 40, 30)}, {"b.png", pngBytes(t, 20, 50)}, {"notes.txt", []byte("hello")}} {
		fw, _ := mw.CreateFormFile("files", f.name)
		fw.Write(f.data)
	}
	mw.Close()
	var up struct {
		Photos   []photoOut     `json:"photos"`
		Rejected []rejectedFile `json:"rejected"`
	}
	if s := studio.do("POST", "/api/galleries/"+g.ID+"/photos", &buf, mw.FormDataContentType(), &up); s != 200 {
		t.Fatalf("upload: %d", s)
	}
	if len(up.Photos) != 2 || len(up.Rejected) != 1 || up.Photos[0].Width != 40 || up.Photos[1].Height != 50 {
		t.Fatalf("upload result: %+v", up)
	}
	a, b := up.Photos[0].ID, up.Photos[1].ID

	// Reorder, set cover.
	if s := studio.json("PUT", "/api/galleries/"+g.ID+"/photos/order", map[string]any{"photoIds": []string{b}}, nil); s != 400 {
		t.Fatalf("partial reorder: %d", s)
	}
	if s := studio.json("PUT", "/api/galleries/"+g.ID+"/photos/order", map[string]any{"photoIds": []string{b, a}}, &g); s != 200 || g.Photos[0].ID != b {
		t.Fatalf("reorder: %d %+v", s, g.Photos)
	}
	if s := studio.json("PATCH", "/api/galleries/"+g.ID, map[string]any{"coverPhotoId": a}, &g); s != 200 || g.CoverPhotoID != a {
		t.Fatalf("set cover: %d %+v", s, g)
	}
	if s := studio.do("GET", "/api/galleries/"+g.ID+"/photos/"+a+"/thumb", nil, "", nil); s != 200 {
		t.Fatalf("studio thumb: %d", s)
	}
	if s := anon.do("GET", "/api/galleries/"+g.ID+"/photos/"+a+"/thumb", nil, "", nil); s != 401 {
		t.Fatalf("anon studio thumb: %d", s)
	}

	// Client view and downloads.
	var cg struct {
		Name           string     `json:"name"`
		DownloadsOpen  bool       `json:"downloadsOpen"`
		DownloadAllURL string     `json:"downloadAllUrl"`
		Photos         []photoOut `json:"photos"`
	}
	if s := anon.json("GET", "/api/g/"+g.Slug, nil, &cg); s != 200 || !cg.DownloadsOpen || len(cg.Photos) != 2 {
		t.Fatalf("client gallery: %d %+v", s, cg)
	}
	if s := anon.do("GET", cg.Photos[0].DownloadURL, nil, "", nil); s != 200 {
		t.Fatalf("client download: %d", s)
	}
	resp, err := http.Get(srv.URL + cg.DownloadAllURL)
	if err != nil {
		t.Fatal(err)
	}
	zipData, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil || len(zr.File) != 2 || zr.File[0].Name != "b.png" {
		t.Fatalf("zip: %v %d", err, len(zr.File))
	}

	// Expired period blocks downloads but not viewing.
	if _, err := db.Exec(`UPDATE galleries SET shared_on = '2020-01-01' WHERE id = ?`, g.ID); err != nil {
		t.Fatal(err)
	}
	if s := anon.do("GET", "/api/g/"+g.Slug+"/download.zip", nil, "", nil); s != 403 {
		t.Fatalf("expired zip: %d", s)
	}
	if s := anon.do("GET", "/api/g/"+g.Slug+"/photos/"+a+"/download", nil, "", nil); s != 403 {
		t.Fatalf("expired download: %d", s)
	}
	if s := anon.do("GET", "/api/g/"+g.Slug+"/photos/"+a+"/thumb", nil, "", nil); s != 200 {
		t.Fatalf("expired thumb: %d", s)
	}
	if s := studio.json("POST", "/api/galleries/"+g.ID+"/restart-period", nil, &g); s != 200 || !g.DownloadsOpen {
		t.Fatalf("restart: %d %+v", s, g)
	}

	// Rotating the link kills the old one.
	old := g.Slug
	if s := studio.json("POST", "/api/galleries/"+g.ID+"/rotate-link", nil, &g); s != 200 || g.Slug == old {
		t.Fatalf("rotate: %d", s)
	}
	if s := anon.json("GET", "/api/g/"+old, nil, nil); s != 404 {
		t.Fatalf("old link: %d", s)
	}

	// Deleting the cover photo falls back to the first remaining photo.
	if s := studio.json("POST", "/api/galleries/"+g.ID+"/photos/delete", map[string]any{"photoIds": []string{a}}, &g); s != 200 || g.PhotoCount != 1 || g.CoverPhotoID != b {
		t.Fatalf("delete photo: %d %+v", s, g)
	}
	if _, err := os.Stat(filepath.Join(photosDir, g.ID, a+".png")); !os.IsNotExist(err) {
		t.Fatalf("photo file not removed: %v", err)
	}

	// Delete gallery.
	if s := studio.json("DELETE", "/api/galleries/"+g.ID, nil, nil); s != 204 {
		t.Fatalf("delete gallery: %d", s)
	}
	if _, err := os.Stat(filepath.Join(photosDir, g.ID)); !os.IsNotExist(err) {
		t.Fatalf("gallery dir not removed: %v", err)
	}
	if s := anon.json("GET", "/api/g/"+g.Slug, nil, nil); s != 404 {
		t.Fatalf("deleted gallery link: %d", s)
	}

	// Logout ends the session.
	if s := studio.json("POST", "/api/auth/logout", nil, nil); s != 204 {
		t.Fatalf("logout: %d", s)
	}
	if s := studio.json("GET", "/api/auth/me", nil, nil); s != 401 {
		t.Fatalf("me after logout: %d", s)
	}
}
