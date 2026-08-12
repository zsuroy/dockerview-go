import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';
import { I18nProvider } from '../i18n';
import { BackupPanel } from './BackupPanel';

function renderPanel(props: Partial<{ token: string; onAuthRequired: () => void; onToast: (m: string, t?: 'info' | 'success' | 'error') => void }> = {}) {
  const onAuthRequired = props.onAuthRequired ?? vi.fn();
  const onToast = props.onToast ?? vi.fn();
  const token = props.token ?? 'tok';
  const utils = render(
    <I18nProvider>
      <BackupPanel token={token} onAuthRequired={onAuthRequired} onToast={onToast} />
    </I18nProvider>,
  );
  return { ...utils, onAuthRequired, onToast };
}

function mockListResponse(backups: unknown[] = []) {
  return new Response(JSON.stringify({ backups, dir: 'data/backups', max_archives: 10 }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn());
  // BackupPanel loads the list on mount; give it an empty list by default.
  vi.mocked(fetch).mockResolvedValue(mockListResponse());
  // jsdom has no real confirm; default to accepting.
  vi.spyOn(window, 'confirm').mockReturnValue(true);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('BackupPanel — create form defaults', () => {
  it('renders the panel root', async () => {
    renderPanel();
    expect(await screen.findByTestId('backup-panel')).toBeInTheDocument();
  });

  it('include_images checkbox is OFF by default (design red line)', () => {
    renderPanel();
    const checkbox = screen.getByTestId('backup-include-images') as HTMLInputElement;
    expect(checkbox.checked).toBe(false);
  });

  it('shows the risk notice only after enabling include_images', async () => {
    renderPanel();
    expect(screen.queryByTestId('backup-risk-notice')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('backup-include-images'));
    expect(await screen.findByTestId('backup-risk-notice')).toBeInTheDocument();
  });

  it('note input accepts operator text', () => {
    renderPanel();
    const note = screen.getByTestId('backup-note') as HTMLTextAreaElement;
    fireEvent.change(note, { target: { value: 'pre-upgrade snapshot' } });
    expect(note.value).toBe('pre-upgrade snapshot');
  });
});

describe('BackupPanel — create flow', () => {
  it('create triggers the API with include_images=false by default', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(mockListResponse()); // mount list load
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({
      name: 'dockerview-backup-20260812T083015Z-a1b2c3.zip', path: 'p', size_bytes: 10,
      containers: 1, images: 0, options: { include_images: false, include_stopped: false }, duration_ms: 5, warnings: [],
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }));
    fetchMock.mockResolvedValue(mockListResponse()); // refresh after create

    const { onToast } = renderPanel();
    await screen.findByTestId('backup-list-empty');
    fireEvent.click(screen.getByTestId('backup-create-btn'));

    await waitFor(() => {
      const calls = fetchMock.mock.calls.filter(c => String(c[0]).includes('api/backup/create'));
      expect(calls).toHaveLength(1);
      expect(JSON.parse(String(calls[0][1]?.body))).toEqual({ include_images: false, include_stopped: false, note: '' });
    });
    await waitFor(() => expect(onToast).toHaveBeenCalled());
  });

  it('create button is disabled while a create is in flight (double-submit guard)', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(mockListResponse()); // mount list load
    // create request that never resolves -> stays busy
    let releaseCreate: (v: Response) => void = () => {};
    fetchMock.mockImplementationOnce(() => new Promise<Response>(res => { releaseCreate = res; }));

    renderPanel();
    await screen.findByTestId('backup-list-empty');
    const btn = screen.getByTestId('backup-create-btn') as HTMLButtonElement;
    expect(btn.disabled).toBe(false);
    fireEvent.click(btn);
    await waitFor(() => expect((screen.getByTestId('backup-create-btn') as HTMLButtonElement).disabled).toBe(true));
    // release to avoid act warnings leaking
    releaseCreate(new Response(JSON.stringify({ name: 'n', path: 'p', size_bytes: 1, containers: 0, images: 0, options: { include_images: false, include_stopped: false }, duration_ms: 1, warnings: [] }), { status: 200 }));
  });

  it('shows create error banner on API failure', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(mockListResponse()); // mount
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ error: 'backup_error', error_description: 'disk full' }), { status: 500, headers: { 'Content-Type': 'application/json' } }));

    renderPanel();
    await screen.findByTestId('backup-list-empty');
    fireEvent.click(screen.getByTestId('backup-create-btn'));
    expect(await screen.findByTestId('backup-create-error')).toBeInTheDocument();
  });
});

describe('BackupPanel — history states', () => {
  it('shows empty state when there are no archives', async () => {
    renderPanel();
    expect(await screen.findByTestId('backup-list-empty')).toBeInTheDocument();
  });

  it('shows error state and retry when the list fails', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockRejectedValueOnce(new Error('connection refused'));
    renderPanel();
    expect(await screen.findByTestId('backup-list-error')).toBeInTheDocument();
  });

  it('renders archive rows with badges and actions', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(mockListResponse([
      { name: 'a.zip', created_at: '2026-08-12T08:00:00Z', size_bytes: 1024, note: 'x', include_images: true, containers: 2 },
    ]));
    renderPanel();
    expect(await screen.findByTestId('backup-table')).toBeInTheDocument();
    expect(screen.getByTestId('backup-download-a.zip')).toBeInTheDocument();
    expect(screen.getByTestId('backup-delete-a.zip')).toBeInTheDocument();
  });

  it('delete asks for confirmation and calls the delete endpoint', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(mockListResponse([
      { name: 'a.zip', created_at: '2026-08-12T08:00:00Z', size_bytes: 1024, include_images: false, containers: 1 },
    ]));
    renderPanel();
    await screen.findByTestId('backup-table');

    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ deleted: 'a.zip' }), { status: 200 }));
    fetchMock.mockResolvedValue(mockListResponse());

    fireEvent.click(screen.getByTestId('backup-delete-a.zip'));
    await waitFor(() => {
      expect(window.confirm).toHaveBeenCalled();
      const calls = fetchMock.mock.calls.filter(c => String(c[0]).includes('api/backup/delete'));
      expect(calls).toHaveLength(1);
    });
  });
});

describe('BackupPanel — auth', () => {
  it('shows the auth prompt instead of actions when token is empty', () => {
    renderPanel({ token: '' });
    expect(screen.getByTestId('backup-auth-prompt')).toBeInTheDocument();
    expect(screen.queryByTestId('backup-create-btn')).not.toBeInTheDocument();
  });
});
