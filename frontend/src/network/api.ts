import { basePath } from '../utils';
import type { ApiErrorBody, NetworkTopology } from './types';

export class TopologyFetchError extends Error {
  status: number;
  code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'TopologyFetchError';
    this.status = status;
    this.code = code;
  }
}

/**
 * Guest-readable topology fetch. No token, matching /data and
 * GET /api/prune/candidates. The only write the page performs is a local
 * SVG download in the browser.
 */
export async function fetchTopology(signal?: AbortSignal): Promise<NetworkTopology> {
  const res = await fetch(`${basePath}api/networks/topology`, {
    method: 'GET',
    headers: { Accept: 'application/json' },
    signal,
  });
  if (!res.ok) {
    let code = `http_${res.status}`;
    let description = res.statusText;
    try {
      const body = (await res.json()) as ApiErrorBody;
      if (body.error) code = body.error;
      if (body.error_description) description = body.error_description;
    } catch {
      // Non-JSON error body: keep status text.
    }
    throw new TopologyFetchError(res.status, code, description);
  }
  return (await res.json()) as NetworkTopology;
}
