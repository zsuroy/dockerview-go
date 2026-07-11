export interface PortMapping {
  ip?: string;
  private_port: number;
  public_port?: number;
  type: string;
}

export type HealthStatus = 'healthy' | 'unhealthy' | 'starting' | 'unknown';

export interface ContainerInfo {
  FullID: string;
  ID: string;
  Name: string;
  Status: string;
  CPU: string;
  Memory: string;
  Blkio: string;
  Network: string;
  HealthScore?: number;
  HealthStatus?: HealthStatus;
  ports: PortMapping[];
}

export interface ExecResult {
  exit_code: number;
  stdout: string;
  stderr: string;
}

function getHeaders(token: string): HeadersInit {
  const headers: HeadersInit = {
    'Content-Type': 'application/json',
  };
  if (token) {
    headers['X-Auth-Token'] = token;
  }
  return headers;
}

export async function fetchContainers(host: string, token: string): Promise<ContainerInfo[]> {
  const url = `${host}/data`;
  const response = await fetch(url, {
    method: 'GET',
    headers: getHeaders(token),
  });

  if (!response.ok) {
    if (response.status === 401) {
      throw new Error('Unauthorized: Invalid security token');
    }
    throw new Error(`Failed to fetch containers: ${response.statusText}`);
  }

  const data = await response.json();
  return data as ContainerInfo[];
}

export async function performContainerOp(
  host: string,
  token: string,
  id: string,
  op: 'start' | 'stop' | 'restart'
): Promise<{ status: string; op: string }> {
  // POST /api/container/op?id=...&op=...
  const url = `${host}/api/container/op?id=${encodeURIComponent(id)}&op=${encodeURIComponent(op)}`;
  const response = await fetch(url, {
    method: 'POST',
    headers: getHeaders(token),
  });

  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Failed to perform ${op}: ${response.statusText}`);
  }

  return await response.json();
}

export async function fetchContainerLogs(
  host: string,
  token: string,
  id: string,
  tail: string = '100',
  grep: string = '',
  level: string = ''
): Promise<string> {
  // GET /api/container/logs?id=...&tail=...&grep=...&level=...
  const params = new URLSearchParams({ id, tail });
  if (grep) params.append('grep', grep);
  if (level) params.append('level', level);

  const url = `${host}/api/container/logs?${params.toString()}`;
  const response = await fetch(url, {
    method: 'GET',
    headers: getHeaders(token),
  });

  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Failed to fetch logs: ${response.statusText}`);
  }

  return await response.text();
}

export async function runContainerExec(
  host: string,
  token: string,
  id: string,
  cmd: string | string[]
): Promise<ExecResult> {
  // POST /api/container/exec?id=...
  // Body: { cmd: string | string[] }
  const url = `${host}/api/container/exec?id=${encodeURIComponent(id)}`;
  const response = await fetch(url, {
    method: 'POST',
    headers: getHeaders(token),
    body: JSON.stringify({ cmd }),
  });

  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Failed to execute command: ${response.statusText}`);
  }

  return await response.json();
}
