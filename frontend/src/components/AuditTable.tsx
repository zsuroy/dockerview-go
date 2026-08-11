import React, { useState } from 'react';
import { useTranslation } from '../i18n';
import type { AuditFilters, AuditPage } from '../hooks/useAudit';
import { ChevronDown, ChevronRight } from 'lucide-react';

interface Props {
  page: AuditPage;
  filters?: AuditFilters;
  onChange: (next: Partial<AuditFilters>) => void;
}

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, {
      year: 'numeric', month: '2-digit', day: '2-digit',
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    });
  } catch {
    return iso;
  }
}

export function AuditTable({ page, onChange }: Props) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState<number | null>(null);

  const from = page.total === 0 ? 0 : page.offset + 1;
  const to = page.offset + page.count;
  const totalPages = Math.max(1, Math.ceil(page.total / (page.limit || 50)));
  const currentPage = Math.floor(page.offset / (page.limit || 50)) + 1;

  return (
    <div className="rounded-[14px] bg-surface-1 border border-border-light overflow-hidden" data-testid="audit-table">
      <div className="overflow-x-auto">
        <table className="w-full text-[13px]">
          <thead>
            <tr className="text-left bg-surface-2 text-[10px] uppercase tracking-wider text-text-dim">
              <th className="py-2.5 px-3 w-8"></th>
              <th className="py-2.5 px-3">{t('audit.time')}</th>
              <th className="py-2.5 px-3">{t('audit.actor')}</th>
              <th className="py-2.5 px-3">{t('audit.source')}</th>
              <th className="py-2.5 px-3">{t('audit.action')}</th>
              <th className="py-2.5 px-3">{t('audit.container')}</th>
              <th className="py-2.5 px-3">{t('audit.result')}</th>
              <th className="py-2.5 px-3">{t('audit.duration')}</th>
            </tr>
          </thead>
          <tbody>
            {page.items.map((it) => {
              const isOpen = expanded === it.id;
              const pillClass =
                it.result === 'success'
                  ? 'bg-success/15 text-success border-success/30'
                  : it.result === 'failure'
                  ? 'bg-danger/15 text-danger border-danger/30'
                  : 'bg-warning/15 text-warning border-warning/30';
              const shortId = it.container_id ? it.container_id.slice(0, 12) : '';
              const containerName = it.container_name || shortId || t('audit.unknown');
              return (
                <React.Fragment key={it.id}>
                  <tr className="border-t border-border-subtle hover:bg-surface-2/60 cursor-pointer" onClick={() => setExpanded(isOpen ? null : it.id)} data-testid={`audit-row-${it.id}`}>
                    <td className="py-2 px-3 text-text-dim">
                      {isOpen ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
                    </td>
                    <td className="py-2 px-3 font-mono text-[12px] whitespace-nowrap">{formatTime(it.time)}</td>
                    <td className="py-2 px-3 font-mono text-[12px]">{it.actor}</td>
                    <td className="py-2 px-3 text-[12px] text-text-dim">{it.source}</td>
                    <td className="py-2 px-3 font-bold">{t(`audit.${it.action}`)}</td>
                    <td className="py-2 px-3">
                      <div className="font-semibold text-text" data-testid="audit-container-name">{containerName}</div>
                      {it.container_id && <div className="text-[11px] text-text-dim font-mono" data-testid="audit-container-id">{it.container_id.slice(0, 19)}</div>}
                    </td>
                    <td className="py-2 px-3">
                      <span className={`inline-block px-2 py-0.5 rounded-full border text-[11px] font-bold ${pillClass}`}>
                        {t(`audit.${it.result}`)} {it.status_code ? <span className="opacity-70 font-mono ml-1">{it.status_code}</span> : null}
                      </span>
                    </td>
                    <td className="py-2 px-3 font-mono text-[12px] text-text-dim">{t('audit.durationMs', { ms: it.duration_ms })}</td>
                  </tr>
                  {isOpen && (
                    <tr key={`${it.id}-d`} className="bg-surface-2/70 border-t border-border-subtle">
                      <td colSpan={8} className="py-3 px-6 text-[12px]">
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                          <Field k={t('audit.detail')} v={it.detail || '—'} />
                          <Field k="Request ID" v={<code className="font-mono text-[11px]">{it.request_id || '—'}</code>} />
                          <Field k="Client IP" v={it.client_ip || '—'} />
                          <Field k="User-Agent" v={it.user_agent || '—'} />
                          {it.payload && Object.keys(it.payload).length > 0 && (
                            <div className="md:col-span-2">
                              <div className="text-[10px] uppercase tracking-wider text-text-dim mb-1">Payload</div>
                              <pre className="bg-[color:var(--code-bg,#0a0f1f)] border border-border-subtle rounded-[8px] p-2 text-[11px] font-mono overflow-x-auto text-text">
                                {JSON.stringify(it.payload, null, 2)}
                              </pre>
                            </div>
                          )}
                        </div>
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2 p-3 text-[12px] text-text-dim">
        <span>{t('audit.rowsShown', { from, to, total: page.total })}</span>
        <div className="flex items-center gap-2">
          <button
            className="btn-xs"
            disabled={currentPage <= 1}
            onClick={() => onChange({ offset: Math.max(0, (currentPage - 2) * (page.limit || 50)) })}
          >
            {t('audit.prev')}
          </button>
          <span className="font-mono">{currentPage} / {totalPages}</span>
          <button
            className="btn-xs"
            disabled={to >= page.total}
            onClick={() => onChange({ offset: currentPage * (page.limit || 50) })}
          >
            {t('audit.next')}
          </button>
        </div>
      </div>
      <style>{`
        .btn-xs { padding:6px 10px; border-radius:8px; background:var(--surface-2);
          border:1px solid var(--border-subtle); color:var(--text); font-weight:600;
          font-size:11px; cursor:pointer; }
        .btn-xs:disabled { opacity:.4; cursor:default; }
      `}</style>
    </div>
  );
}

function Field({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div>
      <div className="text-[10px] uppercase tracking-wider text-text-dim mb-1">{k}</div>
      <div className="text-text">{v}</div>
    </div>
  );
}
