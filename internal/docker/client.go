package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type ContainerInfo struct {
	ID      string
	Name    string
	Status  string
	CPU     string
	Memory  string
	Blkio   string
	Network string
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

func detectUnixSockets() []string {
	var sockets []string

	sockets = append(sockets, "/var/run/docker.sock")

	home, _ := os.UserHomeDir()
	if home != "" {
		sockets = append(sockets, home+"/.colima/default/docker.sock")
		sockets = append(sockets, home+"/.orbstack/run/docker.sock")

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

	if home, _ := os.UserHomeDir(); home != "" {
		sockets = append(sockets, home+"/.minikube/apiserver.sock")
	}

	return sockets
}

func GetContainerStats(ctx context.Context, cli *client.Client) ([]ContainerInfo, error) {
	containers, err := cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var result []ContainerInfo
	for _, c := range containers {
		statsData, err := cli.ContainerStatsOneShot(ctx, c.ID)
		if err != nil {
			continue
		}

		cpuPercent, memoryUsage, blkio, networks, err := parseStats(statsData.Body)
		statsData.Body.Close()
		if err != nil {
			continue
		}

		status := c.State
		if c.Status != "" {
			status = c.Status
		}

		result = append(result, ContainerInfo{
			ID:     c.ID[:12],
			Name:   c.Names[0][1:],
			Status: status,
			CPU:    fmt.Sprintf("%.1f%%", cpuPercent),
			Memory: memoryUsage,
			Blkio: func() string {
				return fmt.Sprintf("%s / %s", formatBytes(blkio[0].Value), formatBytes(blkio[1].Value))
			}(),
			Network: func() string {
				var totalRx, totalTx uint64
				for _, net := range networks {
					totalRx += net.RxBytes
					totalTx += net.TxBytes
				}
				return fmt.Sprintf("↓%s ↑%s",
					formatBytes(totalRx),
					formatBytes(totalTx))
			}(),
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

func parseStats(body io.Reader) (float64, string, []BlkioEntry, map[string]NetworkStats, error) {
	var stats statsJSON

	if err := json.NewDecoder(body).Decode(&stats); err != nil {
		return 0, "", nil, nil, err
	}

	var cpuPercent float64
	var memoryUsage string

	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)
	cpuCount := float64(stats.CPUStats.OnlineCPUs)

	if systemDelta > 0 && cpuDelta > 0 && cpuCount > 0 {
		cpuPercent = (cpuDelta / systemDelta) * cpuCount * 100.0
	}

	usage := stats.MemoryStats.Usage
	memoryUsage = formatBytes(usage)

	return cpuPercent, memoryUsage, stats.BlockIOStats.IOServiceBytesRecursive, stats.Networks, nil
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
