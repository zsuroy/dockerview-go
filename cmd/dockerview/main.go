package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dockerview-go/internal/docker"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	showVersion := flag.Bool("version", false, "Show version and exit")
	showHelp := flag.Bool("help", false, "Show help and exit")
	flag.Parse()

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

	m := &model{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				containers, err := docker.GetContainerStats(ctx, client)
				m.mu.Lock()
				m.containers = containers
				m.err = err
				m.mu.Unlock()
			}
		}
	}()

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf("DockerView-Go v%s - A beautiful terminal-based Docker container monitoring tool\n\n", Version)
	fmt.Println("USAGE:")
	fmt.Println("  dockerview-go [OPTIONS]")
	fmt.Println()
	fmt.Println("OPTIONS:")
	fmt.Println("  -version, -help")
	fmt.Println("        Show this help message")
	fmt.Println("  -version")
	fmt.Println("        Show version information and exit")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Println("  dockerview-go")
	fmt.Println("  dockerview-go -version")
	fmt.Println()
	fmt.Println("CONTROLS:")
	fmt.Println("  Ctrl+C    Exit the application")
	fmt.Println()
	fmt.Println("DOCKER SOCKET:")
	fmt.Println("  DockerView-Go automatically detects Docker sockets.")
	fmt.Println("  You can also specify via DOCKER_HOST environment variable:")
	fmt.Println("  DOCKER_HOST=unix:///path/to/docker.sock dockerview-go")
	fmt.Println()
	fmt.Println("For more information, visit: https://github.com/zsuroy/dockerview-go")
}
