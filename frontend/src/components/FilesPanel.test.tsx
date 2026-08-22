import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';
import { I18nProvider } from '../i18n';
import { FilesPanel } from './FilesPanel';
import type { Container } from '../types';

const containers: Container[] = [
  { fullid: 'abc123full', id: 'abc123', name: 'web', status: 'Up', cpu: '1%', memory: '10MiB', blkio: '0B', network: '0B' },
];

function renderPanel(props: Partial<{ token: string; onAuthRequired: () => void; onToast: (m: string, t?: 'info' | 'success' | 'error') => void }> = {}) {
  const onAuthRequired = props.onAuthRequired ?? vi.fn();
  const onToast = props.onToast ?? vi.fn();
  const utils = render(
    <I18nProvider>
      <FilesPanel
        token={props.token ?? 'tok'}
        containers={containers}
        onAuthRequired={onAuthRequired}
        onToast={onToast}
      />
    </I18nProvider>,
  );
  return { ...utils, onAuthRequired, onToast };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

function listResponse() {
  return jsonResponse({
    path: '/tmp/dockerview-files',
    entries: [
      { name: 'a.txt', size: 3, is_dir: false, is_symlink: false },
      { name: 'sub', size: 0, is_dir: true, is_symlink: false },
    ],
  });
}
function configResponse() {
  return jsonResponse({
    jail_root: '/tmp/dockerview-files', max_file_bytes: 8388608,
    max_archive_bytes: 8388608, guest_download: false, backend_configured: true,
  });
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn());
  vi.mocked(fetch).mockImplementation(async (url: unknown) => {
    return String(url).includes('/api/files/config') ? configResponse() : listResponse();
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('FilesPanel', () => {
  it('renders inside the dashboard and lists jail entries', async () => {
    renderPanel();
    expect(screen.getByTestId('files-panel')).toBeTruthy();
    await waitFor(() => expect(screen.getByText('a.txt')).toBeTruthy());
    // The jail root path from the server is displayed, not a host path.
    expect(screen.getByText(/\/tmp\/dockerview-files/)).toBeTruthy();
  });

  it('prompts for auth when the token is missing', async () => {
    const { onAuthRequired } = renderPanel({ token: '' });
    await waitFor(() => expect(onAuthRequired).toHaveBeenCalled());
  });

  it('disables confirm upload without a file and target', async () => {
    renderPanel();
    await waitFor(() => expect(screen.getByText('a.txt')).toBeTruthy());
    expect((screen.getByTestId('files-upload') as HTMLButtonElement).disabled).toBe(true);
  });

  it('previews an existing target and demands overwrite confirmation', async () => {
    vi.mocked(fetch).mockImplementation(async (url: unknown) => {
      if (String(url).includes('/api/files/in/preview')) {
        return jsonResponse({
          path: '/tmp/dockerview-files/a.txt', exists: true, overwrite_required: true,
          size_existing: 3, max_file_bytes: 8388608,
        });
      }
      return jsonResponse({
        path: '/tmp/dockerview-files',
        entries: [{ name: 'a.txt', size: 3, is_dir: false, is_symlink: false }],
      });
    });
    renderPanel();
    await waitFor(() => expect(screen.getByText('a.txt')).toBeTruthy());
    fireEvent.change(screen.getByPlaceholderText(/logs\/app\.log/), { target: { value: 'a.txt' } });
    fireEvent.change(screen.getByTestId('files-input'), {
      target: { files: [new File(['x'], 'x.bin', { type: 'application/octet-stream' })] },
    });
    fireEvent.click(screen.getByText('Preview'));
    await waitFor(() => expect(screen.getByText(/overwriting/i)).toBeTruthy());
    // Upload without ticking overwrite is blocked client-side hint or server 409;
    // the checkbox must be present.
    expect(screen.getByRole('checkbox')).toBeTruthy();
  });

  it('previews a missing target dir and demands mkdir confirmation', async () => {
    vi.mocked(fetch).mockImplementation(async (url: unknown) => {
      const u = String(url);
      if (u.includes('/api/files/in/preview')) {
        return jsonResponse({
          path: '/tmp/dockerview-files/logs/app.log', exists: false, overwrite_required: false,
          size_existing: 0, max_file_bytes: 8388608,
          missing_dirs: ['/tmp/dockerview-files/logs'], mkdir_required: true,
        });
      }
      return listResponse();
    });
    // Stub XHR: the real upload path uses XMLHttpRequest (multipart + progress).
    let sentBody = '';
    const FakeXhr = class {
      upload = {};
      status = 200;
      responseText = JSON.stringify({
        path: '/tmp/dockerview-files/logs/app.log', bytes: 1,
        sha256: 'ab'.repeat(32), overwritten: false,
      });
      onload: (() => void) | null = null;
      onerror: (() => void) | null = null;
      onprogress: (() => void) | null = null;
      open() {}
      setRequestHeader() {}
      send(form: FormData) {
        sentBody = JSON.stringify([...form.entries()]);
        this.onload?.();
      }
    };
    vi.stubGlobal('XMLHttpRequest', FakeXhr);
    const { onToast } = renderPanel();
    await waitFor(() => expect(screen.getByText('a.txt')).toBeTruthy());
    fireEvent.change(screen.getByPlaceholderText(/logs\/app\.log/), { target: { value: 'logs/app.log' } });
    fireEvent.change(screen.getByTestId('files-input'), {
      target: { files: [new File(['x'], 'x.bin', { type: 'application/octet-stream' })] },
    });
    fireEvent.click(screen.getByText('Preview'));
    // The plan names the directory that will be created.
    await waitFor(() => expect(screen.getByText(/Missing directories to create/)).toBeTruthy());
    const mkdirBox = screen.getByTestId('files-mkdir') as HTMLInputElement;
    expect(mkdirBox.checked).toBe(false);
    fireEvent.click(mkdirBox);
    expect(mkdirBox.checked).toBe(true);
    fireEvent.click(screen.getByTestId('files-upload'));
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith(expect.stringMatching(/Uploaded/), 'success');
    });
    // mkdir consent must ride the multipart body.
    expect(sentBody).toContain('"mkdir","true"');
  });

  it('autofills the target path from the browse dir and file name', async () => {
    vi.mocked(fetch).mockImplementation(async (url: unknown) => {
      const u = String(url);
      if (u.includes('/api/files/list')) {
        return jsonResponse({
          path: 'docs',
          entries: [
            { name: 'notes', size: 0, is_dir: true, is_symlink: false },
            { name: 'keep.txt', size: 1, is_dir: false, is_symlink: false },
          ],
        });
      }
      return configResponse();
    });
    renderPanel();
    await waitFor(() => expect(screen.getByText('keep.txt')).toBeTruthy());
    fireEvent.click(screen.getByText('notes'));
    await waitFor(() => {
      expect(vi.mocked(fetch).mock.calls.length).toBeGreaterThanOrEqual(3);
    });
    // Picking a file fills "browse dir + file name" — no typing needed.
    fireEvent.change(screen.getByTestId('files-input'), {
      target: { files: [new File(['x'], 'shot.png', { type: 'image/png' })] },
    });
    const input = screen.getByPlaceholderText(/logs\/app\.log/) as HTMLInputElement;
    expect(input.value).toBe('notes/shot.png');
  });

  it('clears the old directory listing while navigating', async () => {
    let releaseSecond!: () => void;
    const gate = Promise.withResolvers<void>();
    releaseSecond = gate.resolve;
    vi.mocked(fetch).mockImplementation(async (url: unknown) => {
      const u = String(url);
      if (u.includes('/api/files/list')) {
        // First (root) listing returns at once; the second (sub) hangs
        // until we let it go.
        if (vi.mocked(fetch).mock.calls.length >= 3) {
          await gate.promise;
          return jsonResponse({
            path: 'sub',
            entries: [{ name: 'new.txt', size: 1, is_dir: false, is_symlink: false }],
          });
        }
        return listResponse();
      }
      return configResponse();
    });
    renderPanel();
    await waitFor(() => expect(screen.getByText('a.txt')).toBeTruthy());
    fireEvent.click(screen.getByText('sub'));
    // The stale root entry disappears immediately, before the response lands.
    await waitFor(() => expect(screen.queryByText('a.txt')).toBeNull());
    expect(screen.getByText(/Loading/)).toBeTruthy();
    releaseSecond();
    await waitFor(() => expect(screen.getByText('new.txt')).toBeTruthy());
  });
});


