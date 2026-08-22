import { basePath } from '../utils';
import type { InResult, InPreviewReport, ListReport, OutPreviewReport, ArchivePreviewReport } from './types';

function authHeaders(token: string): Record<string, string> {
  const h: Record<string, string> = { 'Content-Type': 'application/json' };
  if (token) h['X-Auth-Token'] = token;
  return h;
}

async function parseOrText(res: Response): Promise<string> {
  const text = await res.text();
  try {
    const j = JSON.parse(text);
    return j?.message || j?.error || text;
  } catch {
    return text;
  }
}

function withStatus(err: Error, status: number): Error & { status: number } {
  return Object.assign(err, { status });
}

async function postJSON<T>(token: string, url: string, body: unknown): Promise<T> {
  const res = await fetch(`${basePath}${url}`, {
    method: 'POST',
    headers: authHeaders(token),
    body: JSON.stringify(body),
  });
  if (!res.ok) throw withStatus(new Error(await parseOrText(res)), res.status);
  return res.json();
}

export interface FilesConfigReport {
  jail_root: string;
  max_file_bytes: number;
  max_archive_bytes: number;
  guest_download: boolean;
  backend_configured: boolean;
}

export async function fetchFilesConfig(token: string): Promise<FilesConfigReport> {
  const res = await fetch(`${basePath}api/files/config`, {
    headers: token ? { 'X-Auth-Token': token } : {},
  });
  if (!res.ok) throw withStatus(new Error(await parseOrText(res)), res.status);
  return res.json();
}

export async function fetchFilesList(token: string, id: string, path = ''): Promise<ListReport> {
  const params = new URLSearchParams({ id });
  if (path) params.set('path', path);
  const res = await fetch(`${basePath}api/files/list?${params.toString()}`, {
    headers: token ? { 'X-Auth-Token': token } : {},
  });
  if (!res.ok) throw withStatus(new Error(await parseOrText(res)), res.status);
  return res.json();
}

export function previewIn(token: string, id: string, path: string) {
  return postJSON<InPreviewReport>(token, 'api/files/in/preview', { id, path });
}

export function previewOut(token: string, id: string, path: string) {
  return postJSON<OutPreviewReport>(token, 'api/files/out/preview', { id, path });
}

export function previewArchive(token: string, id: string, path: string) {
  return postJSON<ArchivePreviewReport>(token, 'api/files/archive/preview', { id, path });
}

// uploadFile streams multipart with progress. It never touches the browser
// disk; the server stages into $DataRoot/files only.
export function uploadFile(
  token: string,
  id: string,
  path: string,
  file: File,
  overwrite: boolean,
  mkdir: boolean,
  onProgress?: (pct: number) => void,
): Promise<InResult> {
  const { promise, resolve, reject } = Promise.withResolvers<InResult>();
  {
    const form = new FormData();
    form.set('id', id);
    form.set('path', path);
    if (overwrite) form.set('overwrite', 'true');
    if (mkdir) form.set('mkdir', 'true');
    form.set('file', file);
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `${basePath}api/files/in`);
    if (token) xhr.setRequestHeader('X-Auth-Token', token);
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(Math.round((e.loaded / e.total) * 100));
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(JSON.parse(xhr.responseText));
        } catch (e) {
          reject(e as Error);
        }
      } else {
        let message = xhr.responseText || xhr.statusText;
        try {
          message = JSON.parse(xhr.responseText)?.message || message;
        } catch {
          /* keep raw */
        }
        reject(withStatus(new Error(message), xhr.status));
      }
    };
    xhr.onerror = () => reject(new Error('network error'));
    xhr.send(form);
  }
  return promise;
}

// Browser GET downloads: token rides the accepted query parameter.
export function fileDownloadUrl(token: string, kind: 'out' | 'archive', id: string, path: string): string {
  const params = new URLSearchParams({ id, path });
  if (token) params.set('token', token);
  return `${basePath}api/files/${kind}?${params.toString()}`;
}
