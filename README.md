# DockerView-Go

A beautiful terminal-based Docker container monitoring tool built with Go and bubbletea.

## Features

- **Real-time Monitoring**: Updates every second
- **Beautiful UI**: Built with [bubbletea](https://github.com/charmbracelet/bubbletea) and [lipgloss](https://github.com/charmbracelet/lipgloss)
- **Color-coded Status**: Green for running, red for stopped/exited containers
- **CPU Alerts**: High CPU usage (>50%) highlighted in red
- **Auto-detection**: Automatically detects Docker socket (including Colima)

## Requirements

- Go 1.21+
- Docker daemon running
- Terminal with true color support (recommended)

## Installation

### From Source

```bash
git clone https://github.com/zsuroy/dockerview-go.git
cd dockerview-go
make build
./build/dockerview
```

### Quick Run

```bash
go run ./cmd/dockerview/
```

## Usage

```bash
./dockerview
```

Press `Ctrl+C` to exit the application.

### Docker Socket

DockerView-Go automatically detects Docker sockets:

- Standard Docker socket (`/var/run/docker.sock`)
- Colima (`~/.colima/default/docker.sock`)
- Custom socket via `DOCKER_HOST` environment variable

```bash
DOCKER_HOST=unix:///path/to/docker.sock ./dockerview
```

## Build Commands

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

## Project Structure

```txt
dockerview-go/
├── cmd/dockerview/           # Main application
│   ├── main.go               # Entry point
│   ├── model.go              # TUI model
│   ├── update.go             # Self-update
│   ├── utils.go              # Utilities
│   └── version.go            # Version info
├── internal/docker/          # Docker client
│   ├── client.go             # Docker API client
│   └── client_test.go        # Tests
├── .github/                  # CI/CD
├── Makefile                  # Build commands
├── go.mod/go.sum             # Go modules
└── README.md                 # This file
```

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Author

[Suroy](https://suroy.cn)
