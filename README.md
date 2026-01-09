# DockerView-Go

A beautiful terminal-based Docker container monitoring tool built with Go and bubbletea.

![DockerView](https://via.placeholder.com/800x400/1a1a2e/00d9ff?text=DockerView+Monitor)

## Description

DockerView-Go is a real-time Docker container monitoring tool featuring a modern terminal UI. It displays container statistics including ID, name, status, CPU usage, and memory usage with automatic refresh.

## Features

- **Real-time Monitoring**: Updates every second
- **Beautiful UI**: Built with [bubbletea](https://github.com/charmbracelet/bubbletea) and [lipgloss](https://github.com/charmbracelet/lipgloss)
- **Color-coded Status**: Green for running, red for stopped/exited containers
- **CPU Alerts**: High CPU usage (>50%) highlighted in red
- **Auto-detection**: Automatically detects Docker socket (including Colima)
- **Clean Exit**: Press `Ctrl+C` to quit

## Installation

### From Source

```bash
git clone https://github.com/zsuroy/dockerview-go.git
cd dockerview-go
go build -o dv ./cmd/dockerview/
./dv
```

### Quick Run

```bash
go run ./cmd/dockerview/
```

## Usage

```bash
./dv
```

### Controls

- `Ctrl+C` - Exit the application

### Docker Socket

DockerView-Go automatically detects Docker sockets:

- Standard Docker socket (`/var/run/docker.sock`)
- Colima (`~/.colima/default/docker.sock`)
- Custom socket via `DOCKER_HOST` environment variable

```bash
DOCKER_HOST=unix:///path/to/docker.sock ./dv
```

## Project Structure

```
dockerview-go/
├── cmd/
│   ├── dockerview/
│   │   └── main.go              # Main application with bubbletea UI
│   └── debug/
│       └── main.go              # Debug tool for testing Docker connection
├── internal/
│   └── docker/
│       └── client.go            # Docker client and stats fetching
├── go.mod                       # Go module definition
├── go.sum                       # Dependency checksums
├── LICENSE                      # MIT License
└── README.md                    # This file
```

## Dependencies

- [bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [lipgloss](https://github.com/charmbracelet/lipgloss) - Style library
- [Docker Go SDK](https://github.com/docker/docker) - Docker API client

## Requirements

- Go 1.21+
- Docker daemon running
- Terminal with true color support (recommended)

## Building

```bash
# Build binary
go build -o dv ./cmd/dockerview/

# Build with version info
go build -ldflags="-s -w" -o dv ./cmd/dockerview/
```

## Screenshots

```
┌────────────────────────────────────────┐
│  DockerView Monitor                    │
│  Press Ctrl+C to exit                  │
│                                        │
│  ID         Name              Status   │ CPU     Memory
│  ────────────────────────────────────────────────
│  d1c80c1090bb infallible_snyder Up 2 h...  0.0%    4.8 MB
│  abc123def456 my-postgres      Up 1 h...  2.5%    256 MB
│  xyz789ghi012 redis-cache      Running      0.8%    45 MB
└────────────────────────────────────────┘
```

## Debugging

If you encounter issues, use the debug tool:

```bash
go run ./cmd/debug/
```

This will print Docker connection status and container list.

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Author

[Suroy](https://suroy.cn)

---

Inspired by [dockerview](https://github.com/zsuroy/dockerview) - the Python version.
