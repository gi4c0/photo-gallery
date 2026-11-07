# Photo Gallery — Backend

Go backend for a client photo gallery. Photographers sign in, create a gallery per client, upload photos, and share a private link. Clients open the link without an account, browse the photos, and download them one at a time or all at once as a ZIP. Each gallery has its own download period.

## Contents

- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Accounts and roles](#accounts-and-roles)
- [Concepts](#concepts)
- [API reference](#api-reference)
- [Storage](#storage)
- [Security](#security)
- [Limits](#limits)
- [Project layout](#project-layout)
- [Development](#development)
- [Known gaps](#known-gaps)

## Quick start

Requires Go 1.26+. No cgo or system libraries are needed.

```sh
cd backend
PHOTOS_DIR=./photos COOKIE_SECURE=false go run .
```

Then register the first account. That account becomes the admin:

```sh
curl -c cj -H 'Content-Type: application/json' \
  -d '{"email":"you@studio.com","password":"choose-a-password","studioName":"My Studio"}' \
  localhost:8080/api/register
```

> **Warning:** until the first account exists, anyone who can reach the server can register as admin. Register before you expose the server publicly.

## Configuration

All configuration is through environment variables.

| Variable        | Required | Default      | Meaning |
|-----------------|----------|--------------|---------|
| `PHOTOS_DIR`    | yes      | none         | Directory for uploaded photos and their generated versions. Created if missing. |
| `DB_PATH`       | no       | `gallery.db` | SQLite database file. |
| `ADDR`          | no       | `:8080`      | Listen address. |
| `COOKIE_SECURE` | no       | `true`       | Send the session cookie over HTTPS only. Set `false` for local development over plain HTTP. |

The server does not terminate TLS. In production, run it behind a reverse proxy that provides HTTPS, such as Caddy or nginx. On `SIGINT`/`SIGTERM` the server shuts down gracefully and waits up to 30 s for in-flight requests.

## Accounts and roles

- **First registration.** `POST /api/register` works only while the database has no users. It creates an **admin** account, signs it in, and can set the studio name. After that it returns `403`. The check and the insert run in one transaction, so two simultaneous sign-ups cannot both succeed.
- **Admins** create and delete users (`/api/users`), and edit studio settings. A new user can be made an admin with `isAdmin: true`.
- **Every user**, admin or not, manages only their own galleries. Another user's gallery returns `404`.
- **Self-deletion is blocked.** An admin cannot delete their own account, so at least one admin always remains.
- **Deleting a user** deletes all their galleries and photo files. Their sessions stop working on the next request.
- **Passwords** are 8 to 72 bytes (a bcrypt limit) and hashed with bcrypt. Users change their own password with `POST /api/password`. There is no reset by email yet.
- **Emails** are case-insensitive and unique.

## Concepts

### Gallery

| Field          | Meaning |
|----------------|---------|
| `name`         | Client or gallery name, 1–200 characters. |
| `shotDate`     | Photoshoot date, `YYYY-MM-DD`, or empty. |
| `months`       | Download period length in months, from `0` to `120`. `0` means no limit. |
| `sharedOn`     | Start of the download period. Set to today at creation and by *restart-download-period*. |
| `expiresOn`    | First day downloads are **closed**: `sharedOn + months`, in server local time. Omitted when `months` is `0`. |
| `downloadsOpen`| `true` while today is before `expiresOn`. Derived on every request, so changing `months` or restarting the period takes effect at once. |
| `slug`         | The secret token in the client link. |
| `coverPhotoId` | Cover photo. Defaults to the first uploaded photo. If the cover is deleted, it falls back to the first remaining photo in display order. |

The creation UI offers 1, 2, 3, 6, 9 and 12 months or no limit. The editor offers 1, 3, 6 and 12 months or no limit. The API accepts any value from 0 to 120.

### Client link

The link is `/g/{slug}` in the frontend, backed by `/api/g/{slug}`. A slug is a readable prefix from the gallery name plus 128 random bits, for example `sofia-and-tomas-` followed by 26 base32 characters.

- **Credential.** The slug is the client's only credential. It grants access to exactly one gallery.
- **Rotation.** *rotate-link* issues a new slug, and the old link stops working immediately.
- **Expiry.** After expiry the gallery stays **viewable** through thumbnails and web-size images. Every download endpoint returns `403`.
- **Originals** are never served to clients except through the expiry-checked download endpoints.

### Photo versions

Every upload is stored as three files:

| Version  | Size                       | Used for |
|----------|----------------------------|----------|
| original | untouched, metadata kept   | downloads, studio view |
| web      | JPEG, longest side ≤ 2048 px | lightbox |
| thumb    | JPEG, longest side ≤ 800 px  | grids |

EXIF and HEIF rotation is applied to the generated versions. `width` and `height` are the display size after rotation, so the frontend can reserve the correct aspect ratio before images load. HEIC uploads get JPEG versions that any browser can show; the original HEIC is kept for download.

## API reference

The API speaks JSON throughout. Errors look like `{"error": "human-readable message"}`, and messages meant for users are phrased to show as they are. URLs inside responses are relative to the server root.

### Public

| Method | Path            | Description |
|--------|-----------------|-------------|
| GET    | `/api/settings` | Studio branding: `{studioName, accentColor}`. Used by the login and client pages. |
| GET    | `/api/register` | `{open: bool}`. `true` while no account exists, so the frontend shows first-run sign-up instead of login. |
| POST   | `/api/register` | `{email, password, studioName?}`. Returns `201` with the user, as admin, and sets the session cookie. Returns `403` once any account exists. Rate limited. |
| POST   | `/api/login`    | `{email, password}`. Returns `200` with the user and sets the session cookie, or `401` with a generic error. Rate limited. |
| POST   | `/api/logout`   | Ends the session. Returns `204`. |

User object: `{id, email, isAdmin}`.

### Signed-in user

Requires the `session` cookie. Returns `401` if the cookie is missing, invalid, or belongs to a deleted user.

| Method | Path            | Description |
|--------|-----------------|-------------|
| GET    | `/api/me`       | Current user. |
| POST   | `/api/password` | `{currentPassword, newPassword}`. Returns `204`. Rate limited. |

### Admin only

Returns `403` for non-admins.

| Method | Path                | Description |
|--------|---------------------|-------------|
| PATCH  | `/api/settings`     | `{studioName?, accentColor?}`. The colour must be `#RRGGBB`. Returns the new settings. |
| GET    | `/api/users`        | All users. |
| POST   | `/api/users`        | `{email, password, isAdmin?}`. Returns `201` with the user, or `400` if the email is taken. |
| DELETE | `/api/users/{id}`   | Deletes the user, their galleries and their files. Returns `204`, or `400` for your own account. |

### Galleries (owner only)

| Method | Path | Body | Description |
|--------|------|------|-------------|
| GET    | `/api/galleries` | | Your galleries, newest first, without the photo list. |
| POST   | `/api/galleries` | `{name, shotDate?, months}` | Creates a gallery. Returns `201` with the gallery. |
| GET    | `/api/galleries/{id}` | | Gallery with `photos`. |
| PATCH  | `/api/galleries/{id}` | `{name?, shotDate?, months?}` | Partial update. |
| DELETE | `/api/galleries/{id}` | | Deletes the gallery, its photos and its files. Returns `204`. |
| PATCH  | `/api/galleries/{id}/cover` | `{photoId}` | Sets the cover. Returns `400` if the photo is not in this gallery. |
| POST   | `/api/galleries/{id}/restart-download-period` | | Sets `sharedOn` to today. |
| POST   | `/api/galleries/{id}/rotate-link` | | Issues a new slug; the old link dies. |
| POST   | `/api/galleries/{id}/photos` | multipart, `files` parts | Uploads photos (see below). |
| PATCH  | `/api/galleries/{id}/photos/order` | `{photoIds: [...]}` | New order. The list must contain every photo exactly once, otherwise `400`. |
| DELETE | `/api/galleries/{id}/photos` | `{photoIds: [...]}` | Bulk or single delete. Unknown IDs are ignored. |
| GET    | `/api/galleries/{id}/photos/{pid}/thumb` | | Thumbnail JPEG. |
| GET    | `/api/galleries/{id}/photos/{pid}/web` | | Web-size JPEG. |
| GET    | `/api/galleries/{id}/photos/{pid}/original` | | Original file. |

Every gallery endpoint except upload returns the full gallery object after the change, so the frontend can replace its state directly:

```json
{
  "id": "…", "name": "Sofia & Tomás", "shotDate": "2026-09-05",
  "sharedOn": "2026-11-03", "months": 3, "expiresOn": "2027-02-03",
  "downloadsOpen": true, "slug": "sofia-and-tomas-…",
  "coverPhotoId": "…", "coverThumbUrl": "/api/galleries/…/thumb",
  "photoCount": 2,
  "photos": [
    {"id": "…", "name": "IMG_2051.jpg", "width": 4000, "height": 6000,
     "thumbUrl": "…/thumb", "webUrl": "…/web", "originalUrl": "…/original"}
  ]
}
```

The list endpoint returns the same shape without `photos`. To build the share URL, the frontend combines its own origin with `/g/{slug}`.

#### Uploading

`POST /api/galleries/{id}/photos` takes `multipart/form-data` with one or more parts named `files`. Each file is streamed to disk, checked by content, and processed into its versions. Valid files are kept even if others fail:

```json
{
  "photos":   [ /* photo objects that were added */ ],
  "rejected": [ {"name": "notes.txt", "error": "Only JPG, PNG or HEIC images can be uploaded."} ]
}
```

**Recommended:** send **one file per request**. That gives per-file progress through XHR/fetch upload events and lets the frontend retry only the failed files after a network error. New photos are appended at the end of the order.

### Client (link holders)

No authentication; the `{slug}` is the credential. Responses carry `Referrer-Policy: no-referrer` and `X-Robots-Tag: noindex`.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/g/{slug}` | Gallery for the client (see below). Returns `404` with "This link is no longer active." for unknown or rotated links. |
| GET | `/api/g/{slug}/photos/{pid}/thumb` | Thumbnail. Works after expiry. |
| GET | `/api/g/{slug}/photos/{pid}/web` | Web-size image. Works after expiry. |
| GET | `/api/g/{slug}/photos/{pid}/download` | Original as an attachment with its original filename. Returns `403` after expiry. |
| GET | `/api/g/{slug}/download.zip` | All originals in display order, streamed as a ZIP. Duplicate names get ` (2)`, ` (3)`, and so on. Returns `403` after expiry and `404` if the gallery is empty. |

```json
{
  "name": "Sofia & Tomás", "shotDate": "2026-09-05", "coverPhotoId": "…",
  "downloadsOpen": true, "expiresOn": "2027-02-03",
  "downloadAllUrl": "/api/g/…/download.zip",
  "photos": [
    {"id": "…", "name": "IMG_2051.jpg", "width": 4000, "height": 6000,
     "thumbUrl": "…/thumb", "webUrl": "…/web", "downloadUrl": "…/download"}
  ]
}
```

When downloads are closed, `downloadUrl` and `downloadAllUrl` are omitted. The frontend should hide the download controls and show the "Downloads closed on `expiresOn`" notice. Studio branding comes from `/api/settings`.

The ZIP has no `Content-Length`, because it is generated while it streams. Show a "Preparing…" state until the browser starts the download.

### Status codes

| Code | When |
|------|------|
| 200 / 201 / 204 | Success. |
| 400 | Validation error; the `error` message is meant for users. |
| 401 | Not signed in, or wrong credentials. |
| 403 | Not an admin, registration closed, downloads expired, or a cross-origin write was blocked. |
| 404 | Not found, or not owned by you. |
| 413 | Request body too large. |
| 429 | Rate limited. A `Retry-After` header is sent. |

## Storage

```
$PHOTOS_DIR/
  <galleryID>/
    <photoID>.jpg|.png|.heic   original, byte-for-byte as uploaded
    <photoID>_web.jpg
    <photoID>_thumb.jpg
```

- **Database.** SQLite in WAL mode at `DB_PATH`. It holds `users`, `galleries`, `photos`, `settings` (a single row), and `sessions`. The schema is created at startup with `CREATE TABLE IF NOT EXISTS`; there is no migration system yet, so a schema change in development needs a fresh database file.
- **Deletes.** Deleting a gallery or a user removes the rows first, then the directory. If removing files fails, the error is logged and the rows stay deleted, so orphaned files are possible but no row ever points to a missing file.
- **Photo display order** is the `position` column.
- **Backups:** copy both `DB_PATH` and `PHOTOS_DIR`. For a consistent database copy while the server runs, use `sqlite3 gallery.db ".backup backup.db"`.

## Security

| Concern | Measure |
|---------|---------|
| Passwords | bcrypt. Login takes the same time whether or not the email exists. Generic error message. |
| Sessions | `session` cookie: `HttpOnly`, `SameSite=Lax`, `Secure` (unless `COOKIE_SECURE=false`). Lifetime 14 days, stored in SQLite. The token changes on login, registration and password change. |
| CSRF | Go's `http.CrossOriginProtection` rejects cross-origin non-GET browser requests using `Sec-Fetch-Site`/`Origin`, alongside `SameSite=Lax`. |
| Brute force | Login, register and password change share a limit of 10 requests per minute per IP. Failed client-link lookups are limited to 20 per minute per IP. |
| Link guessing | 128-bit random slug. Only one gallery per link, and no listing endpoint. |
| Link secrecy | Request logs replace the slug with `***`. Client responses send `no-referrer` and `noindex`. |
| Isolation | Every studio query is filtered by the owner. Every photo lookup is filtered by gallery. |
| Expiry | Enforced on the server for every download request. Originals are not reachable by clients otherwise. |
| Uploads | File type is detected from content, never from the extension or the client's claimed type. Size and pixel caps apply (see [Limits](#limits)). Files are served with `X-Content-Type-Options: nosniff`. |
| Filenames | Only the base name is kept. Control characters and quotes are stripped, and names are capped at 200 characters. Disk paths use server-generated IDs only. |

Rate limits use the TCP peer address. When the peer is loopback, they use the `X-Real-IP` header instead, so a reverse proxy on the same host must set `X-Real-IP` to the client's address and overwrite any value the client sent. nginx with `recommendedProxySettings` does this. `X-Forwarded-For` is never used, because clients can forge it. A proxy on another host is not trusted, so all its clients share one limit.

## Limits

| Limit | Value | Where |
|-------|-------|-------|
| Photo file size | 95 MB (under Cloudflare's 100 MB free-plan request cap) | `maxFileBytes`, `photos.go` |
| Upload request size | 4 GB | `maxUploadBytes`, `photos.go` |
| Image pixels | 120 MP (protects against decompression bombs) | `maxPixels`, `photos.go` |
| JSON body | 1 MB | `decodeJSON`, `app.go` |
| Accepted formats | JPEG, PNG, HEIC/HEIF | `sniffImage`, `photos.go` |

There are no HTTP read/write timeouts, so large uploads and ZIP downloads can run for minutes; slow headers time out after 10 s. Image processing runs during the upload request, and a 24 MP JPEG takes roughly a second.

## Project layout

| File | Responsibility |
|------|----------------|
| `main.go` | Configuration, startup, graceful shutdown. |
| `app.go` | Routes, middleware (logging, CSRF, rate limits), JSON helpers. |
| `db.go` | Opening SQLite, the schema, the transaction helper. |
| `auth.go` | Register, login, logout, password change, `requireUser`/`requireAdmin`. |
| `users.go` | Admin user management. |
| `settings.go` | Studio branding. |
| `galleries.go` | Gallery model, download period logic, slugs, studio gallery handlers. |
| `photos.go` | Upload pipeline, image versions, reorder, delete, file serving. |
| `public.go` | Client link handlers, single downloads, the ZIP stream. |
| `server_test.go` | End-to-end test of the studio, client, user and security flows. |

Libraries: [chi](https://github.com/go-chi/chi) (router), [httprate](https://github.com/go-chi/httprate) (rate limits), [scs](https://github.com/alexedwards/scs) with `sqlite3store` (sessions), [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) (pure-Go SQLite), [x/crypto/bcrypt](https://pkg.go.dev/golang.org/x/crypto/bcrypt), [imaging](https://github.com/disintegration/imaging) (resizing), [gen2brain/heic](https://github.com/gen2brain/heic) (HEIC decoding via WebAssembly, no cgo), [google/uuid](https://github.com/google/uuid), [gosimple/slug](https://github.com/gosimple/slug). ZIPs use the standard library's `archive/zip`.

## Development

```sh
go test ./...     # end-to-end test against an in-memory server and temp dirs
go vet ./...
go build -o server .
```

During development, serve the frontend from the same origin, for example through a Vite dev proxy that forwards `/api` to `:8080`. The backend sends no CORS headers, and cross-origin writes are blocked by design.

## Known gaps

These are relative to the PRD.

- **Resumable uploads.** Not implemented within a single file. Upload one file per request and retry the failed ones.
- **Password reset by email (A5), 2FA (A6), gallery passwords (S5), GPS/EXIF stripping.** Not implemented. Originals are delivered exactly as uploaded.
- **Branding is site-wide.** There is one studio name and accent colour, not one per user.
- **Search and sort of the gallery list (G5).** The list is newest first; sort it on the client.
- **No schema migrations** (see [Storage](#storage)).
- **No expiry reminders, no deletion of files after expiry.** Files are kept until the gallery is deleted.
