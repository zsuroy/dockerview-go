// Synchronous force layout. d3-force only computes positions
// (https://d3js.org/d3-force): React renders the SVG. The simulation is stopped and ticked to convergence
// deterministically, so there are no frame animations and export matches
// what is on screen.
import {
  forceCollide,
  forceLink,
  forceManyBody,
  forceSimulation,
  forceX,
  forceY,
  type SimulationLinkDatum,
  type SimulationNodeDatum,
} from 'd3-force';
import { EdgeKeyShape } from './edgeKey';
import type {
  GraphGroup,
  GraphLayout,
  GraphLink,
  GraphNode,
  NetworkTopology,
  NetworkView,
} from './types';

const COLS = 3;
const SLOT_W = 320;
const SLOT_H = 250;
const MARGIN = 64;
const FRAME_PAD_X = 28;
const FRAME_TOP = 52;
const FRAME_BOTTOM = 28;
const EMPTY_W = 240;
const EMPTY_H = 150;
const TITLE_CHAR_WIDTH = 7.5;
const META_RESERVED_WIDTH = 90;
const TITLE_PADDING = 32;

function minFrameWidthForName(name: string): number {
  return Math.max(EMPTY_W, name.length * TITLE_CHAR_WIDTH + META_RESERVED_WIDTH + TITLE_PADDING);
}
const TICKS = 260;

interface SimNode extends GraphNode, SimulationNodeDatum {}
interface SimLink extends SimulationLinkDatum<SimNode> {
  id: string;
  network: string;
  sourceName: string;
  targetName: string;
}

function slotCenter(index: number): { x: number; y: number } {
  const col = index % COLS;
  const row = Math.floor(index / COLS);
  return { x: MARGIN + SLOT_W * col + SLOT_W / 2, y: MARGIN + SLOT_H * row + SLOT_H / 2 };
}

function nodeHalfW(name: string): number {
  return Math.min(112, Math.max(54, name.length * 8 + 30));
}

const nodeHalfH = 27;

/**
 * Build the render model: unique container nodes, one pairwise edge per
 * member pair per network, and one frame per network (empty included).
 */
