// Package netview holds the read-only network topology model: the DTO served
// at GET /api/networks/topology, the offline fixture loader, and the small
// set of pure helpers shared by the web backend and the TUI summary.
//
// Design invariants:
//   - the model never imports the Docker SDK; daemon access stays behind the
//     narrow TopologyClient interface in internal/docker.
//   - every network returned by NetworkList is represented, even when it has
//     zero containers (empty nets must render and export).
//   - a container attached to several networks is one identity (by ID) with
//     several memberships; the UI merges it into one node.
//   - the model is read-only: there are no create/delete/connect helpers.
package netview

import (
	"context"
	"sort"
	"strings"
	"time"
)

// Topology is the response body of GET /api/networks/topology.
type Topology struct {
	// GeneratedAt is when the snapshot was assembled by the backend.
	GeneratedAt time.Time `json:"generated_at"`
	// Networks is one entry per Docker network, sorted by name.
	Networks []Network `json:"networks"`
}

// Network describes one Docker network and its container endpoints.
type Network struct {
	// ID is the engine network ID.
	ID string `json:"id"`
	// Name is the human network name (bridge, net-used, ...).
	Name string `json:"name"`
	// Driver is the network driver (bridge, overlay, host, ...).
	Driver string `json:"driver"`
	// Scope is local, swarm or global.
	Scope string `json:"scope"`
	// Internal marks networks that cannot talk to the outside world.
	Internal bool `json:"internal"`
	// ContainerCount equals len(Containers); it is recomputed by Normalize.
	ContainerCount int `json:"container_count"`
	// Containers is the network's member endpoints, sorted by name. An
	// empty-but-non-nil slice marks an explicitly empty network.
	Containers []ContainerMembership `json:"containers"`
}

// ContainerMembership is one container endpoint as seen from a network.
// The same container ID may appear in several networks.
type ContainerMembership struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Running bool   `json:"running"`
	// IPv4/IPv6 are endpoint addresses; empty string renders as "empty".
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
}

// Provider is implemented by the daemon-backed adapter in internal/docker and
// by the offline fixture loader. The HTTP layer depends only on this.
type Provider interface {
	Topology(ctx context.Context) (*Topology, error)
}

// NetworkCount is one row of the TUI read-only summary (name + counts).
type NetworkCount struct {
	Name     string
	Driver   string
	Scope    string
	Total    int
	Running  int
	Stopped  int
	Internal bool
}

// Counts returns one NetworkCount per network, sorted by name. The TUI
// formats it; web and drill tests assert against the same pure function.
func (t *Topology) Counts() []NetworkCount {
	out := make([]NetworkCount, 0, len(t.Networks))
	for _, n := range t.Networks {
		row := NetworkCount{
			Name:     n.Name,
			Driver:   n.Driver,
			Scope:    n.Scope,
			Total:    len(n.Containers),
			Internal: n.Internal,
		}
		for _, c := range n.Containers {
			if c.Running {
				row.Running++
			} else {
				row.Stopped++
			}
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Normalize makes a topology deterministic: non-nil empty member slices,
// counts recomputed from memberships, running flags reconciled with state,
// and stable name ordering. Both the daemon adapter and the fixture loader
// pass through it before serving.
func Normalize(t *Topology) {
	if t == nil {
		return
	}
	for i := range t.Networks {
		n := &t.Networks[i]
		if n.Containers == nil {
			n.Containers = []ContainerMembership{}
		}
		for j := range n.Containers {
			c := &n.Containers[j]
			c.IPv4 = strings.TrimSpace(c.IPv4)
			c.IPv6 = strings.TrimSpace(c.IPv6)
			if c.State != "" {
				c.Running = strings.EqualFold(c.State, "running")
			}
		}
		sort.SliceStable(n.Containers, func(a, b int) bool {
			if n.Containers[a].Name == n.Containers[b].Name {
				return n.Containers[a].ID < n.Containers[b].ID
			}
			return n.Containers[a].Name < n.Containers[b].Name
		})
		n.ContainerCount = len(n.Containers)
	}
	sort.SliceStable(t.Networks, func(i, j int) bool {
		return t.Networks[i].Name < t.Networks[j].Name
	})
}

// ContainerNode is the web graph's per-container view: one per container ID,
// with every network attachment (and that attachment's IP) listed.
type ContainerNode struct {
	ID      string
	Name    string
	State   string
	Status  string
	Running bool
	Members []ContainerAttachment
}

// ContainerAttachment joins a container to one network.
type ContainerAttachment struct {
	Network string
	IPv4    string
	IPv6    string
}

// Nodes collapses the network-centric DTO into unique container nodes.
// Nodes are sorted by name; members of two networks appear exactly once.
func (t *Topology) Nodes() []ContainerNode {
	byID := map[string]*ContainerNode{}
	var order []string
	for _, n := range t.Networks {
		for _, c := range n.Containers {
			node, ok := byID[c.ID]
			if !ok {
				node = &ContainerNode{
					ID:      c.ID,
					Name:    c.Name,
					State:   c.State,
					Status:  c.Status,
					Running: c.Running,
				}
				byID[c.ID] = node
				order = append(order, c.ID)
			} else if node.Running && !c.Running {
				// Keep the strongest state label consistent across memberships.
				node.Running = false
				if c.State != "" {
					node.State = c.State
					node.Status = c.Status
				}
			}
			node.Members = append(node.Members, ContainerAttachment{
				Network: n.Name,
				IPv4:    c.IPv4,
				IPv6:    c.IPv6,
			})
		}
	}
	out := make([]ContainerNode, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// EdgeKey returns the deterministic data-edge key for an unordered pair of
// containers on one network: "<network>|<nameA>|<nameB>" with the names
// sorted. The web graph uses it for data-edge and the drill greps it.
func EdgeKey(network, nameA, nameB string) string {
	if nameA > nameB {
		nameA, nameB = nameB, nameA
	}
	return network + "|" + nameA + "|" + nameB
}
