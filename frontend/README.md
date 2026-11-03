# Photo Gallery — Frontend

React + TypeScript (Vite) client for the Go backend in `../backend`.

```sh
# terminal 1
cd backend && PHOTOS_DIR=./photos COOKIE_SECURE=false go run .
# terminal 2
cd frontend && npm install && npm run dev   # http://localhost:5173
```

The Vite dev server proxies `/api` to `:8080`, so the app and API share one origin (the backend sends no CORS headers and blocks cross-origin writes). In production, serve `dist/` and the API from the same origin, with every unknown path falling back to `index.html`.

| Route | Page |
|-------|------|
| `/login` | Sign in, or first-run admin sign-up while no account exists |
| `/` | Gallery list and "New gallery" |
| `/galleries/:id` | Editor: upload (one file per request, with progress and retry), reorder, cover, delete, download period, share link |
| `/account` | Password change; for admins also studio branding and users |
| `/g/:slug` | Client view: cover, grid, lightbox, downloads |
