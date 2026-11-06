{
  description = "Client photo gallery: Go backend and React frontend";

  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-26.05";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = self.shortRev or self.dirtyShortRev or "dev";
    in
    {
      packages = forAllSystems (pkgs: rec {
        backend = pkgs.buildGoModule {
          pname = "photo-gallery";
          inherit version;
          src = ./backend;
          # Update after changing go.mod/go.sum: set to pkgs.lib.fakeHash, build, copy the hash from the error.
          vendorHash = "sha256-jYVxQB+g8mAtW8lMkeKWUL1SRl0EpAD5dXFr03NM2W4=";
          env.CGO_ENABLED = 0;
          ldflags = [
            "-s"
            "-w"
          ];
          postInstall = "mv $out/bin/photogallery $out/bin/photo-gallery";
          meta.mainProgram = "photo-gallery";
        };

        # Static files (index.html + assets). Serve them from the same origin as
        # the API, with unknown paths falling back to index.html.
        frontend = pkgs.buildNpmPackage {
          pname = "photo-gallery-frontend";
          inherit version;
          src = ./frontend;
          # Update after changing package-lock.json: set to pkgs.lib.fakeHash, build, copy the hash from the error.
          npmDepsHash = "sha256-eQAQ+QfrhHZw5wX14HpBYJzDBB59w4DKK6bTaBif/OY=";
          installPhase = ''
            runHook preInstall
            cp -r dist $out
            runHook postInstall
          '';
        };

        default = backend;
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            nodejs
          ];
        };
      });

      # Runs the backend with SQLite and a photo directory. Plain HTTP: put a TLS
      # reverse proxy in front that also serves packages.frontend (see README).
      nixosModules.default =
        {
          config,
          lib,
          pkgs,
          ...
        }:
        let
          cfg = config.services.photo-gallery;
          name = "photogallery";
        in
        {
          options.services.photo-gallery = {
            enable = lib.mkEnableOption "client photo gallery backend";

            package = lib.mkOption {
              type = lib.types.package;
              default = self.packages.${pkgs.stdenv.hostPlatform.system}.backend;
              description = "Backend package.";
            };

            listenAddress = lib.mkOption {
              type = lib.types.str;
              default = "127.0.0.1:8080";
              description = "ADDR. Plain HTTP only.";
            };

            photosDir = lib.mkOption {
              type = lib.types.path;
              default = "/var/lib/photo-gallery/photos";
              description = "PHOTOS_DIR: originals and generated versions.";
            };

            dbPath = lib.mkOption {
              type = lib.types.path;
              default = "/var/lib/photo-gallery/gallery.db";
              description = "DB_PATH: SQLite database file. Back it up together with photosDir.";
            };
          };

          config = lib.mkIf cfg.enable {
            users.users.${name} = {
              isSystemUser = true;
              group = name;
            };
            users.groups.${name} = { };

            systemd.tmpfiles.rules = [
              "d ${cfg.photosDir} 0700 ${name} ${name} -"
              "d ${dirOf cfg.dbPath} 0700 ${name} ${name} -"
            ];

            systemd.services.photo-gallery = {
              description = "Client photo gallery backend";
              wantedBy = [ "multi-user.target" ];
              after = [ "network-online.target" ];
              wants = [ "network-online.target" ];

              environment = {
                ADDR = cfg.listenAddress;
                PHOTOS_DIR = cfg.photosDir;
                DB_PATH = cfg.dbPath;
                COOKIE_SECURE = "true";
              };

              serviceConfig = {
                ExecStart = lib.getExe cfg.package;
                User = name;
                Group = name;
                Restart = "on-failure";
                RestartSec = 5;
                # Graceful shutdown waits up to 30 s for uploads and ZIP downloads.
                TimeoutStopSec = 40;

                ReadWritePaths = [
                  cfg.photosDir
                  (dirOf cfg.dbPath)
                ];
                ProtectSystem = "strict";
                ProtectHome = true;
                PrivateTmp = true;
                PrivateDevices = true;
                NoNewPrivileges = true;
                ProtectKernelTunables = true;
                ProtectKernelModules = true;
                ProtectControlGroups = true;
                RestrictAddressFamilies = [
                  "AF_UNIX"
                  "AF_INET"
                  "AF_INET6"
                ];
                RestrictNamespaces = true;
                LockPersonality = true;
                # No MemoryDenyWriteExecute: HEIC decoding runs WebAssembly
                # through wazero, which compiles it to native code at runtime.
                SystemCallArchitectures = "native";
                UMask = "0077";
              };
            };
          };
        };
    };
}
