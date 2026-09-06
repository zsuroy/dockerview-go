package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zsuroy/dockerview-go/internal/docker"
)

// contextWithTimeout returns a context with a timeout, derived from the
// request context.
func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

var errDutyDockerUnavailable = errors.New("docker client not available")

func errDutyContainerNotFound(id string) error {
	return errors.New("container not found: " + id)
}

func errDutyUnknownTemplate(t string) error {
	return errors.New("unknown exec template: " + t)
}

// resolveContainerID maps a 12-char short id or name to the full container
// ID using the latest snapshot.
func resolveContainerID(data []docker.ContainerInfo, idOrName string) string {
	for _, c := range data {
		if c.FullID == idOrName || c.ID == idOrName || c.Name == idOrName {
			return c.FullID
		}
	}
	// Prefix match on short id.
	for _, c := range data {
		if len(idOrName) >= 4 && (strings.HasPrefix(c.ID, idOrName) || strings.HasPrefix(c.FullID, idOrName)) {
			return c.FullID
		}
	}
	return ""
}

// lookupNameFromData returns the container name from the snapshot, or "".
func lookupNameFromData(data []docker.ContainerInfo, fullID string) string {
	for _, c := range data {
		if c.FullID == fullID {
			return c.Name
		}
	}
	return ""
}
