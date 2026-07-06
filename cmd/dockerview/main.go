package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zsuroy/dockerview-go/internal/docker"
	"github.com/zsuroy/dockerview-go/internal/server"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	updateTolatest := flag.Bool("update", false, "update to the latest")
	showVersion := flag.Bool("version", false, "Show version")
	showHelp := flag.Bool("help", false, "Show help")
	enableServer := flag.Bool("server", false, "Enable HTTP server for real-time data")
	serverPort := flag.Int("port", 8080, "Port for HTTP server")
	serverToken := flag.String("token", "", "Security token for HTTP server (auto-generated if empty)")
	flag.Parse()

	SetColor()

	if *updateTolatest {
		doUpdate()
		os.Exit(0)
	}

	if *showVersion {
		fmt.Printf("DockerView-Go %s\n", Version)
		fmt.Printf("Commit: %s\n", Commit)
		fmt.Printf("Built: %s\n", Date)
		os.Exit(0)
	}

	if *showHelp {
		printHelp()
		os.Exit(0)
	}

	client, err := docker.NewClient()
	if err != nil {
		fmt.Printf("Failed to connect to Docker: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var srv *server.Server
	if *enableServer {
		token := *serverToken
		if token == "" {
			token = os.Getenv("DOCKERVIEW_TOKEN")
		}
		if token == "" {
			bytes := make([]byte, 16)
			if _, err := rand.Read(bytes); err == nil {
				token = hex.EncodeToString(bytes)
			} else {
				token = "dockerview-secure-default"
			}
		}

		fmt.Printf("[INFO] Web dashboard is running at http://localhost:%d/?token=%s\n", *serverPort, token)
		fmt.Printf("[INFO] Security token: %s\n", token)

		srv = server.NewServer(client, token, Version, Commit, Date)
		go func() {
			if err := srv.Start(ctx, *serverPort); err != nil {
				fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
			}
		}()
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	// In server mode without a TTY, run headless (no TUI)
	if *enableServer && !isTTY() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				containers, err := docker.GetContainerStats(ctx, client)
				if err == nil && srv != nil {
					srv.UpdateData(containers)
				}
			}
		}
	}

	m := &model{dockerClient: client}

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				containers, err := docker.GetContainerStats(ctx, client)
				m.mu.Lock()
				m.containers = containers
				m.err = err
				m.mu.Unlock()

				if srv != nil && err == nil {
					srv.UpdateData(containers)
				}
			}
		}
	}()

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func printHelp() {
	fmt.Printf("DockerView %s - A beautiful terminal-based Docker container monitoring tool\n\n", Version)
	fmt.Println("USAGE:")
	fmt.Println("  dockerview [OPTIONS]")
	fmt.Println()
	fmt.Println("OPTIONS:")
	fmt.Println("  -update")
	fmt.Println("        Update to the latest")
	fmt.Println("  -server")
	fmt.Println("        Enable HTTP server for real-time data")
	fmt.Println("  -port int")
	fmt.Println("        Port for HTTP server (default 8080)")
	fmt.Println("  -token string")
	fmt.Println("        Security token for HTTP server (auto-generated if empty)")
	fmt.Println("  -help")
	fmt.Println("        Show this help message")
	fmt.Println("  -version")
	fmt.Println("        Show version information")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Println("  dockerview")
	fmt.Println("  dockerview -version")
	fmt.Println()
	fmt.Println("CONTROLS:")
	fmt.Println("  ↑/↓       Select container")
	fmt.Println("  Enter     Show actions")
	fmt.Println("  s         Start container")
	fmt.Println("  x         Stop container")
	fmt.Println("  r         Restart container")
	fmt.Println("  l         View logs")
	fmt.Println("  e         Execute command")
	fmt.Println("  q/Esc     Back / Exit")
	fmt.Println("  Ctrl+C    Exit application")
	fmt.Println()
	fmt.Println("DOCKER SOCKET:")
	fmt.Println("  DockerView automatically detects Docker sockets.")
	fmt.Println("  You can also specify via DOCKER_HOST environment variable:")
	fmt.Println("  DOCKER_HOST=unix:///path/to/docker.sock dockerview")
	fmt.Println()
	fmt.Println("For more information, visit: https://github.com/zsuroy/dockerview-go")
}
