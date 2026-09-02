# MyShare

A single self-hosted binary for moving **text, screenshots and large files**
between your own devices — over localhost or your LAN. Think "a personal
AirForShare that runs on your own machine".

- **Resumable multi-GB uploads.** A 12 GB file that loses its connection at 7 GB
  resumes from ~7 GB — it never restarts, and it is never held in RAM.
- **Instant clipboard & screenshots.** Paste text on the laptop, tap *Copy* on
  the phone. Press <kbd>Ctrl/Cmd</kbd>+<kbd>V</kbd> anywhere to push a screenshot
  to every connected device.
- **One executable.** Go backend + embedded web UI + CLI. No Node, no Python, no
  database server to run. `~30 MB` binary, `~25 KB` gzipped initial page load,
  `~30 MB` RAM at rest and roughly flat during huge uploads.
- **Runs anywhere.** Linux, macOS, Windows — amd64 and arm64. Install into
  `~/.local/bin` with no root.

---

## Contents

- [Install](#install) · [Run](#run) · [Configuration](#configuration)
- [Storage & backup](#storage) · [LAN access](#lan-access) · [Large files](#large-files)
- [Security](#security) · [Run as a service](#run-as-a-service)
- [CLI](#cli) · [API](#api) · [Development](#development) · [Testing](#testing)

---

## Install

### From source (any OS with Go 1.24+)

```sh
git clone https://github.com/Dynamo2k1/MyShare.git && cd MyShare
make build          # builds the frontend (if npm is present) and ./bin/myshare
make install        # copies ./bin/myshare to ~/.local/bin  (no root)
```

`make install` runs `scripts/install.sh`, which:

- builds `bin/myshare` if it is missing,
- copies **only** `myshare` into `~/.local/bin` (nothing else there is touched),
- tells you how to add `~/.local/bin` to `PATH` if it isn't already.

### Windows

```powershell
powershell -ExecutionPolicy Bypass -File scripts\install.ps1
```

Installs to `%LOCALAPPDATA%\Programs\MyShare` and adds it to your user `PATH`.

### Prebuilt binaries

```sh
make dist    # dist-bin/myshare-{linux,darwin,windows}-{amd64,arm64}[.exe]
```

Drop the one for your platform anywhere on your `PATH`.

### Uninstall

```sh
sh scripts/uninstall.sh      # removes the binary + user service; leaves ~/MyShare
```

---

## Run

```sh
myshare                                   # http://127.0.0.1:8787
myshare --port 8787 --data-dir ~/MyShare
myshare --help
myshare --version
```

Startup prints where to reach it:

```
  MyShare is running

  Local:    http://localhost:8787
  LAN:      http://192.168.1.20:8787         (only with --host 0.0.0.0)

  Data:     /home/you/MyShare
  Storage:  42.0 GiB used, 918.0 GiB free (ext)
```

If the port is taken, MyShare **stops with a clear message and suggests a free
port** — it never silently picks a different one.

---

## Configuration

Precedence, highest wins:

```
CLI flags  >  MYSHARE_* environment variables  >  config file (TOML)  >  defaults
```

| Flag | Env | Default | Meaning |
|---|---|---|---|
| `--host` | `MYSHARE_HOST` | `127.0.0.1` | Bind address. `0.0.0.0` = reachable on the LAN. |
| `--port` | `MYSHARE_PORT` | `8787` | TCP port. Conflict = error, not a silent switch. |
| `--data-dir` | `MYSHARE_DATA_DIR` | `~/MyShare` | Database + all file storage. `~` is expanded. |
| `--max-file-size` | `MYSHARE_MAX_FILE_SIZE` | unlimited | e.g. `5GB`, `500MiB`. Enforced before any bytes are written. |
| `--max-storage` | `MYSHARE_MAX_STORAGE` | unlimited | Cap on total stored bytes. Warns at 90%. |
| `--auth` | `MYSHARE_AUTH` | `false` | Require a password (set it with `myshare set-password`). |
| `--tls` | `MYSHARE_TLS` | `false` | HTTPS with a self-signed cert (full clipboard support on LAN). |
| `--log-level` | `MYSHARE_LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error`. |
| `--cleanup-interval` | `MYSHARE_CLEANUP_INTERVAL` | `1h` | Background cleanup cadence (min `1m`). |
| `--config` | `MYSHARE_CONFIG` | OS default path | Path to a TOML config file. |

Config file locations checked automatically:
`~/.config/myshare/config.toml` (Linux),
`~/Library/Application Support/myshare/config.toml` (macOS),
`%APPDATA%\myshare\config.toml` (Windows).
See [`config.example.toml`](config.example.toml).

---

## Storage

Everything lives under one directory — easy to back up, nothing scattered:

```
~/MyShare/
├── myshare.db          SQLite metadata (files, clipboard, snippets, notes,
│                       share links, upload sessions, settings)  + -wal/-shm
├── blobs/aa/bb/<sha256>   file contents, addressed by hash (dedup + integrity)
├── uploads/               in-progress resumable uploads
├── tmp/                   scratch for direct uploads
└── certs/                 self-signed cert, only if --tls
```

**File names never become file paths.** Content is stored under its SHA-256; the
name you see is metadata only. Identical files are stored once and reference
counted.

### Backup

Stop MyShare (or accept a crash-consistent copy) and copy the whole directory:

```sh
myshare service stop 2>/dev/null || true
cp -a ~/MyShare ~/MyShare-backup-$(date +%F)
```

For a hot backup of just the database while the server runs:

```sh
sqlite3 ~/MyShare/myshare.db ".backup ~/MyShare-db-backup.db"
```

…then copy `blobs/` separately (it is append-only for live files).

> **Note on filesystems:** if `--data-dir` points at a FUSE / NTFS / exFAT / network
> mount, MyShare detects it and uses SQLite's rollback-journal mode instead of
> WAL (WAL needs reliable POSIX locking). It still works; keep the data dir on a
> native filesystem when you can.

---

## LAN access

By default MyShare binds `127.0.0.1` and is reachable only from the same machine.
To use it from your phone or another computer:

```sh
myshare --host 0.0.0.0 --port 8787 --data-dir ~/MyShare
```

Find your machine's LAN address:

```sh
# Linux
ip -4 -br addr | grep -v '127.0.0.1'
# macOS
ipconfig getifaddr en0
# Windows
ipconfig | findstr IPv4
```

Then open `http://<that-ip>:8787` on the other device. The **Settings** tab shows
this URL and a QR code for it.

`myshare.local` also works on networks with mDNS/Bonjour.

---

## Large files

Uploads over ~8 MiB use the [**tus**](https://tus.io) resumable protocol:

- streamed straight to disk in 16 MiB chunks — **never buffered in memory**,
- **pause / resume / cancel / retry** from the Transfers tab,
- survive a **browser refresh** (the upload URL is kept in `localStorage`),
- survive a **dropped connection** — on reconnect the client asks the server for
  the current offset and continues from there,
- on completion the file is hashed with streaming I/O, moved into the blob store
  with an atomic rename, and its checksum recorded.

A 3 GiB upload through the real server was measured at **&lt;1 MiB of heap
growth**. Downloads are equally lean: `Range`-capable, `ETag`, resumable, constant
memory.

---

## Security

This is a personal tool, but it is built defensively:

- **Localhost by default.** Exposing to the LAN requires an explicit
  `--host 0.0.0.0`, which logs a warning (and a louder one if `--auth` is off).
- **Path traversal is structurally impossible** — content is stored by hash, not
  by name; the API addresses everything by opaque ID. Filenames are sanitised
  (Windows-strict, on every OS) and there is a test corpus of hostile names.
- **Uploaded HTML/SVG cannot run:** everything outside a small media allowlist is
  served `Content-Disposition: attachment` with `X-Content-Type-Options: nosniff`.
- **Share tokens** are 32 bytes from `crypto/rand`; only their SHA-256 is stored,
  so a database leak yields no working links. Tokens support expiry, a max
  download count, one-time use, and revocation.
- **Strict CSP** (`default-src 'self'`, no `unsafe-eval`), same-origin only, no
  CORS. State-changing requests get an `Origin` check (CSRF defence); `SameSite`
  cookies back it up.
- **Optional auth** uses argon2id (`myshare set-password`, read from the terminal,
  never a flag) with per-IP login rate limiting.
- Server logs never contain passwords or tokens; browser-facing errors never leak
  filesystem paths.

For **remote** access (outside your LAN), don't expose the port directly — use a
VPN / Tailscale / WireGuard, or a properly secured reverse proxy.

---

## Run as a service

Per-user, **no root/admin**:

```sh
myshare service install     # Linux: systemd user unit
                            # macOS: launchd LaunchAgent
                            # Windows: Scheduled Task at logon
myshare service status
myshare service stop
myshare service start
myshare service restart
myshare service uninstall
```

The service records the host, port, data dir and auth/TLS settings resolved at
install time.

> **Linux:** a systemd *user* service stops when you log out unless lingering is
> enabled. Run once: `loginctl enable-linger "$USER"`.

Reference unit/plist templates are in [`packaging/`](packaging/).

---

## CLI

```
myshare                         start the server (default command)
myshare --help | --version

myshare set-password [--clear]   set/clear the login password (prompts; never a flag)

myshare upload <file>...         upload files to a running server
myshare clipboard [text]         add stdin/args to the shared clipboard
myshare clipboard get            print the most recent clipboard entry

myshare service <install|uninstall|start|stop|restart|status>
```

Examples:

```sh
echo "npm run build" | myshare clipboard
myshare clipboard get
myshare upload ./release.tar.gz
```

The `upload`/`clipboard` commands talk to a local server; point them elsewhere
with `--url http://host:port`.

---

## API

REST, same-origin, everything addressed by opaque ID. All list endpoints
paginate (`?limit=&cursor=`), and `q=` filters.

```
GET    /api/files            ?kind=&sort=name|size|created|updated&dir=asc|desc&q=
POST   /api/files            multipart or raw body (small files; large -> tus)
GET    /api/files/{id}
PATCH  /api/files/{id}        { "name": "..." }
DELETE /api/files/{id}
GET    /api/files/{id}/raw    streamed download, Range-capable  (?dl=1 forces attachment)

POST   /api/tus/              tus 1.0.0 resumable upload endpoint (HEAD/PATCH/…)

GET/POST/PATCH/DELETE  /api/clipboard[/{id}]      DELETE /api/clipboard clears all
GET/POST/PATCH/DELETE  /api/snippets[/{id}]       POST /api/snippets/{id}/duplicate
GET/POST/PATCH/DELETE  /api/notes[/{id}]

GET    /api/shares?file_id=   POST /api/shares   DELETE /api/shares/{id}
GET    /s/{token}             public download (?meta=1 for JSON preview)

GET    /api/transfers         DELETE /api/transfers/{id}
GET    /api/search?q=         full-text over files/clipboard/snippets/notes
GET    /api/status            version, storage, disk, connected clients
GET    /api/events            server-sent events stream (live updates)

POST   /api/auth/login|logout   GET /api/auth/status
```

---

## Development

Backend and frontend with live reload on one origin:

```sh
cd web && npm install        # once
make dev                     # Vite on :5173, MyShare on :8787 with --dev-proxy
```

Or separately:

```sh
cd web && npm run dev                                  # terminal 1
go run ./cmd/myshare --dev-proxy http://localhost:5173 # terminal 2
```

The production build embeds `web/dist` into the binary (`go:embed`), so
`make build` needs no Node at runtime — only to rebuild the UI.

Project layout:

```
cmd/myshare/          entrypoint
internal/
  cli/                cobra commands, listener-first startup, banner
  config/             precedence resolution + size parsing
  app/                wiring, lifecycle, cleanup, self-signed TLS
  server/             chi router, security headers, SPA embed, dev proxy
  api/                REST handlers (files, text, shares, search, status)
  uploads/            tus handler + streaming finalizer
  store/              SQLite: migrations, typed queries, FTS5, ULIDs
  blob/               content-addressed storage, streaming hash, dedup, GC
  auth/               argon2id, sessions, rate limiting
  sse/                one-way event hub (bounded buffers)
  shares/ safepath/ diskusage/ netinfo/ service/
web/                  Preact + TypeScript + Vite frontend
packaging/            systemd / launchd / Task Scheduler references
scripts/              install.sh · install.ps1 · uninstall.sh
```

---

## Testing

```sh
make test        # go test -race ./...
go vet ./... && gofmt -l .
```

What's covered:

- **Files:** upload, list/sort/filter/search, rename, delete, streamed download
  with `Range`, dedup + refcount, orphan GC.
- **Resumable uploads:** interrupt at 40% → HEAD for offset → resume → verify
  SHA-256; hundreds of misaligned chunks with HEAD polling; concurrent uploads
  under `-race`.
- **Memory:** a 3 GiB upload through the real tus HTTP handler asserts peak heap
  growth stays small (streaming proof). Run it with:
  ```sh
  MYSHARE_BIGTEST=1 TMPDIR=$HOME/.myshare-bigtmp \
    go test -run TestTusLargeUploadMemoryStable -timeout 20m ./internal/uploads
  ```
  (Point `TMPDIR` at a disk with room — the default `/tmp` is often a small tmpfs.)
- **Security:** hostile-filename corpus (`../../etc/passwd`, `..\..\windows`,
  `C:\`, `\\?\`, NUL, `CON`, NTFS ADS, RTL-override), path containment,
  HTML-served-as-attachment, strict CSP present, cross-origin POST rejected,
  auth gates the API when enabled, bogus share tokens 404.
- **Shares:** create → fetch → one-time auto-revoke → 410; expiry and max-download
  policy.
- **UI (browser-automated):** drag-drop upload with live SSE refresh, `Ctrl+V`
  screenshot capture, light/dark toggle, QR dialog, clipboard add, mobile
  bottom-nav layout, Settings status — zero console errors.

---

## License

MIT — see [LICENSE](LICENSE).
