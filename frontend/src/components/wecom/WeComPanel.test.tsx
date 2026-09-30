import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { I18nProvider } from '../../i18n';
import { WeComPanel } from './WeComPanel';
import type { WeComSnapshot } from './wecomTypes';

function snapshot(over: Partial<WeComSnapshot> = {}): WeComSnapshot {
  return {
    enabled: true,
    mode: 'mock',
    state: 'mock',
    state_reason: '未配置凭证，使用内存 transport，不拨公网',
    ws_url: 'wss://openws.work.weixin.qq.com',
    bot_id_masked: '',
    has_secret: false,
    reply_mode: 'stream',
    inject_allowed: true,
    counts: { inbound: 0, outbound: 0, event: 0 },
    recent: [],
    group_webhook_enabled: false,
    ...over,
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function renderPanel(props: Partial<{ token: string }> = {}) {
  const onAuthRequired = vi.fn();
  const utils = render(
    <I18nProvider>
      <WeComPanel serverToken={props.token ?? 'admin-token'} onAuthRequired={onAuthRequired} />
    </I18nProvider>
  );
  return { ...utils, onAuthRequired };
}

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('WeComPanel — connection state', () => {
  it('shows the mock notice when no credentials are configured', async () => {
    fetchMock.mockResolvedValue(jsonResponse(snapshot()));

    renderPanel();

    expect(await screen.findByTestId('wecom-panel')).toBeTruthy();
    const state = await screen.findByTestId('wecom-state');
    expect(state.getAttribute('data-state')).toBe('mock');
    expect(await screen.findByTestId('wecom-mock-notice')).toBeTruthy();
  });

  it('shows connected without the mock notice', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(
        snapshot({
          mode: 'live',
          state: 'connected',
          state_reason: '',
          bot_id_masked: 'ww12****',
          has_secret: true,
        })
      )
    );

    renderPanel();

    const state = await screen.findByTestId('wecom-state');
    expect(state.getAttribute('data-state')).toBe('connected');
    expect(screen.queryByTestId('wecom-mock-notice')).toBeNull();
  });

  it('shows disconnected', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(snapshot({ mode: 'live', state: 'disconnected', has_secret: true }))
    );

    renderPanel();

    const state = await screen.findByTestId('wecom-state');
    expect(state.getAttribute('data-state')).toBe('disconnected');
  });

  it('shows the auth-failure reason in the error state', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(
        snapshot({
          mode: 'live',
          state: 'error',
          state_reason: 'aibot_subscribe rejected: bad secret',
          has_secret: true,
        })
      )
    );

    renderPanel();

    const state = await screen.findByTestId('wecom-state');
    expect(state.getAttribute('data-state')).toBe('error');
    expect(await screen.findByText(/aibot_subscribe rejected/)).toBeTruthy();
  });

  it('shows a retryable error when the state request fails', async () => {
    fetchMock.mockResolvedValue(new Response('boom', { status: 500 }));

    renderPanel();

    const err = await screen.findByTestId('wecom-error');
    expect(err.getAttribute('role')).toBe('alert');
    expect(err.textContent).toContain('boom');
  });
});

describe('WeComPanel — inject', () => {
  it('sends the typed question to /api/wecom/inject', async () => {
    fetchMock.mockResolvedValue(jsonResponse(snapshot()));
    renderPanel();
    await screen.findByTestId('wecom-panel');

    const input = (await screen.findByTestId('wecom-inject-input')) as HTMLInputElement;
    fireEvent.change(input, { target: { value: '哪些容器不健康？' } });

    fetchMock.mockResolvedValue(jsonResponse({ status: 'accepted' }, 202));
    fireEvent.click(screen.getByTestId('wecom-inject-send'));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find((c) => String(c[0]).includes('/api/wecom/inject'));
      expect(call).toBeTruthy();
      expect(JSON.parse(String(call![1]?.body))).toEqual({
        text: '哪些容器不健康？',
        user_id: undefined,
      });
    });
  });

  it('renders the reply that appears after an inject', async () => {
    fetchMock.mockResolvedValue(jsonResponse(snapshot()));
    renderPanel();
    await screen.findByTestId('wecom-panel');

    fetchMock.mockResolvedValue(
      jsonResponse(
        snapshot({
          recent: [
            { time: '2026-09-28T12:03:44Z', kind: 'inbound', source: 'inject', user: 'admin', text: '哪些容器不健康？' },
            {
              time: '2026-09-28T12:03:44Z',
              kind: 'outbound',
              source: 'system',
              channel: 'stream',
              text: 'api 健康分 42，最近 100 行里 7 条 ERROR。',
            },
          ],
          counts: { inbound: 1, outbound: 1, event: 0 },
        })
      )
    );

    fireEvent.click(screen.getByTestId('wecom-refresh'));

    expect(await screen.findByText('哪些容器不健康？')).toBeTruthy();
    expect(await screen.findByText(/api 健康分 42/)).toBeTruthy();
  });

  it('renders empty states before anything has happened', async () => {
    fetchMock.mockResolvedValue(jsonResponse(snapshot()));
    renderPanel();

    expect(await screen.findByTestId('wecom-recent-inbound')).toBeTruthy();
    expect(await screen.findByTestId('wecom-recent-outbound')).toBeTruthy();
  });
});

describe('WeComPanel — guest', () => {
  it('disables inject and explains why when inject_allowed is false', async () => {
    fetchMock.mockResolvedValue(jsonResponse(snapshot({ inject_allowed: false })));

    renderPanel({ token: '' });

    expect(await screen.findByTestId('wecom-guest-notice')).toBeTruthy();
    expect((screen.getByTestId('wecom-inject-input') as HTMLInputElement).disabled).toBe(true);
    expect((screen.getByTestId('wecom-inject-send') as HTMLButtonElement).disabled).toBe(true);
  });

  it('shows the server refusal when an inject is rejected', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(snapshot()));
    renderPanel();

    const input = (await screen.findByTestId('wecom-inject-input')) as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'hi' } });

    fetchMock.mockResolvedValueOnce(new Response('Forbidden: no admin token', { status: 403 }));
    fireEvent.click(screen.getByTestId('wecom-inject-send'));

    const notice = await screen.findByTestId('wecom-inject-notice');
    expect(notice.getAttribute('role')).toBe('alert');
    expect(notice.textContent).toMatch(/token/i);
  });
});

describe('WeComPanel — pending write', () => {
  it('shows the confirmation card and the "confirm in the web console" gate', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(
        snapshot({
          recent: [
            {
              time: '2026-09-28T12:11:02Z',
              kind: 'outbound',
              source: 'system',
              channel: 'card',
              text: '需要人工确认：restart api（8f3c1a92d4e7）',
            },
          ],
        })
      )
    );

    renderPanel();

    const card = await screen.findByTestId('wecom-proposal');
    // The op and the container id come from the transcript and are
    // language-neutral; the gate sentence is translated, so match either
    // language rather than pinning the default locale.
    expect(card.textContent).toContain('restart api');
    expect(card.textContent).toContain('8f3c1a92d4e7');
    expect(card.textContent).toMatch(/admin|管理员/);
    expect(card.textContent).toMatch(/never starts or stops|不会启停/);
  });

  it('does not show a confirmation card for read-only traffic', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(
        snapshot({
          recent: [
            {
              time: '2026-09-28T12:03:44Z',
              kind: 'outbound',
              source: 'system',
              channel: 'stream',
              text: '一切正常。',
            },
          ],
        })
      )
    );

    renderPanel();
    await screen.findByTestId('wecom-recent-outbound');

    expect(screen.queryByTestId('wecom-proposal')).toBeNull();
  });
});
