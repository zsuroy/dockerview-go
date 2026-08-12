package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

type PortMapping struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort uint16 `json:"private_port"`
	PublicPort  uint16 `json:"public_port,omitempty"`
	Type        string `json:"type"`
}

// deduplicatePorts removes duplicate port entries based on PrivatePort, PublicPort, and Type.
func deduplicatePorts(ports []PortMapping) []PortMapping {
	seen := make(map[[3]any]bool)
	var result []PortMapping
	for _, p := range ports {
		key := [3]any{p.PrivatePort, p.PublicPort, p.Type}
		if !seen[key] {
			seen[key] = true
			result = append(result, p)
		}
	}
	return result
}

// sortPorts returns ports in a stable display order.
func sortPorts(ports []PortMapping) []PortMapping {
	sort.SliceStable(ports, func(i, j int) bool {
		if ports[i].PrivatePort != ports[j].PrivatePort {
			return ports[i].PrivatePort < ports[j].PrivatePort
		}
		if ports[i].PublicPort != ports[j].PublicPort {
			return ports[i].PublicPort < ports[j].PublicPort
		}
		if ports[i].Type != ports[j].Type {
			return ports[i].Type < ports[j].Type
		}
		return ports[i].IP < ports[j].IP
	})
	return ports
}

type ContainerInfo struct {
	FullID        string
	ID            string
	Name          string
	Status        string
	CPU           string
	Memory        string
	MemoryPercent float64 `json:",omitempty"`
	MemoryLimit   string  `json:",omitempty"`
	Blkio         string
	Network       string
	HealthScore   int           `json:",omitempty"`
	HealthStatus  HealthStatus  `json:",omitempty"`
	Ports         []PortMapping `json:"ports"`
}

func NewClient() (*client.Client, error) {
	if dockerHost := os.Getenv("DOCKER_HOST"); dockerHost != "" {
		if cli, err := tryConnect(dockerHost); err == nil {
			return cli, nil
		}
	}
	if runtime.GOOS == "windows" {
		hosts := []string{
			"//./pipe/docker_engine",
			"//./pipe/docker_engine_wsl",
			"//./pipe/podman",
			"//./pipe/rancher-desktop",
		}
		for _, host := range hosts {
			if cli, err := tryConnect(host); err == nil {
				return cli, nil
			}
		}
	} else {
		sockets := detectUnixSockets()
		for _, socket := range sockets {
			if cli, err := tryConnect("unix://" + socket); err == nil {
				return cli, nil
			}
		}
	}

	if cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	); err == nil {
		if _, err = cli.Ping(context.Background()); err == nil {
			return cli, nil
		}
	}

	return nil, fmt.Errorf("no docker daemon found")
}

// NewClientMaybeSkipped honors the -no-docker flag: when skip is true it
// returns (nil, nil) instead of probing for a daemon. This enables offline
// verification of features (like backup) that accept fixture data.
func NewClientMaybeSkipped(skip bool) (*client.Client, error) {
	if skip {
		return nil, nil
	}
	return NewClient()
}

func detectUnixSockets() []string {
	var sockets []string

	sockets = append(sockets, "/var/run/docker.sock")

	home, _ := os.UserHomeDir()
	if home != "" {
		sockets = append(sockets, home+"/.colima/default/docker.sock")
		sockets = append(sockets, home+"/.orbstack/run/docker.sock")
		sockets = append(sockets, home+"/.minikube/apiserver.sock")

		if runtime.GOOS == "darwin" {
			sockets = append(sockets, home+"/Library/Containers/rancher-desktop/Data/docker.sock")
			sockets = append(sockets, home+"/Library/Containers/com.redhat.podman/Data/docker.sock")
		}
	}

	if runtime.GOOS == "linux" {
		sockets = append(sockets, "/run/podman/podman.sock")
		if uid := os.Getuid(); uid != 0 {
			sockets = append(sockets, fmt.Sprintf("/run/user/%d/podman/podman.sock", uid))
		}
	}

	return sockets
}

var (
	stoppedByUs = make(map[string]bool)
	stoppedMu   sync.RWMutex
)

func TrackStopped(containerID string) {
	stoppedMu.Lock()
	stoppedByUs[containerID] = true
	stoppedMu.Unlock()
}

func UntrackStopped(containerID string) {
	stoppedMu.Lock()
	delete(stoppedByUs, containerID)
	stoppedMu.Unlock()
}