export function buildLayout(topo: NetworkTopology): GraphLayout {
  const networks = [...topo.networks].sort((a, b) => a.name.localeCompare(b.name));

  const nodeMap = new Map<string, GraphNode>();
  for (const net of networks) {
    for (const member of net.containers) {
      let node = nodeMap.get(member.id);
      if (!node) {
        node = {
          id: member.id,
          name: member.name,
          state: member.state,
          status: member.status,
          running: member.running,
          networks: [],
          members: [],
        };
        nodeMap.set(member.id, node);
      }
      if (!node.networks.includes(net.name)) node.networks.push(net.name);
      node.members.push({ ...member, network: net.name });
    }
  }
  const nodes: SimNode[] = [...nodeMap.values()].map((n) => ({ ...n }));
  const byId = new Map(nodes.map((n) => [n.id, n]));

  // Deterministic initial positions: average of the node's network slots
  // plus a small per-network offset so members of one net do not stack.
  for (const node of nodes) {
    let sx = 0;
    let sy = 0;
    for (const netName of node.networks) {
      const idx = networks.findIndex((n) => n.name === netName);
      const slot = slotCenter(idx);
      const peers = Math.max(1, networks[idx].containers.length - 1);
      const peerIdx = networks[idx].containers.findIndex((c) => c.id === node.id);
      const angle = peers === 0 ? 0 : (peerIdx / peers) * Math.PI * 2;
      sx += slot.x + Math.cos(angle) * 42;
      sy += slot.y + Math.sin(angle) * 42;
    }
    node.x = sx / Math.max(1, node.networks.length);
    node.y = sy / Math.max(1, node.networks.length);
  }

  // Pairwise member edges for every network. The same pair on two networks
  // yields two edges, each tagged with its own network.
  const links: SimLink[] = [];
  for (const net of networks) {
    const members = [...net.containers].sort((a, b) => a.name.localeCompare(b.name));
    for (let i = 0; i < members.length; i++) {
      for (let j = i + 1; j < members.length; j++) {
        const a = members[i];
        const b = members[j];
        links.push({
          id: EdgeKeyShape(net.name, a.name, b.name),
          source: a.id,
          target: b.id,
          network: net.name,
          sourceName: a.name,
          targetName: b.name,
        });
      }
    }
  }

  const sim = forceSimulation<SimNode>(nodes).stop();
  sim
    .force(
      'link',
      forceLink<SimNode, SimLink>(links)
        .id((d) => d.id)
        .distance(104)
        .strength(0.42),
    )
    .force('charge', forceManyBody<SimNode>().strength(-320))
    .force(
      'collide',
      forceCollide<SimNode>((d) => nodeHalfW(d.name) * 0.72).strength(1).iterations(2),
    );
  networks.forEach((net: NetworkView, idx: number) => {
    const slot = slotCenter(idx);
    const inNet = (d: SimNode) => d.networks.includes(net.name);
    sim.force(
      `cluster-x-${net.name}`,
      forceX<SimNode>((d) => (inNet(d) ? slot.x : 0)).strength((d) => (inNet(d) ? 0.16 : 0)),
    );
    sim.force(
      `cluster-y-${net.name}`,
      forceY<SimNode>((d) => (inNet(d) ? slot.y : 0)).strength((d) => (inNet(d) ? 0.16 : 0)),
    );
  });
  for (let i = 0; i < TICKS; i++) sim.tick();
  sim.stop();

  // Frames: bbox of member nodes, fixed-size frame for empty networks.
  const groups: GraphGroup[] = networks.map((net, idx) => {
    const slot = slotCenter(idx);
    const present = net.containers.length > 0;
    if (!present) {
      const w = minFrameWidthForName(net.name);
      return {
        name: net.name,
        driver: net.driver,
        scope: net.scope,
        internal: net.internal,
        count: 0,
        empty: true,
        x: slot.x - w / 2,
        y: slot.y - EMPTY_H / 2,
        w,
        h: EMPTY_H,
      };
    }
    let minX = Infinity;
    let minY = Infinity;
    let maxX = -Infinity;
    let maxY = -Infinity;
    for (const member of net.containers) {
      const n = byId.get(member.id);
      if (!n) continue;
      const hw = nodeHalfW(n.name);
      minX = Math.min(minX, (n.x ?? 0) - hw);
      maxX = Math.max(maxX, (n.x ?? 0) + hw);
      minY = Math.min(minY, (n.y ?? 0) - nodeHalfH);
      maxY = Math.max(maxY, (n.y ?? 0) + nodeHalfH);
    }
    const nodeW = Math.max(EMPTY_W, maxX - minX + FRAME_PAD_X * 2);
    const w = Math.max(nodeW, minFrameWidthForName(net.name));
    const h = Math.max(EMPTY_H, maxY - minY + FRAME_TOP + FRAME_BOTTOM);
    return {
      name: net.name,
      driver: net.driver,
      scope: net.scope,
      internal: net.internal,
      count: net.containers.length,
      empty: false,
      x: minX - FRAME_PAD_X,
      y: minY - FRAME_TOP,
      w,
      h,
    };
  });

  // Bounds over frames (frames already enclose nodes).
  let minX = Infinity;
  let minY = Infinity;
  let maxX = -Infinity;
  let maxY = -Infinity;
  for (const g of groups) {
    minX = Math.min(minX, g.x);
    minY = Math.min(minY, g.y);
    maxX = Math.max(maxX, g.x + g.w);
    maxY = Math.max(maxY, g.y + g.h);
  }
  const shiftX = -minX + MARGIN / 2;
  const shiftY = -minY + MARGIN / 2;
  for (const n of nodes) {
    n.x = (n.x ?? 0) + shiftX;
    n.y = (n.y ?? 0) + shiftY;
  }
  for (const g of groups) {
    g.x += shiftX;
    g.y += shiftY;
  }

  return {
    nodes: nodes as GraphNode[],
    links: links as unknown as GraphLink[],
    groups,
    width: Math.ceil(maxX - minX + MARGIN),
    height: Math.ceil(maxY - minY + MARGIN),
  };
}

/** Edge pairs per network for callers/tests that do not need coordinates. */
export function edgeKey(network: string, a: string, b: string): string {
  return EdgeKeyShape(network, a, b);
}
