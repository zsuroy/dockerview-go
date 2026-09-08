package netview

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// LoadFixture reads a topology JSON document (the same shape served at
// GET /api/networks/topology) for offline acceptance under -no-docker.
func LoadFixture(path string) (*Topology, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("network fixture: %w", err)
	}
	var fx Topology
	if err := json.Unmarshal(data, &fx); err != nil {
		return nil, fmt.Errorf("network fixture %s: invalid JSON: %w", path, err)
	}
	if err := validateFixture(&fx); err != nil {
		return nil, fmt.Errorf("network fixture %s: %w", path, err)
	}
	if fx.GeneratedAt.IsZero() {
		fx.GeneratedAt = time.Now().UTC()
	}
	Normalize(&fx)
	return &fx, nil
}

func validateFixture(t *Topology) error {
	if t.Networks == nil {
		return fmt.Errorf("missing \"networks\" array")
	}
	seenNets := map[string]bool{}
	for i := range t.Networks {
		n := &t.Networks[i]
		if n.Name == "" {
			return fmt.Errorf("networks[%d]: name is required", i)
		}
		if seenNets[n.Name] {
			return fmt.Errorf("networks[%d]: duplicate network name %q", i, n.Name)
		}
		seenNets[n.Name] = true
		for j := range n.Containers {
			c := &n.Containers[j]
			if c.ID == "" || c.Name == "" {
				return fmt.Errorf("networks[%q].containers[%d]: id and name are required", n.Name, j)
			}
		}
	}
	return nil
}

// FixtureProvider serves a loaded topology as a Provider.
type FixtureProvider struct {
	t *Topology
}

// NewFixtureProvider loads the fixture and wraps it as a Provider.
func NewFixtureProvider(path string) (*FixtureProvider, error) {
	t, err := LoadFixture(path)
	if err != nil {
		return nil, err
	}
	return &FixtureProvider{t: t}, nil
}

// Topology implements Provider.
func (p *FixtureProvider) Topology(_ context.Context) (*Topology, error) {
	out := clone(p.t)
	return &out, nil
}

// clone returns a deep enough copy that callers cannot mutate the fixture.
func clone(t *Topology) Topology {
	nets := make([]Network, len(t.Networks))
	for i, n := range t.Networks {
		members := make([]ContainerMembership, len(n.Containers))
		copy(members, n.Containers)
		n.Containers = members
		nets[i] = n
	}
	return Topology{GeneratedAt: t.GeneratedAt, Networks: nets}
}
