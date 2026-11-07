# Photo Gallery

Client photo gallery for photographers. Photographers sign in, create a gallery per client, upload photos, and share a private link. Clients open the link without an account, browse the photos, and download them one at a time or as a ZIP until the gallery's download period ends.

| Directory | Contents |
|-----------|----------|
| [`backend/`](backend/README.md) | Go API server with SQLite and on-disk photo storage. Configuration, API reference, security and limits. |
| [`frontend/`](frontend/README.md) | React + TypeScript (Vite) app for the studio and the client view. |
| `flake.nix` | Nix packages, dev shell and a NixOS module. |

## Quick start

Requires Go 1.26+ and Node.js. With Nix, `nix develop` provides both.

```sh
# terminal 1
cd backend && PHOTOS_DIR=./photos COOKIE_SECURE=false go run .
# terminal 2
cd frontend && npm install && npm run dev   # http://localhost:5173
```

Open http://localhost:5173. While no account exists, the login page offers sign-up, and the first account becomes the admin.

> **Warning:** until the first account exists, anyone who can reach the server can register as admin. Register before you expose the server publicly.

## Nix

```sh
nix build .#backend    # result/bin/photo-gallery
nix build .#frontend   # result/ holds index.html and assets
nix develop            # shell with go and nodejs
```

After changing `backend/go.sum` or `frontend/package-lock.json`, update `vendorHash` or `npmDepsHash` in `flake.nix`. Set the hash to `pkgs.lib.fakeHash`, build, and copy the correct hash from the error.

## Deployment

The backend serves plain HTTP and only the API. In production, put a reverse proxy in front of it that:

- terminates TLS,
- forwards `/api/*` to the backend,
- serves the frontend build from the same origin, with unknown paths falling back to `index.html`.

The app and API must share one origin: the backend sends no CORS headers and blocks cross-origin writes.

### NixOS

Add the flake as an input, then enable the module and a proxy. Example with Caddy:

```nix
{ inputs, pkgs, ... }:
let
  gallery = inputs.photo-gallery.packages.${pkgs.stdenv.hostPlatform.system};
in
{
  imports = [ inputs.photo-gallery.nixosModules.default ];

  services.photo-gallery.enable = true;   # listens on 127.0.0.1:8080

  services.caddy = {
    enable = true;
    virtualHosts."gallery.example.com".extraConfig = ''
      handle /api/* {
        reverse_proxy 127.0.0.1:8080
      }
      handle {
        root * ${gallery.frontend}
        try_files {path} /index.html
        file_server
      }
    '';
  };
}
```

Module options:

| Option | Default | Backend variable |
|--------|---------|------------------|
| `services.photo-gallery.listenAddress` | `127.0.0.1:8080` | `ADDR` |
| `services.photo-gallery.photosDir` | `/var/lib/photo-gallery/photos` | `PHOTOS_DIR` |
| `services.photo-gallery.dbPath` | `/var/lib/photo-gallery/gallery.db` | `DB_PATH` |
| `services.photo-gallery.package` | this flake's backend | |

The service runs as the `photogallery` system user with systemd sandboxing, and always sets `COOKIE_SECURE=true`. Back up `photosDir` and `dbPath` together.

For per-client rate limits, the proxy must run on the same host and set `X-Real-IP` to the client's address. See [Security](backend/README.md#security) in the backend README.

## Privacy and GDPR

Photos of people are personal data. If you serve clients in the EU, you (the photographer running the instance) are the data controller. This section lists what the app does with personal data. It is not legal advice.

What the app stores and sends:

- **Studio accounts:** email address and bcrypt password hash, in `dbPath`.
- **Photos:** originals are stored and downloaded unchanged, with all EXIF metadata, which can include GPS location and camera serial numbers. Thumbnails and web-size versions are re-encoded and carry no EXIF.
- **Cookies:** one session cookie for signed-in studio users, which is strictly necessary. Clients get no cookies. The app needs no consent banner.
- **Logs:** request logs hold method, path with client-link slugs redacted, status, size and duration. They hold no IP addresses. Rate limiters keep IP addresses in memory only.
- **Third parties:** none. Fonts are bundled with the frontend build, so browsers contact only your server.

Retention: nothing is deleted automatically. When the download period ends, clients can still view the gallery, and the photos stay on disk. The studio lists these galleries under "Downloads expired". Delete the ones you no longer need. Deleting a gallery removes its photos from disk. Deleting a user removes all their galleries.

Your part as the operator:

- Publish a privacy notice that says what you store, why and for how long.
- Sign a data processing agreement with your hosting provider.
- Set a retention period for backups of `photosDir` and `dbPath`. Deleted photos stay in backups until the backups expire.
- Handle client requests to delete or export their photos. Deleting a single photo or a whole gallery is available in the gallery editor.