func GetStoppedIDs() []string {
	stoppedMu.RLock()
	defer stoppedMu.RUnlock()
	var ids []string
	for id := range stoppedByUs {
		ids = append(ids, id)
	}
	return ids
}

func GetContainerStats(ctx context.Context, cli *client.Client) ([]ContainerInfo, error) {
	containers, err := cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	runningMap := make(map[string]bool)
	for _, c := range containers {
		runningMap[c.ID] = true
	}

	// Clean up stoppedByUs if any of those containers are now running
	stoppedIDs := GetStoppedIDs()
	for _, id := range stoppedIDs {
		if runningMap[id] {
			UntrackStopped(id)
		}
	}

	var result []ContainerInfo
	for _, c := range containers {
		cpuPercent := 0.0
		memoryPercent := 0.0
		memoryUsage := "0 B"
		var memoryLimit uint64
		blkioStr := "N/A"
		networkStr := "N/A"
		var blkio []BlkioEntry
		var networks map[string]NetworkStats

		statsData, err := cli.ContainerStatsOneShot(ctx, c.ID)
		if err == nil {
			var parseErr error
			cpuPercent, memoryPercent, memoryUsage, memoryLimit, blkio, networks, parseErr = parseStats(statsData.Body)
			statsData.Body.Close()
			if parseErr == nil {
				if len(blkio) >= 2 {
					blkioStr = fmt.Sprintf("%s / %s", formatBytes(blkio[0].Value), formatBytes(blkio[1].Value))
				} else if len(blkio) == 1 {
					blkioStr = fmt.Sprintf("%s / 0 B", formatBytes(blkio[0].Value))
				}
				var totalRx, totalTx uint64
				for _, net := range networks {
					totalRx += net.RxBytes
					totalTx += net.TxBytes
				}
				networkStr = fmt.Sprintf("↓%s ↑%s",
					formatBytes(totalRx),
					formatBytes(totalTx))
			}
		}

		status := c.State
		if c.Status != "" {
			status = c.Status
		}

		isRunning := c.State == "running"

		// Get restart count and uptime from container inspect
		restartCount := 0
		var uptime time.Duration
		if inspect, err := cli.ContainerInspect(ctx, c.ID); err == nil {
			if inspect.RestartCount > 0 {
				restartCount = inspect.RestartCount
			}
			if inspect.State != nil && inspect.State.StartedAt != "" && isRunning {
				if startedAt, err := time.Parse(time.RFC3339Nano, inspect.State.StartedAt); err == nil {
					uptime = time.Since(startedAt)
				}
			}
		}
		// Fallback: use creation time if uptime not yet set
		if uptime <= 0 && c.Created > 0 {
			uptime = time.Since(time.Unix(c.Created, 0))
		}

		// Calculate disk IO rate (bytes per second)
		var totalBlkioBytes uint64
		for _, entry := range blkio {
			if entry.Op == "Read" || entry.Op == "Write" {
				totalBlkioBytes += entry.Value
			}
		}
		diskIORate := CalculateAverageRate(totalBlkioBytes, uptime)

		// Calculate network rate (bytes per second)
		var totalNetBytes uint64
		for _, net := range networks {
			totalNetBytes += net.RxBytes + net.TxBytes
		}
		networkRate := CalculateAverageRate(totalNetBytes, uptime)

		// Calculate health score
		healthResult := CalculateHealthScore(
			cpuPercent,
			memoryPercent,
			diskIORate,
			networkRate,
			restartCount,
			uptime.Seconds(),
			isRunning,
		)

		var ports []PortMapping
		for _, p := range c.Ports {
			ports = append(ports, PortMapping{
				IP:          p.IP,
				PrivatePort: p.PrivatePort,
				PublicPort:  p.PublicPort,
				Type:        p.Type,
			})
		}

		result = append(result, ContainerInfo{
			FullID:        c.ID,
			ID:            truncateID(c.ID, 12),
			Name:          extractContainerName(c.Names),
			Status:        status,
			CPU:           fmt.Sprintf("%.1f%%", cpuPercent),
			Memory:        memoryUsage,
			MemoryPercent: memoryPercent,
			MemoryLimit:   formatBytes(memoryLimit),
			Blkio:         blkioStr,
			Network:       networkStr,
			HealthScore:   healthResult.Score,
			HealthStatus:  healthResult.Status,
			Ports:         sortPorts(deduplicatePorts(ports)),
		})
	}

	// Append containers stopped during this execution
	stoppedIDs = GetStoppedIDs()
	for _, id := range stoppedIDs {
		inspect, err := cli.ContainerInspect(ctx, id)
		if err != nil {
			// Container might be deleted, remove from tracking
			UntrackStopped(id)
			continue
		}

		if inspect.State != nil && inspect.State.Running {
			UntrackStopped(id)
			continue
		}

		status := "exited"
		var uptime time.Duration
		restartCount := 0
		if inspect.State != nil {
			if inspect.State.Status != "" {
				status = inspect.State.Status
			} else if inspect.State.ExitCode != 0 {
				status = fmt.Sprintf("exited (%d)", inspect.State.ExitCode)
			}
			if inspect.State.StartedAt != "" {
				if startedAt, err := time.Parse(time.RFC3339Nano, inspect.State.StartedAt); err == nil {
					if finishedAt, err := time.Parse(time.RFC3339Nano, inspect.State.FinishedAt); err == nil {
						uptime = finishedAt.Sub(startedAt)
					}
				}
			}
			restartCount = inspect.RestartCount
		}
		if uptime <= 0 && inspect.Created != "" {
			if created, err := time.Parse(time.RFC3339Nano, inspect.Created); err == nil {
				uptime = time.Since(created)
			}
		}

		// Calculate health score for stopped container
		healthResult := CalculateHealthScore(
			0.0,
			0.0,
			0.0,
			0.0,
			restartCount,
			uptime.Seconds(),
			false,
		)

		var ports []PortMapping
		if inspect.NetworkSettings != nil && inspect.NetworkSettings.Ports != nil {
			for port, bindings := range inspect.NetworkSettings.Ports {
				parts := strings.Split(string(port), "/")
				var privPort uint16
				proto := "tcp"
				if len(parts) > 0 {
					if p, err := strconv.Atoi(parts[0]); err == nil {
						privPort = uint16(p)
					}
				}
				if len(parts) > 1 {
					proto = parts[1]
				}

				if len(bindings) == 0 {
					ports = append(ports, PortMapping{
						PrivatePort: privPort,
						Type:        proto,
					})
				} else {
					for _, b := range bindings {
						var pubPort uint16
						if p, err := strconv.Atoi(b.HostPort); err == nil {
							pubPort = uint16(p)
						}
						ports = append(ports, PortMapping{
							IP:          b.HostIP,
							PrivatePort: privPort,
							PublicPort:  pubPort,
							Type:        proto,
						})
					}
				}
			}
		}

		result = append(result, ContainerInfo{
			FullID:       inspect.ID,
			ID:           truncateID(inspect.ID, 12),
			Name:         extractContainerName([]string{inspect.Name}),
			Status:       status,
			CPU:          "0.0%",
			Memory:       "0 B",
			Blkio:        "N/A",
			Network:      "N/A",
			HealthScore:  healthResult.Score,
			HealthStatus: healthResult.Status,
			Ports:        sortPorts(deduplicatePorts(ports)),
		})
	}

	return result, nil
}

