// DTO for GET /api/networks/topology. Field names mirror the Go DTO in
// internal/netview/topology.go (snake_case JSON tags).

export interface ContainerMembership {
  id: string;
  name: string;
  state: string;
  status: string;
  running: boolean;
  ipv4: string;
  ipv6: string;
}

export interface NetworkView {
  id: string;
  name: string;
  driver: string;
  scope: string;
  internal: boolean;
  container_count: number;
  containers: ContainerMembership[];
}

export interface NetworkTopology {
  generated_at: string;
  networks: NetworkView[];
}

export interface ApiErrorBody {
  error?: string;
  error_description?: string;
}

// Per-network attachment of a graph node, used by the detail card.
export interface NodeMember extends ContainerMembership {
  network: string;
}

// Graph node: one per unique container ID. A container attached to several
// networks has multiple members but still renders as one node.
export interface GraphNode {
  id: string;
  name: string;
  state: string;
  status: string;
  running: boolean;
  networks: string[];
  members: NodeMember[];
  x?: number;
  y?: number;
  fx?: number | null;
  fy?: number | null;
}

export interface GraphLink {
  id: string;
  source: string | GraphNode;
  target: string | GraphNode;
  network: string;
  sourceName: string;
  targetName: string;
}

export interface GraphGroup {
  name: string;
  driver: string;
  scope: string;
  internal: boolean;
  count: number;
  empty: boolean;
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface GraphLayout {
  nodes: GraphNode[];
  links: GraphLink[];
  groups: GraphGroup[];
  width: number;
  height: number;
}
