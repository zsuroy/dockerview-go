import { useCallback, useEffect, useRef, useState } from 'react';
import {
  AlertTriangle,
  Bot,
  Inbox,
  Lock,
  MessageSquare,
  RefreshCw,
  Send,
  ShieldAlert,
} from 'lucide-react';
import { useTranslation } from '../../i18n';
import { Markdown } from '../Markdown';
import { drillWeComWelcome, fetchWeComState, injectWeComText, WeComApiError } from './wecomApi';
import type { WeComEntry, WeComSnapshot, WeComState } from './wecomTypes';

interface WeComPanelProps {
  serverToken: string;
  onAuthRequired: () => void;
}

/** Poll interval while the console is open. The reply to an inject is produced
 *  asynchronously by the bridge, so the panel watches rather than waits. */
const POLL_MS = 3000;

const STATE_TONE: Record<WeComState, string> = {
  connected: 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30',
  mock: 'bg-amber-500/15 text-amber-400 border-amber-500/30',
  connecting: 'bg-accent-cyan/15 text-accent-cyan border-accent-cyan/30',
  disconnected: 'bg-surface-2 text-text-dim border-border-default',
  error: 'bg-danger/15 text-danger border-danger/40',
};

function formatTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleTimeString();
}

/** Entries are newest-last from the server; the console reads newest-first. */
function newestFirst(entries: WeComEntry[]): WeComEntry[] {
  return [...entries].reverse();
}

