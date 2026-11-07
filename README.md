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

Behind a proxy, all clients share the proxy's address for rate limiting. See [Security](backend/README.md#security) in the backend README.
