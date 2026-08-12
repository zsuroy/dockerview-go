import { basePath } from '../utils';
import type { CreateReport, ListReport, PreviewReport } from './types';

function authHeaders(token: string): Record<string, string> {
  const h: Record<string, string> = { 'Content-Type': 'application/json' };
  if (token) h['X-Auth-Token'] = token;
  return h;
}

// parseOrText extracts a user-presentable message from an error response,
// preferring the shared {error, error_description} JSON shape.
// NOTE: the body can only be consumed once — read it as text first, then try
// to parse. Calling res.json() and falling back to res.text() throws
// "body stream already read" and loses the HTTP status handling upstream.
export async function parseOrText(res: Response): Promise<string> {
  const text = await res.text();
  try {
    const j = JSON.parse(text);
    return j?.error_description || j?.error || text;
  } catch {
    return text;
  }
}

// attachStatus decorates an Error with the HTTP status so the UI can branch
// on 401 (re-auth), 409 (busy), 503 (unavailable), etc.
function withStatus(err: Error, status: number): Error & { status: number } {
  return Object.assign(err, { status });
}

export async function fetchBackupPreview(token: string, includeImages: boolean, includeStopped: boolean): Promise<PreviewReport> {
  const res = await fetch(`${basePath}api/backup/preview`, {
    method: 'POST',
    headers: authHeaders(token),
    body: JSON.stringify({ include_images: includeImages, include_stopped: includeStopped }),
  });
  if (!res.ok) throw withStatus(new Error(await parseOrText(res)), res.status);
  return res.json();
}

export async function createBackup(
  token: string,
  opts: { include_images: boolean; include_stopped: boolean; note: string }
): Promise<CreateReport> {
  const res = await fetch(`${basePath}api/backup/create`, {
    method: 'POST',
    headers: authHeaders(token),
    body: JSON.stringify(opts),
  });
  if (!res.ok) throw withStatus(new Error(await parseOrText(res)), res.status);
  return res.json();
}

export async function fetchBackupList(token: string): Promise<ListReport> {
  const res = await fetch(`${basePath}api/backup/list`, { headers: authHeaders(token) });
  if (!res.ok) throw withStatus(new Error(await parseOrText(res)), res.status);
  return res.json();
}

export async function deleteBackup(token: string, name: string): Promise<{ deleted: string }> {
  const res = await fetch(`${basePath}api/backup/delete`, {
    method: 'POST',
    headers: authHeaders(token),
    body: JSON.stringify({ name }),
  });
  if (!res.ok) throw withStatus(new Error(await parseOrText(res)), res.status);
  return res.json();
}

// backupDownloadUrl builds the authenticated download URL for a <a href>.
export function backupDownloadUrl(token: string, name: string): string {
  const params = new URLSearchParams({ name });
  if (token) params.set('token', token);
  return `${basePath}api/backup/download?${params.toString()}`;
}
