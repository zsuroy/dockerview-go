# DockerView-Go Examples

Demo environments for showcasing DockerView-Go features.

## What's Inside

- `docker-compose.yml` — starts demo containers for monitoring
- `demo/nginx/index.html` — a simple web page served by the demo Nginx container

## Demo Containers

| Service | Image | Port | Purpose |
|---------|-------|------|---------|
| `demo-nginx` | `nginx:alpine` | `8081` | Static web server |
| `demo-redis` | `redis:7-alpine` | `6379` | In-memory datastore |
| `demo-api` | `hashicorp/http-echo` | `8082` | Simple HTTP API |
| `demo-busybox` | `busybox` | — | Idle container for comparison |

## Quick Start

### 1. Start the demo containers

```bash
cd examples
docker compose up -d
```

### 2. Run DockerView-Go locally

In a separate terminal, from the project root:

```bash
go run ./cmd/dockerview -server
```

Or use a pre-built binary:

```bash
./build/dockerview -server
```

### 3. Open the dashboard

Navigate to `http://localhost:8080` in your browser.

## Demo Scenarios

### Scenario A: Real-time Monitoring
1. Start all services with `docker compose up -d`
2. Run `dockerview -server`
3. Watch live CPU, memory, network, and block I/O metrics update every second

### Scenario B: Container Lifecycle
1. Stop a container: `docker compose stop demo-redis`
2. See it turn red in the dashboard in real time
3. Restart it: `docker compose start demo-redis`
4. Watch it recover

### Scenario C: Logs & Exec
1. Open the dashboard
2. Enter the admin token when prompted
3. View filtered logs from `demo-api`
4. Execute a command inside `demo-redis`

### Scenario D: Disk Cleanup (Prune)
1. Pull some unused images: `docker pull alpine:latest`
2. Go to the **PRUNE** tab in the dashboard
3. Preview candidates, confirm deletion

### Scenario E: Backup Snapshots
1. Go to the **BACKUPS** tab
2. Create a snapshot of the current container state
3. Download the zip archive

### Scenario F: Offline Mode (No Docker)
1. Run `dockerview -server -no-docker -fixture testdata/backup_fixture.json`
2. The dashboard shows mock data without a running Docker daemon

## Clean Up

```bash
# Stop demo containers
docker compose down

# Stop and remove volumes (optional)
docker compose down -v
```

## Offline Fixture

For testing without a Docker daemon, use the fixture file:

```bash
go run ./cmd/dockerview -server -no-docker -fixture testdata/backup_fixture.json
```

This starts the dashboard with mock container data, useful for UI development and CI.
