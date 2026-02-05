# DockerView-Go

A beautiful terminal-based Docker container monitoring tool built with Go and bubbletea.

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
make build
./build/dockerview
```

### Using Makefile

```bash
make build      # Build binary
make install    # Install to $GOPATH/bin
make test       # Run tests
make fmt        # Format code
make vet        # Run go vet
make deps       # Download dependencies
```

### Quick Run

```bash
go run ./cmd/dockerview/
```

## Usage

```bash
./dockerview
```

### Controls

- `Ctrl+C` - Exit the application

### Docker Socket

DockerView-Go automatically detects Docker sockets:

- Standard Docker socket (`/var/run/docker.sock`)
- Colima (`~/.colima/default/docker.sock`)
- Custom socket via `DOCKER_HOST` environment variable

```bash
DOCKER_HOST=unix:///path/to/docker.sock ./dockerview
```

## Project Structure

```txt
dockerview-go/
├── cmd/                          # Application entry points
│   └── dockerview/               # Main CLI application
│       ├── main.go               # Application entry point
│       ├── model.go              # TUI model
│       ├── update.go             # Self-update functionality
│       ├── utils.go              # Utility functions
│       └── version.go            # Version info
├── internal/                     # Private application code
│   └── docker/                   # Docker client & stats
│       ├── client.go             # Docker client
│       └── client_test.go        # Client tests
├── pkg/                          # Public libraries (if any)
├── test/                         # Integration tests & test data
├── configs/                      # Configuration files (if any)
├── docs/                         # Documentation (if any)
├── .github/                      # GitHub CI/CD
├── Makefile                      # Build commands
├── go.mod                        # Go module definition
├── go.sum                        # Dependency checksums
├── LICENSE                       # MIT License
├── README.md                     # This file
├── CHANGELOG.md                  # Changelog
└── CONTRIBUTING.md               # Contributing guide
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

### Using Makefile (Recommended)

```bash
make build      # Build binary to ./build/dockerview
make install    # Install to $GOPATH/bin
make test       # Run tests
make fmt        # Format code
make vet        # Run go vet
make deps       # Download and tidy dependencies
make release    # Build for all platforms (macOS, Linux, Windows)
make run        # Build and run
make clean      # Clean build directory
```

### Using go build

```bash
# Build binary
go build -o dockerview ./cmd/dockerview/

# Build with version info
go build -ldflags="-s -w" ./cmd/dockerview/
```

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Author

[Suroy](https://suroy.cn)

---

Inspired by [dockerview](https://github.com/zsuroy/dockerview) - the Python version.
