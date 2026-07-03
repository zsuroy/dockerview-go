# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.17] - 2026-07-03

### Fixed

- **Stable Port Display Order**: Sort container port mappings by private port, public port, protocol, and IP address before returning them to the dashboard, preventing the displayed order from changing between refreshes due to randomized map iteration. The same normalization is applied to both running and stopped containers.

## [0.1.16] - 2026-06-26

### Fixed

- **Port Duplication Display**: Added deduplication logic for running container port mappings fetched via Docker API `ContainerList`, which could return duplicate port entries for the same binding. Now uses a `deduplicatePorts()` helper to ensure each port mapping (PrivatePort + PublicPort + Type) appears only once in the dashboard.

- **Chinese Text Layout**: Fixed formatting issues when using Chinese language. Replaced `uppercase` CSS class (which doesn't work with CJK characters) with `tracking-wide`, adjusted letter-spacing from `tracking-wider` to `tracking-wide` for better CJK rendering, increased label widths from 72px to 80px to accommodate wider Chinese characters, and added Chinese font support (PingFang SC, Hiragino Sans GB, Microsoft YaHei, Noto Sans CJK SC) to the font stack.

- **Mobile Responsiveness**: Added responsive styles for grid container (single column on mobile), card padding, and button layout. Reduced main container padding on mobile for better edge handling.

- **Language Switcher Layout**: Grouped language and theme buttons in a flex container with `break-inside-avoid` to prevent them from splitting across lines on mobile when header wraps.

## [0.1.15] - 2026-06-25

### Added

- **One-Click Web Upgrade**: Added browser-based self-upgrade functionality next to the version badge in the footer, which queries GitHub releases, detects the installation type (`go_install` or `binary`), replaces the running binary atomically via `selfupdate` or triggers a background Go toolchain compile, and streams step-by-step progress events in real-time.
- **Container Port Mappings Visualization**: Render container port bindings and exposed ports inside each telemetry card on the web dashboard. Standard exposed ports display in clean neutral badges; bound port mappings are visualized in clickable hyperlinks (e.g. `8080 → 80/tcp`) pointing directly to their mapped browser host URL.
- **Container Command Execution (Exec)**: Introduced an interactive command execution (exec) modal in the web dashboard. Users can execute arbitrary shell commands inside running containers under secure token authentication. Includes interactive output streams (separated stdout/stderr styling), exit code visualization, instant output clipboard copying, and customizable template shortcuts (e.g. directory listings, process tree, env variables).

## [0.1.14] - 2026-06-24

### Added

- **Multi-language Support (i18n)**: Implemented full internationalization support on the web dashboard. Added translation configuration and context hooks to switch dynamically between English and Chinese.
- **Localized UI & Tooltips**: Fully translated headers, dashboard telemetry summary cards, container metric items, operation actions, log viewer UI, and interactive dialogs/tooltips.
- **Theme Toggle (Light/Dark Mode)**: Added switching between dark and light themes with automatic system color-scheme preference detection and a custom persistent toggle button in the header. Optimized all metrics cards, logs viewports, and action dialogs for clear text contrast and visibility under the light theme.


## [0.1.13] - 2026-06-18

### Added

- **Container Health Scoring**: Compute real-time health score (0-100) and status labels ("healthy", "warning", "dangerous") based on CPU load, memory utilization, Disk I/O, Network traffic, container restarts, and uptime.
- **Health Indicators Panel**: Optimized the top dashboard summary grid into an integrated, mobile-responsive monitoring panel showing total nodes, active nodes, and health distribution with neon pulsing status indicators.
- **Log Modal Enhancements**: Added support for log keyword searching (grep), log level filters (ALL, DEBUG, INFO, WARN, ERROR), customizable tail line counts, search match highlighting, and instant log file downloading.

### Fixed

- **Double Auth Dialog Bug**: Resolved stale closure state issue where container action actions or opening logs would trigger the authentication window twice.
- **Stopped Container Tracking**: Redesigned stopped containers listing to only show containers stopped via dockerview during the current session, preventing dashboard clutter from old historical inactive containers.
- **Mobile Adaptations**: Enhanced spacing and sizes of dialog content boxes and headers on small screens to ensure all elements (e.g. close buttons) remain perfectly visible and aligned.
- **iOS Zoom Prevention**: Added font-size rules (16px) for input and select controls in mobile views to block automatic Safari page-zoom.
- **Dialog A11y Warnings**: Suppressed Radix UI console warnings by ensuring proper description bindings on dialog contents.

## [0.1.12] - 2026-06-14

### Added

- **React Frontend**: Rebuilt web dashboard from legacy single `index.html` into a modular React + TypeScript application (`frontend/`) using Vite, Tailwind CSS v4, and Radix UI
  - Component architecture: `Header`, `ContainerCard`, `SummaryDashboard`, `Sparkline`, `AuthModal`, `LogsModal`
  - Dynamic metric dashboards, SVG sparklines, custom toasts, Radix Dialog-based modals
  - Collapsible offline grid with expand/collapse for stopped containers
- **Go Embed Optimization**: Replaced legacy single-file cached reading with `http.FileServer` and `fs.Sub` serving compiled Vite assets from embedded filesystem
- **Reverse Proxy Support**: Configured relative base paths (`./`) in Vite and `/dashboard` route for seamless subpath proxy integration
- **Container Tracking**: Stateful tracker ignores already-stopped containers on startup, tracks active ones so they display correctly when stopped during session
- **Build Integration**: Added `build-ui` target to Makefile, automated React asset compilation on standard `make build`

### Changed

- **Brand Assets**: Replaced all SVGs (favicon, icons, logo) with unified DockerView brand illustration — neon glow, glassmorphism gradients, radar grid background, `#38BDF8` accents
- **Makefile**: Updated `build-ui` target to use `frontend/` directory
- **README.md**: Updated directory tree to reflect `frontend/` structure
- **Gitignore**: Added `dockerview` binary, `.claude/`, `.antigravity/`

## [0.1.11] - 2026-05-27

### Added

- **Container operations**: Start, stop, restart containers directly from the TUI
- **Log viewer**: View last 100 lines of container logs inline
- **Keyboard navigation**: Arrow keys to select containers, Enter to open action panel
- Action bar with keybindings: `s`tart, `x`stop, `r`estart, `l`ogs, `q`uit
- **Web Container Controls**: Start, stop, restart containers directly from the Web Dashboard.
- **Web Log Viewer**: View inline container logs with auto-scroll and 3-second auto-polling stream in a sleek glassmorphic modal.
- **Security & Authentication**:
  - Secure token-based authentication for Web control APIs and logs.
  - Auto-generated 16-byte random security tokens printed on startup, customizable via `-token` flag or `DOCKERVIEW_TOKEN` environment variable.
  - "Guest View & Authenticated Control" mode: stats are publicly viewable by default, while admin actions/logs trigger an elegant security token input overlay.
  - URL cleanup: automatically strip token parameters from the address bar after loading for cleaner sharing.
- **Log Decoded Formatting**: Fixed binary multiplexing stream headers (unwanted gibberish symbols) at the beginning of log lines by demultiplexing stdout/stderr streams.

### Changed

- **Performance**: Extract lipgloss styles to package-level vars, avoiding repeated allocations per `View()` render
- **Performance**: Replace `time.After` with `time.NewTicker` in polling loop to prevent timer leaks
- **Performance**: Cache embedded web dashboard HTML at startup instead of reading on every HTTP request
- **Performance**: Deduplicate `os.UserHomeDir()` call in Docker socket detection
- **Concurrency**: Switch `model.mu` from `sync.Mutex` to `sync.RWMutex`, allowing concurrent reads in `View()`
- **Code quality**: Remove redundant `colStyles` array (7 identical copies of `headerStyle`)
- **Code quality**: Remove duplicate ID truncation (already truncated in `client.go`)
- **Code quality**: Log server startup errors to stderr instead of silently swallowing them
- **Fix**: Correct typo "Donwloading" in self-update output

## [0.1.9] - 2026-05-20

### Fixed

- **Fix dashboard sorting**: Correctly apply the CSS `order` property to the direct grid child `.card-wrapper` instead of the inner `.card`, enabling the NAME, CPU, and RAM sorting/grouping controls to function properly.

## [0.1.8] - 2026-05-20

- **Offline & Intranet Compatibility**:
  - Replaced all external Lucide CDN scripts with self-contained, inline SVG icons for instant offline load.
  - Implemented CSS native system font fallbacks to guarantee robust rendering behind firewalls/proxies.
- **Reverse Proxy Support**:
  - Optimized the SSE network layer to dynamically resolve paths based on the browser's current URL, fully supporting subpath routing under reverse proxies.
- **Performance Optimizations**:
  - Replaced regular Ticker HTML element updates with precise `.innerText` modifications, decreasing telemetry client-side rendering CPU load by over 90%.

## [0.1.7] - 2026-05-20

### Added

- Add HTTP Server and Real-time Web Dashboard (`-server` and `-port` flags)
- Stream real-time container metrics using Server-Sent Events (SSE) `/stream`
- Premium, interactive glassmorphic web interface:
  - CPU and Memory horizontal progress rows
  - Dynamic real-time SVG sparklines showing metrics history
  - Segmented status filter tabs (All, Running, Stopped) with live badges
  - Instant fuzzy search with matching query text highlighting
  - 3D hover card tilt with follow-mouse radial glow
  - Automatic JSON case normalization for cross-client compatibility

## [0.1.6] - 2026-03-19

### Fixes

- handle empty blkio and Names arrays to prevent panic

## [0.1.5] - 2026-02-06

### Added

- add blkio stats support

## [0.1.4] - 2026-02-06

### Added

- add network stats support

## [0.1.3] - 2026-02-05

### Added

- add update support

## [0.1.0] - 2026-01-13

### Added

- Initial release of DockerView-Go
- Real-time Docker container monitoring with terminal UI
- Cross-platform Docker host detection supporting:
  - Docker Desktop (macOS/Linux/Windows)
  - Colima (macOS)
  - OrbStack (macOS)
  - Podman (macOS/Linux/Windows)
  - Rancher Desktop (macOS/Windows)
  - Minikube (macOS/Linux)
- Color-coded container status (green for running, red for stopped)
- CPU usage alerts (highlighted when >50%)
- Memory usage statistics
- Auto-refresh every second
- GitHub Actions CI/CD pipeline:
  - Automated multi-platform builds (macOS, Linux, Windows)
  - Automatic release generation on git tags
- Unit tests for Docker client utilities
- Project documentation and README

### Changed

- Split main.go into separate files (main.go and model.go) for better maintainability
- Improved Docker connection detection with multiple fallback mechanisms

### Fixed

- Removed placeholder image from README
- Fixed import issues for cross-platform compilation
