import { basePath } from '../utils';
import type { Candidates, DeleteReport, DryRunReport, AuditEvent } from './types';

function authHeaders(token: string): Record<string, string> {
  const h: Record<string, string> = { 'Content-Type': 'application/json' };
  if (token) h['X-Auth-Token'] = token;
  return h;
}

async function parseOrText(res: Response): Promise<string> {
  try {
    const j = await res.json();
    return j?.error_description || j?.error || JSON.stringify(j);
  } catch {
    return await res.text();
  }
}

export async function fetchCandidates(token?: string): Promise<Candidates> {
  const headers = authHeaders(token || '');
  headers['Accept'] = 'application/json';
  const res = await fetch(`${basePath}api/prune/candidates`, { headers });
  if (!res.ok) throw new Error(await parseOrText(res));
  return res.json();
}

export async function runDryRun(selection: { images: string[]; volumes: string[] }, token?: string): Promise<DryRunReport> {
  const res = await fetch(`${basePath}api/prune/dry-run`, {
    method: 'POST',
    headers: authHeaders(token || ''),
    body: JSON.stringify(selection),
  });
  if (!res.ok) throw new Error(await parseOrText(res));
  return res.json();
}

export async function confirmPrune(
  token: string,
  body: { confirm: string; fingerprint: string; images: string[]; volumes: string[] }
): Promise<DeleteReport> {
  const res = await fetch(`${basePath}api/prune/confirm`, {
    method: 'POST',
    headers: authHeaders(token),
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const err = new Error(await parseOrText(res)) as Error & { status?: number };
    err.status = res.status;
    throw err;
  }
  return res.json();
}

export async function fetchAudit(token: string): Promise<{ events: AuditEvent[] }> {
  const res = await fetch(`${basePath}api/prune/audit`, { headers: authHeaders(token) });
  if (!res.ok) throw new Error(await parseOrText(res));
  return res.json();
}
