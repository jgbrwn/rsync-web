# rsync-web

A modern web UI for rsync with real-time output streaming.

## Features

- **Real-time output** via WebSocket - see rsync progress live
- **Multiple concurrent jobs** - run and monitor multiple rsync operations
- **Command history** - reuse or modify previous commands
- **File browser** - navigate and select files/directories
- **SSH host integration** - read hosts from `~/.ssh/config`
- **All rsync options** - archive, verbose, compress, progress, dry-run, delete, and many more
- **Exclude patterns** - easily configure exclusions
- **rsync availability check** - warns if rsync isn't installed

## Usage

```bash
# Run from current directory (files browsable from pwd)
./rsync-web

# Custom listen address
./rsync-web -listen 0.0.0.0:8080

# Custom working directory
./rsync-web -dir /path/to/files
```

Then open http://127.0.0.1:8000 in your browser.

## Building

```bash
go build -o rsync-web ./cmd/srv
```

## Options

| Flag | Default | Description |
|------|---------|-------------|
| `-listen` | `127.0.0.1:8000` | Address and port to listen on |
| `-dir` | Current directory | Working directory for file browsing |

## Screenshot

The web UI provides:
- Source/destination inputs with file browser and SSH host picker
- Checkbox-based rsync option selection
- Live command preview
- Real-time job output streaming
- Job history with reuse capability

## Requirements

- Go 1.21+
- rsync installed on the system

## License

MIT
