// Edge key shape, kept in one place so the graph, export and tests agree
// with the Go helper netview.EdgeKey: network|nameA|nameB, names sorted.
export function EdgeKeyShape(network: string, a: string, b: string): string {
  const [x, y] = a <= b ? [a, b] : [b, a];
  return `${network}|${x}|${y}`;
}
