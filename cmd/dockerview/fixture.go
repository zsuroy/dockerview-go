package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/zsuroy/dockerview-go/internal/docker"
)

// loadContainerFixture reads a JSON array of container snapshots.
//
// Under -no-docker there is no daemon to poll, so the dashboard snapshot stays
// empty. That is fine for the container table, but everything downstream of the
// snapshot inherits the emptiness — including DUTY's previewRestart/previewStop
// tools, which resolve the container through the snapshot before they can
// propose anything. Seeding one snapshot makes the whole path exercisable
// offline, which is what -no-docker acceptance needs.
func loadContainerFixture(path string) ([]docker.ContainerInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("container fixture: %w", err)
	}
	var out []docker.ContainerInfo
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("container fixture %s: invalid JSON: %w", path, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("container fixture %s: empty array", path)
	}
	for i := range out {
		if out[i].Name == "" {
			return nil, fmt.Errorf("container fixture %s: containers[%d] has no name", path, i)
		}
	}
	return out, nil
}
