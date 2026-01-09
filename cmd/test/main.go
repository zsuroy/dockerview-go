package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"dockerview-go/internal/docker"
)

func main() {
	client, err := docker.NewClient()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		containers, err := docker.GetContainerStats(ctx, client)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Found %d containers:\n", len(containers))
			for _, c := range containers {
				fmt.Printf("  %s - %s - %s - %s\n", c.ID, c.Name, c.CPU, c.Memory)
			}
		}
		time.Sleep(time.Second)
	}
}
