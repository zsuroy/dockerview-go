import { useCallback, useEffect, useRef, useState } from 'react';
import { basePath } from '../utils';

// Convert a datetime-local value ("YYYY-MM-DDTHH:mm") to RFC3339 UTC for
// transport. Returns "" when empty or unparseable (query param omitted).
function toRFC3339(v?: string): string {
  if (!v) return '';
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return '';
  return d.toISOString();
}

export interface AuditItem {
  id: number;
  time: string;
  actor: string;
  actor_kind: string;
  source: string;
  action: string;
  container_id: string;
  container_name: string;
  result: 'success' | 'failure' | 'denied';
  status_code: number;
  duration_ms: number;
  detail: string;
  request_id: string;
  client_ip: string;
  user_agent: string;
  payload?: Record<string, unknown>;
}

export interface AuditPage {
  total: number;
  count: number;
  offset: number;
  limit: number;
  filters: Record<string, unknown>;
  items: AuditItem[];
}

export interface AuditStats {
  total: number;
  last_24h: number;
  failures_24h: number;
  denied_24h: number;
  retention_days: number;
  drop_count: number;
  db_path: string;
}

export interface AuditFilters {
  since?: string;
  until?: string;
  container_id?: string;
  container_name?: string;
  action?: string[];
  result?: string[];
  limit?: number;
  offset?: number;
  sort?: 'time_desc' | 'time_asc';
}

interface UseAuditArgs {
  token: string;
  enabled: boolean;
}

export class AuditAuthError extends Error {}

export function useAudit({ token, enabled }: UseAuditArgs) {
  const [page, setPage] = useState<AuditPage | null>(null);
  const [stats, setStats] = useState<AuditStats | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filters, setFilters] = useState<AuditFilters>({ limit: 50, offset: 0, sort: 'time_desc' });
  const abortRef = useRef<AbortController | null>(null);

  const tokenRef = useRef(token);
  tokenRef.current = token;

  const buildUrl = useCallback((endpoint: string, f?: AuditFilters, extra?: Record<string, string>) => {
    const params = new URLSearchParams();
    if (tokenRef.current) params.set('token', tokenRef.current);
    if (f) {
      const since = toRFC3339(f.since);
      const until = toRFC3339(f.until);
      if (since) params.set('since', since);
      if (until) params.set('until', until);
      if (f.container_id) params.set('container_id', f.container_id);
      if (f.container_name) params.set('container_name', f.container_name);
      if (f.action && f.action.length) params.set('action', f.action.join(','));
      if (f.result && f.result.length) params.set('result', f.result.join(','));
      if (f.limit) params.set('limit', String(f.limit));
      if (f.offset) params.set('offset', String(f.offset));
      if (f.sort) params.set('sort', f.sort);
    }
    if (extra) {
      for (const [k, v] of Object.entries(extra)) params.set(k, v);
    }
    return `${basePath}api/audit${endpoint}?${params.toString()}`;
  }, []);

  const fetchPage = useCallback(async (f: AuditFilters, signal?: AbortSignal) => {
    const resp = await fetch(buildUrl('', f), { signal });
    if (resp.status === 401) throw new AuditAuthError('unauthorized');
    if (!resp.ok) {
      const t = await resp.text();
      throw new Error(t || `HTTP ${resp.status}`);
    }
    return (await resp.json()) as AuditPage;
  }, [buildUrl]);

  const fetchStats = useCallback(async (signal?: AbortSignal) => {
    const resp = await fetch(buildUrl('/stats'), { signal });
    if (resp.status === 401) throw new AuditAuthError('unauthorized');
    if (!resp.ok) {
      const t = await resp.text();
      throw new Error(t || `HTTP ${resp.status}`);
    }
    return (await resp.json()) as AuditStats;
  }, [buildUrl]);

  const refresh = useCallback(async () => {
    if (!enabled) return;
    if (abortRef.current) abortRef.current.abort();
    const ac = new AbortController();
    abortRef.current = ac;
    setLoading(true);
    setError(null);
    try {
      const [p, s] = await Promise.all([fetchPage(filters, ac.signal), fetchStats(ac.signal)]);
      setPage(p);
      setStats(s);
    } catch (e) {
      if ((e as Error).name === 'AbortError') return;
      if (e instanceof AuditAuthError) throw e;
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }, [enabled, fetchPage, fetchStats, filters]);

  const updateFilters = useCallback((next: Partial<AuditFilters>) => {
    setFilters((prev) => ({ ...prev, ...next, offset: next.offset ?? (Object.prototype.hasOwnProperty.call(next, 'limit') ? 0 : prev.offset) }));
  }, []);

  const exportUrl = useCallback((format: 'json' | 'md') => buildUrl('/export', filters, { format }), [buildUrl, filters]);

  useEffect(() => {
    if (!enabled) return;
    refresh();
    const id = setInterval(refresh, 10_000);
    return () => clearInterval(id);
  }, [enabled, refresh]);

  useEffect(() => {
    if (!enabled) return;
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filters.limit, filters.offset, filters.sort, JSON.stringify(filters.action), JSON.stringify(filters.result), filters.since, filters.until, filters.container_id, filters.container_name, enabled]);

  return { page, stats, loading, error, filters, updateFilters, refresh, exportUrl };
}
