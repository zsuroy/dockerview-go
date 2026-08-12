import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  parseOrText, fetchBackupPreview, createBackup, fetchBackupList,
  deleteBackup, backupDownloadUrl,
} from './api';

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function textResponse(status: number, body: string): Response {
  return new Response(body, {
    status,
    headers: { 'Content-Type': 'text/plain' },
  });
}

beforeEach(() => {
  // api.ts reads window.location at module load (basePath); jsdom gives http://localhost/
  vi.stubGlobal('fetch', vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('parseOrText', () => {
  it('prefers error_description from the shared error shape', async () => {
    const res = jsonResponse(400, { error: 'invalid_name', error_description: 'bad name' });
    expect(await parseOrText(res)).toBe('bad name');
  });

  it('falls back to error code when no description', async () => {
    const res = jsonResponse(400, { error: 'invalid_name' });
    expect(await parseOrText(res)).toBe('invalid_name');
  });

  it('returns raw text for non-JSON bodies without throwing (single-read fix)', async () => {
    // The server answers 401 with plain text; the old double-read implementation
    // threw "body stream already read" here.
    const res = textResponse(401, 'Unauthorized: Invalid or missing security token');
    await expect(parseOrText(res)).resolves.toBe('Unauthorized: Invalid or missing security token');
  });
});

describe('fetchBackupPreview', () => {
  it('POSTs include_images and the auth header', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { containers: 1, images: null, estimated_bytes: 10, options: { include_images: false, include_stopped: false }, warnings: [], would_include: [] }));
    await fetchBackupPreview('tok', false, false);
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain('api/backup/preview');
    expect(init?.method).toBe('POST');
    expect((init?.headers as Record<string, string>)['X-Auth-Token']).toBe('tok');
    expect(JSON.parse(String(init?.body))).toEqual({ include_images: false, include_stopped: false });
  });

  it('throws an Error carrying the HTTP status on failure', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(textResponse(401, 'Unauthorized'));
    const err = await fetchBackupPreview('bad', false, false).catch(e => e);
    expect(err).toBeInstanceOf(Error);
    expect((err as Error & { status?: number }).status).toBe(401);
    expect((err as Error).message).toContain('Unauthorized');
  });
});

describe('createBackup', () => {
  it('sends include_images and note', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { name: 'n.zip', path: 'p', size_bytes: 1, containers: 0, images: 0, options: { include_images: true, include_stopped: false }, duration_ms: 1, warnings: [] }));
    await createBackup('tok', { include_images: true, include_stopped: false, note: 'handover' });
    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(String(init?.body))).toEqual({ include_images: true, include_stopped: false, note: 'handover' });
  });

  it('surfaces 409 busy with status for the UI to classify', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(jsonResponse(409, { error: 'create_in_progress', error_description: 'a backup create is already in progress' }));
    const err = await createBackup('tok', { include_images: false, include_stopped: false, note: '' }).catch(e => e);
    expect((err as Error & { status?: number }).status).toBe(409);
    expect((err as Error).message).toContain('already in progress');
  });
});

describe('fetchBackupList', () => {
  it('GETs the list endpoint with the token header', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { backups: [], dir: 'd', max_archives: 10 }));
    const rep = await fetchBackupList('tok');
    expect(rep.max_archives).toBe(10);
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain('api/backup/list');
    expect(init?.method ?? 'GET').toBe('GET');
  });

  it('propagates 503 with status', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(jsonResponse(503, { error: 'backup_unavailable', error_description: 'backup backend not configured' }));
    const err = await fetchBackupList('tok').catch(e => e);
    expect((err as Error & { status?: number }).status).toBe(503);
  });
});

describe('deleteBackup', () => {
  it('POSTs the archive name and returns the deletion echo', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { deleted: 'x.zip' }));
    const rep = await deleteBackup('tok', 'x.zip');
    expect(rep.deleted).toBe('x.zip');
    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(String(init?.body))).toEqual({ name: 'x.zip' });
  });

  it('propagates 404 for a missing archive', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(jsonResponse(404, { error: 'not_found', error_description: 'archive does not exist' }));
    const err = await deleteBackup('tok', 'gone.zip').catch(e => e);
    expect((err as Error & { status?: number }).status).toBe(404);
  });
});

describe('backupDownloadUrl', () => {
  it('builds an authenticated download URL with encoded name', async () => {
    const url = backupDownloadUrl('tok', 'dockerview-backup-20260812T083015Z-a1b2c3.zip');
    expect(url).toContain('api/backup/download?');
    expect(url).toContain('name=dockerview-backup-20260812T083015Z-a1b2c3.zip');
    expect(url).toContain('token=tok');
  });

  it('omits the token param when empty', () => {
    const url = backupDownloadUrl('', 'a.zip');
    expect(url).not.toContain('token=');
  });
});
