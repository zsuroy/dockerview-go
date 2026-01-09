package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"io"
	"os"
	"os/exec"
	"strings"
)

type ContainerInfo struct {
	ID     string
	Name   string
	Status string
	CPU    string
	Memory string
}

func NewClient() (*client.Client, error) {
	dockerHost := os.Getenv("DOCKER_HOST")
	if dockerHost == "" {
		if path := detectColimaSocket(); path != "" {
			dockerHost = "unix://" + path
			os.Setenv("DOCKER_HOST", dockerHost)
		}
	}

	cli, err := client.NewClientWithOpts(
		client.WithHost(dockerHost),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}
	if _, err = cli.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("docker daemon not accessible: %w", err)
	}
	return cli, nil
}

func detectColimaSocket() string {
	home, _ := os.UserHomeDir()
	colimaSocket := home + "/.colima/default/docker.sock"
	if _, err := os.Stat(colimaSocket); err == nil {
		return colimaSocket
	}
	if out, _ := exec.Command("docker", "context", "ls").Output(); strings.Contains(string(out), "colima") {
		if out, _ := exec.Command("docker", "context", "inspect", "colima").Output(); len(out) > 0 {
			var contexts []map[string]interface{}
			json.Unmarshal(out, &contexts)
			if len(contexts) > 0 {
				if endpoint, ok := contexts[0]["Endpoints"].(map[string]interface{})["docker"]; ok {
					if ep, ok := endpoint.(map[string]interface{})["Host"]; ok {
						host := fmt.Sprintf("%v", ep)
						return strings.TrimPrefix(host, "unix://")
					}
				}
			}
		}
	}
	return ""
}

func GetContainerStats(ctx context.Context, cli *client.Client) ([]ContainerInfo, error) {
	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var result []ContainerInfo
	for _, c := range containers {
		stats, err := cli.ContainerStatsOneShot(ctx, c.ID)
		if err != nil {
			continue
		}

		cpuPercent, memoryUsage, err := parseStats(stats.Body)
		stats.Body.Close()
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
		})
	}

	return result, nil
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
}

func parseStats(body io.Reader) (float64, string, error) {
	var stats statsJSON
	if err := json.NewDecoder(body).Decode(&stats); err != nil {
		return 0, "", err
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

	return cpuPercent, memoryUsage, nil
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
