# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