type BlkioEntry struct {
	Major int    `json:"major"`
	Minor int    `json:"minor"`
	Op    string `json:"op"`
	Value uint64 `json:"value"`
}
type NetworkStats struct {
	RxBytes   uint64 `json:"rx_bytes"`
	RxPackets uint64 `json:"rx_packets"`
	TxBytes   uint64 `json:"tx_bytes"`
	TxPackets uint64 `json:"tx_packets"`
}

type statsJSON struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64  `json:"system_cpu_usage"`
		OnlineCPUs  float64 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
	} `json:"memory_stats"`
	BlockIOStats struct {
		IOServiceBytesRecursive []BlkioEntry `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
	Networks map[string]NetworkStats `json:"networks"`
}

func parseStats(body io.Reader) (float64, float64, string, uint64, []BlkioEntry, map[string]NetworkStats, error) {
	var stats statsJSON

	if err := json.NewDecoder(body).Decode(&stats); err != nil {
		return 0, 0, "", 0, nil, nil, err
	}

	var cpuPercent float64
	var memoryPercent float64
	var memoryUsage string

	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)
	cpuCount := float64(stats.CPUStats.OnlineCPUs)

	if systemDelta > 0 && cpuDelta > 0 && cpuCount > 0 {
		cpuPercent = (cpuDelta / systemDelta) * cpuCount * 100.0
	}

	usage := stats.MemoryStats.Usage
	limit := stats.MemoryStats.Limit
	if limit > 0 {
		memoryPercent = float64(usage) / float64(limit) * 100.0
	}
	memoryUsage = formatBytes(usage)

	return cpuPercent, memoryPercent, memoryUsage, limit, stats.BlockIOStats.IOServiceBytesRecursive, stats.Networks, nil
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func tryConnect(host string) (*client.Client, error) {
	cli, err := client.NewClientWithOpts(
		client.WithHost(host),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}

	_, err = cli.Ping(context.Background())
	if err != nil {
		cli.Close()
		return nil, err
	}

	return cli, nil
}

func extractContainerName(names []string) string {
	if len(names) == 0 || len(names[0]) == 0 {
		return ""
	}
	name := names[0]
	if name[0] == '/' {
		return name[1:]
	}
	return name
}

func truncateID(id string, length int) string {
	if len(id) >= length {
		return id[:length]
	}
	return id
}

func ContainerOp(ctx context.Context, cli *client.Client, containerID, op string) error {
	switch op {
	case "start":
		err := cli.ContainerStart(ctx, containerID, container.StartOptions{})
		if err == nil {
			UntrackStopped(containerID)
		}
		return err
	case "stop":
		err := cli.ContainerStop(ctx, containerID, container.StopOptions{})
		if err == nil {
			TrackStopped(containerID)
		}
		return err
	case "restart":
		err := cli.ContainerRestart(ctx, containerID, container.StopOptions{})
		if err == nil {
			UntrackStopped(containerID)
		}
		return err
	default:
		return fmt.Errorf("unknown operation: %s", op)
	}
}

func GetContainerLogs(ctx context.Context, cli *client.Client, containerID, tail string) (io.ReadCloser, error) {
	inspect, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, err
	}

	reader, err := cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
		Follow:     false,
	})
	if err != nil {
		return nil, err
	}

	if inspect.Config.Tty {
		return reader, nil
	}

	defer reader.Close()
	var buf bytes.Buffer
	_, err = stdcopy.StdCopy(&buf, &buf, reader)
	if err != nil {
		return nil, err
	}

	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

// ExecResult holds the result of command execution
type ExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

const (
	DefaultExecTimeout   = 30 * time.Second
	MaxExecOutputBytes   = 256 * 1024
	execTruncatedMessage = "\n[output truncated: exceeded 256KB limit]"
	execTimeoutMessage   = "\n[command timed out after 30s]"
)

type cappedBuffer struct {
	buf       bytes.Buffer
	remaining int64
	truncated bool
}

func newCappedBuffer(limit int64) *cappedBuffer {
	return &cappedBuffer{remaining: limit}
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
		b.truncated = true
	}
	n, err := b.buf.Write(p)
	b.remaining -= int64(n)
	return n, err
}

func (b *cappedBuffer) String() string {
	return b.buf.String()
}

// ContainerExec executes a command inside a running container and returns stdout/stderr.
func ContainerExec(ctx context.Context, cli *client.Client, containerID string, cmd []string) (ExecResult, error) {
	var result ExecResult

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultExecTimeout)
		defer cancel()
	}

	options := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	}

	resp, err := cli.ContainerExecCreate(ctx, containerID, options)
	if err != nil {
		return result, err
	}

	attachResp, err := cli.ContainerExecAttach(ctx, resp.ID, container.ExecStartOptions{})
	if err != nil {
		return result, err
	}
	defer attachResp.Close()

	stdout := newCappedBuffer(MaxExecOutputBytes)
	stderr := newCappedBuffer(MaxExecOutputBytes)

	copyDone := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(stdout, stderr, attachResp.Reader)
		copyDone <- err
	}()

	select {
	case <-ctx.Done():
		attachResp.Close()
		result.Stderr = stderr.String() + execTimeoutMessage
		result.Stdout = stdout.String()
		return result, ctx.Err()
	case err := <-copyDone:
		if err != nil {
			return result, err
		}
	}

	inspectResp, err := cli.ContainerExecInspect(ctx, resp.ID)
	if err != nil {
		return result, err
	}

	result.ExitCode = inspectResp.ExitCode
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	if stdout.truncated || stderr.truncated {
		if result.Stderr == "" {
			result.Stderr = execTruncatedMessage
		} else {
			result.Stderr += execTruncatedMessage
		}
	}

	return result, nil
}
