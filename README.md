# rsync-web

A modern, lightweight web UI for rsync with real-time output streaming via WebSockets.

![rsync-web screenshot](screenshot.png?v=3)

## Features

- **Real-time output streaming** - Watch rsync progress live via WebSocket (no page refresh needed)
- **Multiple concurrent jobs** - Run and monitor several rsync operations simultaneously
- **Command history** - View past commands, reuse them, or modify and re-run
- **File browser** - Navigate directories and select source/destination paths visually
- **SSH host integration** - Automatically reads hosts from `~/.ssh/config`
- **Comprehensive rsync options** - All common flags available via checkboxes:
  - Archive (`-a`), Verbose (`-v`), Compress (`-z`), Progress (`-P`)
  - Dry-run (`-n`), Delete (`--delete`), Human-readable (`-h`)
  - Recursive, symlinks, permissions, times, group, owner, devices
  - Partial, append, ignore-existing, size-only, checksum, backup
- **Exclude patterns** - Multi-line exclude support
- **Custom options** - Add any rsync flag (e.g., `--bwlimit=1000`)
- **rsync availability check** - Warns if rsync isn't installed on the system
- **Disk space** - `GET /api/status` includes `disk_total` and `disk_free` (bytes) for the working directory
- **Cross-platform** - Works on Linux and FreeBSD

## Installation

### Pre-built binaries

Download from [Releases](https://github.com/jgbrwn/rsync-web/releases) (coming soon) or build from source.

### Build from source

```bash
# Clone
git clone https://github.com/jgbrwn/rsync-web.git
cd rsync-web

# Build for current platform
make build

# Or build for specific platforms
make build-linux    # Linux amd64
make build-freebsd  # FreeBSD amd64
make build-all      # Both platforms
```

Requires Go 1.21+ and rsync installed on the target system.

## Usage

```bash
# Run from current directory (files browsable from pwd)
./rsync-web

# Custom listen address and port
./rsync-web -listen 0.0.0.0:8080

# Custom working directory for file browsing
./rsync-web -dir /data/backups

# Combine options
./rsync-web -listen 192.168.1.100:9000 -dir /mnt/storage
```

Then open http://127.0.0.1:8000 in your browser.

## Command-line Options

| Flag | Default | Description |
|------|---------|-------------|
| `-listen` | `127.0.0.1:8000` | Address and port to listen on |
| `-dir` | Current directory | Working directory for file browsing |
| `-preset NAME=DEST` | none | One-click destination shown under *Destination*; repeatable (e.g. `-preset Movies=nas:media/movies/`) |
| `-preset-opts` | safe copy flags | Comma- or space-separated rsync options used by presets and `/api/push` (default `-v -P -h -r -l -p -t -H --no-o --no-g --omit-dir-times --timeout=185`, never `--delete`) |
| `-push-root DIR` | off | Enables `POST /api/push`: sends a direct child of `DIR` (relative to `-dir`) to a preset |
| `-webhook URL` | off | POSTs a JSON summary when any job finishes; signed with `X-Rsync-Web-Signature: sha256=<HMAC>` when `RSYNC_WEB_WEBHOOK_SECRET` is set |

## Automation API

- `GET /api/presets`: `{presets, options, push_root}`.
- `POST /api/push` `{"name": "Movie (2020)", "preset": "Movies"}`: a narrow alternative to `/api/run` for scripts. It only
  sends an existing direct child of `-push-root`, using the preset's destination and options. Returns the history row.
  Add `"remove_source": true` to have rsync delete each file from the push root once it has been sent (`--remove-source-files`;
  folders stay), for callers that free space as they go.
- Webhook body: `{"id", "status", "exit_code", "source", "destination", "finished_at"}`. Verify the signature as
  HMAC-SHA256 of the raw body with your secret.

Options are normalized before running: shell-style quotes are removed (`--exclude='/temp/'` becomes
`--exclude=/temp/`, since there is no shell to strip them) and exact duplicates are dropped.

## How It Works

1. **Select source and destination** - Type paths manually, use the file browser, or pick SSH hosts
2. **Configure options** - Check the rsync flags you need, add excludes or custom options
3. **Preview command** - See the exact rsync command that will run
4. **Run** - Execute and watch real-time output in the browser
5. **Monitor** - Switch between multiple running jobs, cancel if needed
6. **Reuse** - Click any history entry to reload its settings

## Architecture

- **Backend**: Go with gorilla/websocket for real-time streaming
- **Database**: SQLite (pure Go, no CGO) for command history
- **Frontend**: Vanilla JS with Tailwind CSS
- **No external dependencies** at runtime beyond rsync itself

## FreeBSD Support

Fully tested on FreeBSD. Install rsync if needed:

```bash
pkg install rsync
```

All features work identically on FreeBSD and Linux.

## Security Notes

- By default, listens only on `127.0.0.1` (localhost)
- File browser is restricted to the working directory and subdirectories
- No authentication built-in - use a reverse proxy if exposing to a network
- Cross-site protection: state-changing requests and WebSocket handshakes from another origin are refused, and POST
  bodies must be `application/json` (so a web page can't drive rsync-web through a browser's cached proxy login)

## License

MIT
