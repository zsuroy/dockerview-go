import { basePath } from '../../utils';
import type { WeComInjectResult, WeComSnapshot } from './wecomTypes';

/** Error carrying the HTTP status so callers can branch on 401/403. */
export class WeComApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = 'WeComApiError';
    this.status = status;
  }
}

async function parseOrText(res: Response): Promise<string> {
  const text = await res.text();
  try {
    const parsed = JSON.parse(text) as { error?: string };
    if (parsed && typeof parsed.error === 'string') return parsed.error;
  } catch {
    // not JSON; fall through to the raw body
  }
  return text;
}

/** GET /api/wecom/state — read-only, works without a token. */
export async function fetchWeComState(token: string): Promise<WeComSnapshot> {
  const q = token ? `?token=${encodeURIComponent(token)}` : '';
  const res = await fetch(`${basePath}api/wecom/state${q}`);
  if (!res.ok) {
    throw new WeComApiError(await parseOrText(res), res.status);
  }
  return res.json();
}

/** POST /api/wecom/inject — admin only. */
export async function injectWeComText(
  text: string,
  token: string,
  userId?: string
): Promise<WeComInjectResult> {
  const res = await fetch(`${basePath}api/wecom/inject`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Auth-Token': token,
    },
    body: JSON.stringify({ text, user_id: userId }),
  });
  if (!res.ok) {
    throw new WeComApiError(await parseOrText(res), res.status);
  }
  return res.json();
}

/** POST /api/wecom/welcome — admin only; drills the enter_chat path. */
export async function drillWeComWelcome(token: string, userId?: string): Promise<void> {
  const res = await fetch(`${basePath}api/wecom/welcome`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Auth-Token': token,
    },
    body: JSON.stringify({ user_id: userId }),
  });
  if (!res.ok) {
    throw new WeComApiError(await parseOrText(res), res.status);
  }
}
