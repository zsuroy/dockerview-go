<div align="center">

<!-- markdownlint-disable MD033 -->
<img src="assets/logo.svg" alt="DockerView Go" width="120" />

# DockerView-Go

A beautiful terminal-based Docker container monitoring tool built with Go and bubbletea, featuring a gorgeous real-time web dashboard.

[![Release](https://img.shields.io/github/v/release/zsuroy/dockerview-go?logo=github)](https://github.com/zsuroy/dockerview-go/releases/latest)
[![CI](https://img.shields.io/github/actions/workflow/status/zsuroy/dockerview-go/ci.yml?label=ci)](https://github.com/zsuroy/dockerview-go/actions/workflows/ci.yml)
[![Downloads](https://img.shields.io/github/downloads/zsuroy/dockerview-go/total?logo=github&label=downloads)](https://github.com/zsuroy/dockerview-go/releases)
[![License](https://img.shields.io/github/license/zsuroy/dockerview-go)](https://github.com/zsuroy/dockerview-go/blob/master/LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](https://react.dev/)
[![Docker](https://img.shields.io/badge/Docker-SDK-2496ED?logo=docker&logoColor=white)](https://docs.docker.com/engine/reference/api/client-lib/)

English | [中文](README_zh.md)

</div>

## Demo

![DockerView Go Demo](assets/demo.gif)

## Features

- **Real-time Monitoring**: Updates every second.
- **Beautiful TUI**: Built with [bubbletea](https://github.com/charmbracelet/bubbletea) and [lipgloss](https://github.com/charmbracelet/lipgloss) with keybindings for start, stop, restart, inline logs viewing, and command execution.
- **Real-Time Web Dashboard**: Enable the HTTP server (`-server`) to broadcast real-time container telemetry using Server-Sent Events (SSE) `/stream` and host a gorgeous glassmorphism web console with live SVG sparkline history, status filters, search highlighting, and 3D hover effects.
- **Web Container Controls**: Start, stop, and restart containers directly from the Web Dashboard (only showing containers stopped via dockerview during current session to keep the list clean).
- **Container Health Scoring**: Dynamically calculates a 0-100 health score for each container based on CPU load, memory usage, disk I/O, network rate, restarts, and uptime. Features a grouped top panel showing healthy, warning, and dangerous counts with neon pulsing indicators.
- **Inline Logs Modal**: Read container logs from TUI or in an advanced web modal featuring case-insensitive keyword searching, log level filters (ALL, DEBUG, INFO, WARN, ERROR), customizable tail line counts, match highlighting, auto-scroll, and instant log file downloads.
- **Command Execution**: Execute shell commands inside running containers from the TUI (`e` in the action panel) or the web dashboard modal. Features quick template shortcuts on web (directory list, environment variables, disk usage, etc.), stdout/stderr output separation, exit status code display, copy output helper, and token verification security.
- **Token Security**: Secured control API and log endpoints with token verification. Automatically generates secure startup keys, supports guest/read-only mode, and stores session tokens in localStorage.
- **Multi-language Support**: Interactive web dashboard supports language toggling between English and Chinese (via a button in the navigation header).
- **Mobile Client**: A cross-platform Expo / React Native app mirrors the dashboard on your phone — live monitoring, start/stop/restart controls, log filtering, and command execution, with English / 简体中文 support.
- **Theme Toggle**: Real-time web dashboard supports toggling between light and dark themes (with automatic system color-scheme preference detection).
- **One-Click Web Upgrade**: Trigger browser-based self-upgrades directly next to the version badge in the footer, which queries GitHub releases, automatically identifies the installation type (`go install` or `binary`), performs atomic updates, and streams step-by-step progress events in real-time.
- **Port Mappings Visualizer**: Displays all container port mappings and exposed ports directly on the dashboard cards. Exposed ports render as clean tags, and mapped port mappings appear as interactive badges (e.g. `8080 → 80/tcp`) linking directly to the running container web interface.
- **Disk Cleanup (Prune)**: Clean up unused images and dangling volumes from the web dashboard. Preview candidates with a dry-run, confirm deletion with an explicit acknowledgement, and view a detailed result summary with audit log. Guests can preview; admin token is required to delete.
- **Operation Audit Center**: Track who did what, when, to which container. Key write operations (start, stop, restart, exec) are persisted to an audit log with actor identity, source, timestamp, container, result, duration, and request context. The web dashboard provides a searchable audit view with filters, pagination, and JSON/Markdown export.
- **Color-coded Status**: Green for running, red for stopped/exited containers.
- **CPU Alerts**: High CPU usage (>50%) highlighted in red.
- **Auto-detection**: Automatically detects Docker socket (including Unix sockets, WSL, Colima, OrbStack, Podman, Rancher Desktop, etc.).

## Requirements

- Go 1.24+
- Docker daemon running
- Terminal with true color support (recommended)

## Installation

### Using `go install`

```bash
go install github.com/zsuroy/dockerview-go/cmd/dockerview@latest
```

Make sure `$GOPATH/bin` (or `$HOME/go/bin`) is in your `PATH`.

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

### TUI Controls

| Key         | Action            |
| ----------- | ----------------- |
| `↑` `↓`     | Select container  |
| `Enter`     | Show actions      |
| `s`         | Start container   |
| `x`         | Stop container    |
| `r`         | Restart container |
| `l`         | View logs         |
| `e`         | Execute command   |
| `q` / `Esc` | Back / Exit       |
| `Ctrl+C`    | Exit application  |

### Web Dashboard & Server Mode

You can run `dockerview` with an HTTP server enabled to view a real-time web dashboard from any browser:

```bash
# Enable HTTP server on default port 8080
./build/dockerview -server

# Customize the HTTP server port (e.g. 8023)
./build/dockerview -server -port 8023

# Set a custom security token
./build/dockerview -server -token my-secret-token
```

Once started, navigate to `http://localhost:8080` (or your custom port) in your web browser to access the interactive web console.

#### Security & Guest View Mode

- **Guest View (Read-Only)**: Anyone can open the dashboard to view real-time telemetry (CPU/Memory loads, network, block I/O) without entering a token.
- **Authenticated Controls (Admin)**: Modifying actions (Start, Stop, Restart) and viewing container Logs are protected and require a security token.
- **Token Management**:
  - If no token is specified via the `-token` flag or the `DOCKERVIEW_TOKEN` environment variable, a 16-byte random hex token is securely generated on startup and printed in the console.
  - When clicking an admin action or logs for the first time, a secure input overlay modal will appear. Once entered, the token is saved in the browser's `localStorage`.
  - Visiting the dashboard via the auto-generated URL `http://localhost:8080/?token=<token>` automatically authenticates your session and cleans up the address bar for clean sharing.

### Docker Socket

DockerView-Go automatically detects Docker sockets:

- Standard Docker socket (`/var/run/docker.sock`)
- Colima (`~/.colima/default/docker.sock`)
- Custom socket via `DOCKER_HOST` environment variable

```bash
DOCKER_HOST=unix:///path/to/docker.sock ./dockerview
```

## Mobile App

DockerView also ships a cross-platform **mobile client** (Expo / React Native) that connects to the same DockerView-Go backend server and offers real-time monitoring, container lifecycle controls (start/stop/restart), log filtering, and interactive command execution from your phone.

![DockerView Mobile Demo](assets/mobile.gif)

### Requirements

- Node.js 20+
- Expo CLI (`npm install -g expo-cli`) or use `npx expo`
- A running DockerView-Go server reachable from the device (use `10.0.2.2` for the Android emulator, `localhost` for iOS/Web)

### Setup & Run

```bash
cd mobile
npm install
npx expo start          # scan the QR code with Expo Go / Camera
npx expo start --android
npx expo start --ios
```

Configure the server host address and optional security token from the in-app **Settings** screen. The app supports English and 简体中文.

### Build a standalone app

- **Local Android APK**: the GitHub workflow `.github/workflows/build-mobile.yml` prebuilds the native project and compiles a signed release APK, uploaded as a workflow artifact.
- **Cloud builds (Android & iOS)**: configure an `EXPO_TOKEN` repository secret and push with `eas.json` profiles (`preview` / `production`). iOS builds additionally require Apple credentials set up in your EAS project.

```bash
cd mobile
npx eas-cli build --platform android --profile preview   # installable APK
npx eas-cli build --platform ios --profile production    # App Store / ad-hoc
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
├── cmd/dockerview/           # Main TUI application (bubbletea)
│   ├── main.go               # Entry point
│   ├── model.go              # TUI model
│   ├── update.go             # Self-update
│   ├── utils.go              # Utilities
│   └── version.go            # Version info
├── internal/
│   ├── docker/               # Docker API client & health scoring
│   ├── server/               # HTTP & SSE server
│   │   ├── server.go         # Server logic & API endpoints
│   │   └── web/              # Compiled web UI assets (embedded automatically)
│   └── version/              # Version helpers
├── frontend/                 # React + TypeScript Web Dashboard (Vite)
│   ├── src/                  # React source (App.tsx, components, i18n, ...)
│   ├── index.html            # Vite template
│   └── vite.config.ts        # Build config (outputs to internal/server/web)
├── mobile/                   # Expo / React Native mobile client
│   ├── app/                  # Screens (dashboard, settings, about)
│   ├── components/           # Reusable UI components
│   ├── utils/                # API client, i18n (zh.ts / en.ts), storage
│   ├── app.json              # Expo config
│   └── eas.json              # EAS Build profiles
├── .github/workflows/        # CI: ci.yml, release.yml, build-mobile.yml
├── Makefile                  # Go build commands
├── go.mod / go.sum           # Go modules
└── README.md                 # This file
```

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Author

[Suroy](https://suroy.cn)