export function WeComPanel({ serverToken, onAuthRequired }: WeComPanelProps) {
  const { t } = useTranslation();
  const [snapshot, setSnapshot] = useState<WeComSnapshot | null>(null);
  const [loadError, setLoadError] = useState('');
  const [input, setInput] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<{ tone: 'ok' | 'error'; text: string } | null>(null);

  const aliveRef = useRef(true);

  const load = useCallback(async () => {
    try {
      const snap = await fetchWeComState(serverToken);
      if (!aliveRef.current) return;
      setSnapshot(snap);
      setLoadError('');
    } catch (err) {
      if (!aliveRef.current) return;
      const msg = err instanceof WeComApiError ? err.message : t('wecom.loadFailed');
      setLoadError(msg || t('wecom.loadFailed'));
    }
  }, [serverToken, t]);

  useEffect(() => {
    aliveRef.current = true;
    void load();
    const id = window.setInterval(() => void load(), POLL_MS);
    return () => {
      aliveRef.current = false;
      window.clearInterval(id);
    };
  }, [load]);

  const injectAllowed = snapshot?.inject_allowed ?? false;

  const handleInject = useCallback(async () => {
    const text = input.trim();
    if (!text || busy) return;
    setBusy(true);
    setNotice(null);

    try {
      await injectWeComText(text, serverToken);
      setInput('');
      setNotice({ tone: 'ok', text: t('wecom.injectAccepted') });
      // Pull immediately so the reply shows up without waiting a full tick.
      await load();
    } catch (err) {
      if (err instanceof WeComApiError && (err.status === 401 || err.status === 403)) {
        setNotice({
          tone: 'error',
          text: err.status === 403 ? t('wecom.injectDisabledNoToken') : t('wecom.injectNeedsAdmin'),
        });
        if (err.status === 401) onAuthRequired();
      } else {
        const msg = err instanceof WeComApiError ? err.message : t('wecom.injectFailed');
        setNotice({ tone: 'error', text: msg || t('wecom.injectFailed') });
      }
    } finally {
      setBusy(false);
    }
  }, [input, busy, serverToken, t, load, onAuthRequired]);

  const handleWelcomeDrill = useCallback(async () => {
    setBusy(true);
    setNotice(null);
    try {
      await drillWeComWelcome(serverToken);
      setNotice({ tone: 'ok', text: t('wecom.welcomeAccepted') });
      await load();
    } catch (err) {
      const msg = err instanceof WeComApiError ? err.message : t('wecom.injectFailed');
      setNotice({ tone: 'error', text: msg || t('wecom.injectFailed') });
    } finally {
      setBusy(false);
    }
  }, [serverToken, t, load]);

  const state: WeComState = snapshot?.state ?? 'disconnected';
  const recent = newestFirst(snapshot?.recent ?? []);
  const inbound = recent.filter((e) => e.kind === 'inbound');
  const outbound = recent.filter((e) => e.kind === 'outbound');
  const proposals = outbound.filter((e) => e.channel === 'card');

  return (
    <div className="space-y-5" data-testid="wecom-panel">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3 min-w-0">
          <div className="w-9 h-9 rounded-xl bg-accent-cyan/15 border border-accent-cyan/30 flex items-center justify-center shrink-0">
            <Bot className="w-4 h-4 text-accent-cyan" />
          </div>
          <div className="min-w-0">
            <h2 className="text-[20px] font-extrabold text-text break-words">{t('wecom.title')}</h2>
            <p className="text-[12px] text-text-dim mt-0.5 break-words">{t('wecom.subtitle')}</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span
            data-testid="wecom-state"
            data-state={state}
            className={`inline-flex items-center gap-1.5 px-3 py-1 rounded-full border text-[11px] font-bold tracking-wide ${STATE_TONE[state]}`}
          >
            <span className="w-1.5 h-1.5 rounded-full bg-current" />
            {t(`wecom.state.${state}`)}
          </span>
          <button
            type="button"
            onClick={() => void load()}
            data-testid="wecom-refresh"
            aria-label={t('wecom.refresh')}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-surface-2 hover:bg-surface-3 border border-border-subtle text-text-dim hover:text-text text-[11px] font-bold transition-all cursor-pointer"
          >
            <RefreshCw className="w-3 h-3" />
            {t('wecom.refresh')}
          </button>
        </div>
      </div>

      {/* Load failure */}
      {loadError ? (
        <div
          role="alert"
          data-testid="wecom-error"
          className="rounded-[12px] bg-danger/10 border border-danger/40 p-3 text-danger text-[12px] flex items-start gap-2"
        >
          <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
          <div className="min-w-0">
            <p className="font-bold">{t('wecom.loadFailed')}</p>
            <p className="break-words">{loadError}</p>
            <button
              type="button"
              onClick={() => void load()}
              className="mt-2 underline font-bold cursor-pointer"
            >
              {t('wecom.retry')}
            </button>
          </div>
        </div>
      ) : null}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-5 items-start">
        <div className="space-y-5 min-w-0">
          {/* Connection */}
          <section className="rounded-[16px] bg-surface-1 border border-border-light p-5">
            <h3 className="text-[13px] font-extrabold text-text mb-3">{t('wecom.connection')}</h3>
            <dl className="grid grid-cols-1 sm:grid-cols-[9rem_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-[12px]">
              <dt className="text-text-dim font-semibold">{t('wecom.field.mode')}</dt>
              <dd className="m-0 break-words">{snapshot?.mode ?? 'off'}</dd>
              <dt className="text-text-dim font-semibold">{t('wecom.field.endpoint')}</dt>
              <dd className="m-0 break-words font-mono text-[11px]">
                {snapshot?.ws_url ?? 'wss://openws.work.weixin.qq.com'}
              </dd>
              <dt className="text-text-dim font-semibold">{t('wecom.field.botId')}</dt>
              <dd className="m-0 break-words font-mono text-[11px]">
                {snapshot?.bot_id_masked || t('wecom.notConfigured')}
              </dd>
              <dt className="text-text-dim font-semibold">{t('wecom.field.secret')}</dt>
              <dd className="m-0 break-words">
                {snapshot?.has_secret ? t('wecom.secretConfigured') : t('wecom.notConfigured')}
              </dd>
              <dt className="text-text-dim font-semibold">{t('wecom.field.replyMode')}</dt>
              <dd className="m-0 break-words">{snapshot?.reply_mode ?? 'stream'}</dd>
              <dt className="text-text-dim font-semibold">{t('wecom.field.groupWebhook')}</dt>
              <dd className="m-0 break-words">
                {snapshot?.group_webhook_enabled ? t('wecom.enabledWord') : t('wecom.disabledWord')}
              </dd>
            </dl>

            {snapshot?.state === 'mock' ? (
              <div
                data-testid="wecom-mock-notice"
                className="mt-4 rounded-[12px] bg-amber-500/10 border border-amber-500/40 p-3 text-[12px] text-amber-400"
              >
                <p className="font-bold m-0">{t('wecom.mockTitle')}</p>
                <p className="m-0 mt-1 break-words">{snapshot.state_reason || t('wecom.mockBody')}</p>
              </div>
            ) : null}

            {snapshot?.state === 'error' ? (
              <div
                role="alert"
                className="mt-4 rounded-[12px] bg-danger/10 border border-danger/40 p-3 text-[12px] text-danger"
              >
                <p className="font-bold m-0">{t('wecom.errorTitle')}</p>
                <p className="m-0 mt-1 break-words">{snapshot.state_reason || t('wecom.errorBody')}</p>
              </div>
            ) : null}
          </section>

          {/* Inject */}
          <section className="rounded-[16px] bg-surface-1 border border-border-light p-5">
            <h3 className="text-[13px] font-extrabold text-text mb-1">{t('wecom.injectTitle')}</h3>
            <p className="text-[12px] text-text-dim m-0 mb-3">{t('wecom.injectHint')}</p>

            {!injectAllowed ? (
              <div
                data-testid="wecom-guest-notice"
                className="mb-3 rounded-[12px] bg-surface-2 border border-border-default p-3 text-[12px] text-text-dim flex items-start gap-2"
              >
                <Lock className="w-4 h-4 shrink-0 mt-0.5" />
                <p className="m-0 break-words">{t('wecom.guestNotice')}</p>
              </div>
            ) : null}

            <div className="flex flex-col sm:flex-row gap-2">
              <label htmlFor="wecom-inject-input" className="sr-only">
                {t('wecom.injectAria')}
              </label>
              <input
                id="wecom-inject-input"
                data-testid="wecom-inject-input"
                type="text"
                value={input}
                disabled={!injectAllowed || busy}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') void handleInject();
                }}
                placeholder={t('wecom.injectPlaceholder')}
                className="flex-1 min-w-0 px-3 py-2.5 rounded-xl bg-surface-2 border border-border-default text-text text-[13px] disabled:opacity-50 disabled:cursor-not-allowed focus:outline-none focus:ring-2 focus:ring-accent-cyan/60"
              />
              <button
                type="button"
                data-testid="wecom-inject-send"
                onClick={() => void handleInject()}
                disabled={!injectAllowed || busy || !input.trim()}
                className="w-full sm:w-auto shrink-0 inline-flex items-center justify-center gap-1.5 px-5 py-2.5 rounded-xl bg-accent-cyan text-bg border border-accent-cyan text-[12px] font-extrabold transition-all disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
              >
                <Send className="w-3.5 h-3.5" />
                {injectAllowed ? t('wecom.inject') : t('wecom.injectNeedsAdminShort')}
              </button>
            </div>

            <div className="flex flex-wrap gap-1.5 mt-2.5">
              {[t('wecom.suggest1'), t('wecom.suggest2'), t('wecom.suggest3'), t('wecom.suggest4')].map(
                (q) => (
                  <button
                    key={q}
                    type="button"
                    disabled={!injectAllowed}
                    onClick={() => setInput(q)}
                    className="px-2.5 py-1 rounded-lg bg-surface-2 border border-border-light text-text-dim hover:text-text text-[11px] font-semibold text-left disabled:opacity-45 disabled:cursor-not-allowed cursor-pointer"
                  >
                    {q}
                  </button>
                )
              )}
            </div>

            {injectAllowed ? (
              <button
                type="button"
                onClick={() => void handleWelcomeDrill()}
                disabled={busy}
                data-testid="wecom-welcome-drill"
                className="mt-3 text-[11px] font-bold text-accent-cyan underline disabled:opacity-40 cursor-pointer"
              >
                {t('wecom.welcomeDrill')}
              </button>
            ) : null}

            {notice ? (
              <p
                role={notice.tone === 'error' ? 'alert' : 'status'}
                data-testid="wecom-inject-notice"
                className={`mt-3 mb-0 text-[12px] break-words ${
                  notice.tone === 'error' ? 'text-danger' : 'text-success'
                }`}
              >
                {notice.text}
              </p>
            ) : null}
          </section>
        </div>

        <div className="space-y-5 min-w-0">
          {/* Pending confirmation */}
          {proposals.length > 0 ? (
            <section
              data-testid="wecom-proposal"
              className="rounded-[16px] border p-5"
              style={{
                borderColor: 'color-mix(in srgb, var(--theme-warning) 45%, transparent)',
                background: 'color-mix(in srgb, var(--theme-warning) 9%, transparent)',
              }}
            >
              <h3 className="text-[13px] font-extrabold text-warning mb-2 flex items-center gap-2">
                <ShieldAlert className="w-4 h-4" />
                {t('wecom.proposalTitle')}
              </h3>
              {proposals.map((p, i) => (
                <p key={`${p.time}-${i}`} className="m-0 text-[12px] text-text break-words">
                  {p.text}
                </p>
              ))}
              <p className="mt-3 mb-0 pt-3 border-t border-warning/30 text-[12px] font-bold text-text break-words">
                {t('wecom.proposalGate')}
              </p>
            </section>
          ) : null}

          {/* Recent inbound */}
          <section
            data-testid="wecom-recent-inbound"
            className="rounded-[16px] bg-surface-1 border border-border-light p-5"
          >
            <h3 className="text-[13px] font-extrabold text-text mb-3 flex items-center gap-2">
              <Inbox className="w-4 h-4 text-accent-cyan" />
              {t('wecom.recentInbound')}
            </h3>
            {inbound.length === 0 ? (
              <p className="text-[12px] text-text-dim m-0 py-4 text-center">
                {t('wecom.emptyInbound')}
              </p>
            ) : (
              <ul className="list-none m-0 p-0 space-y-2.5">
                {inbound.slice(0, 8).map((e, i) => (
                  <li
                    key={`${e.time}-${i}`}
                    data-testid="wecom-inbound-item"
                    className="border-t border-border-subtle first:border-t-0 pt-2.5 first:pt-0"
                  >
                    <div className="text-[10px] font-bold text-text-dim tracking-wide">
                      {e.source} · {formatTime(e.time)}
                      {e.user ? ` · ${e.user}` : ''}
                    </div>
                    <div className="text-[12.5px] break-words">{e.text}</div>
                  </li>
                ))}
              </ul>
            )}
          </section>

          {/* Recent replies */}
          <section
            data-testid="wecom-recent-outbound"
            className="rounded-[16px] bg-surface-1 border border-border-light p-5"
          >
            <h3 className="text-[13px] font-extrabold text-text mb-3 flex items-center gap-2">
              <MessageSquare className="w-4 h-4 text-accent-cyan" />
              {t('wecom.recentOutbound')}
            </h3>
            {outbound.length === 0 ? (
              <p className="text-[12px] text-text-dim m-0 py-4 text-center">
                {t('wecom.emptyOutbound')}
              </p>
            ) : (
              <ul className="list-none m-0 p-0 space-y-2.5">
                {outbound.slice(0, 8).map((e, i) => (
                  <li
                    key={`${e.time}-${i}`}
                    data-testid="wecom-outbound-item"
                    className="border-t border-border-subtle first:border-t-0 pt-2.5 first:pt-0"
                  >
                    <div className="text-[10px] font-bold text-text-dim tracking-wide">
                      {e.channel || 'reply'} · {formatTime(e.time)}
                    </div>
                    <Markdown text={e.text} className="text-[12.5px] leading-relaxed break-words [&_p]:my-1" />
                  </li>
                ))}
              </ul>
            )}
          </section>

          {/* Events */}
          {recent.some((e) => e.kind === 'event') ? (
            <section className="rounded-[16px] bg-surface-1 border border-border-light p-5">
              <h3 className="text-[13px] font-extrabold text-text mb-3">
                {t('wecom.recentEvents')}
              </h3>
              <ul className="list-none m-0 p-0 space-y-1.5">
                {recent
                  .filter((e) => e.kind === 'event')
                  .slice(0, 6)
                  .map((e, i) => (
                    <li key={`${e.time}-${i}`} className="text-[11.5px] text-text-dim break-words">
                      <span className="font-bold">{formatTime(e.time)}</span> {e.text}
                    </li>
                  ))}
              </ul>
            </section>
          ) : null}
        </div>
      </div>
    </div>
  );
}

export default WeComPanel;
