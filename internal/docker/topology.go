// Network topology read-only adapter. It is the ONLY place the network
// feature touches the Docker SDK: it lists networks (empty nets included),
// inspects each network for its endpoint map, and joins a single
// ContainerList(All:true) snapshot for run state. The assembled model lives
// in internal/netview and never imports the SDK.
//
// Official API facts behind the calls:
//   - https://docs.docker.com/reference/api/engine/version/v1.53/
//     GET /networks (NetworkList) does NOT propagate the attached-container
//     list since API 1.28, so members come from GET /networks/{id}
//     (NetworkInspect).
//   - https://d3js.org/d3-force is used only on the web side for layout;
//     it never calls Docker.
//
// The Engine also exposes NetworkDelete/connect/disconnect. This adapter
// does not wrap them on purpose: the topology feature is read-only.
package docker

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"

	"github.com/zsuroy/dockerview-go/internal/netview"
)

// TopologyClient is the narrow SDK surface the topology feature uses.
// *client.Client satisfies it; tests inject a fake.
type TopologyClient interface {
	NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error)
	NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error)
	ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
}

// TopologyProvider builds netview.Topology snapshots from a Docker daemon.
type TopologyProvider struct {
	cli TopologyClient
}

// NewTopologyProvider wires a topology provider.
func NewTopologyProvider(cli TopologyClient) *TopologyProvider {
	return &TopologyProvider{cli: cli}
}

// Topology implements netview.Provider. Any daemon error fails the whole
// snapshot: the UI shows an explicit error instead of a partial graph.
func (p *TopologyProvider) Topology(ctx context.Context) (*netview.Topology, error) {
	summaries, err := p.cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("network list: %w", err)
	}

	// One all-containers snapshot supplies running/stopped state, including
	// stopped containers whose endpoints still appear in network inspect.
	containers, err := p.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("container list: %w", err)
	}
	states := make(map[string]container.Summary, len(containers))
	for _, c := range containers {
		states[c.ID] = c
	}

	sort.SliceStable(summaries, func(i, j int) bool { return summaries[i].Name < summaries[j].Name })

	nets := make([]netview.Network, 0, len(summaries))
	for _, s := range summaries {
		ins, err := p.cli.NetworkInspect(ctx, s.ID, network.InspectOptions{})
		if err != nil {
			return nil, fmt.Errorf("network inspect %q: %w", s.Name, err)
		}
		nets = append(nets, networkFromInspect(ins, states))
	}

	t := &netview.Topology{GeneratedAt: time.Now().UTC(), Networks: nets}
	netview.Normalize(t)
	return t, nil
}

func networkFromInspect(ins network.Inspect, states map[string]container.Summary) netview.Network {
	members := make([]netview.ContainerMembership, 0, len(ins.Containers))
	for id, ep := range ins.Containers {
		member := netview.ContainerMembership{
			ID:    id,
			Name:  strings.TrimPrefix(ep.Name, "/"),
			State: "unknown",
			IPv4:  canonicalIP(ep.IPv4Address),
			IPv6:  canonicalIP(ep.IPv6Address),
		}
		if c, ok := states[id]; ok {
			member.Name = extractContainerName(c.Names)
			member.State = string(c.State)
			member.Status = c.Status
			member.Running = c.State == "running"
		}
		members = append(members, member)
	}
	return netview.Network{
		ID:         ins.ID,
		Name:       ins.Name,
		Driver:     ins.Driver,
		Scope:      ins.Scope,
		Internal:   ins.Internal,
		Containers: members,
	}
}

// canonicalIP trims the CIDR suffix the daemon reports for endpoints
// ("172.18.0.2/16" -> "172.18.0.2"). Empty stays empty so the UI can render
// the literal "empty".
func canonicalIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if i := strings.IndexByte(ip, '/'); i >= 0 {
		ip = ip[:i]
	}
	return ip
}
