import { useCallback, useState } from 'react';
import { useTranslation } from '../i18n';
import { useTheme } from '../hooks/useTheme';
import { AuditAuthError, useAudit } from '../hooks/useAudit';
import { AuditFiltersBar } from './AuditFiltersBar';
import { AuditTable } from './AuditTable';
import { Download, RefreshCw, ClipboardList, AlertTriangle, Loader2, FileJson, FileText, Inbox } from 'lucide-react';

interface Props {
  token: string;
  onAuthRequired: () => void;
}

export function AuditPanel({ token, onAuthRequired }: Props) {
  const { t } = useTranslation();
  const { theme } = useTheme();
  const [downloading, setDownloading] = useState<null | 'json' | 'md'>(null);
  const { page, stats, loading, error, filters, updateFilters, refresh, exportUrl } = useAudit({ token, enabled: true });

  const handleExport = useCallback(async (format: 'json' | 'md') => {
    setDownloading(format);
    try {
      const resp = await fetch(exportUrl(format));
      if (resp.status === 401) {
        onAuthRequired();
        return;
      }
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      const blob = await resp.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      const ext = format === 'md' ? 'md' : 'json';
      a.download = `audit-${Date.now()}.${ext}`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (e) {
      // eslint-disable-next-line no-alert
      alert(t('audit.exportFailed', { error: (e as Error).message }));
    } finally {
      setDownloading(null);
    }
  }, [exportUrl, onAuthRequired, t]);

  // auth error triggers re-prompt
  if (page === null && error === null && loading) {
    return (
      <div className="rounded-[16px] bg-surface-1 border border-border-light p-10 text-center text-text-dim">
        <Loader2 className="animate-spin w-5 h-5 inline mr-2" />{t('audit.loading')}
      </div>
    );
  }

  return (
    <div className="space-y-5" data-testid="audit-panel" data-theme-attr={theme}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-[20px] font-extrabold flex items-center gap-2 text-text">
            <ClipboardList className="w-5 h-5 text-accent-cyan" />
            {t('audit.title')}
          </h2>
          <p className="text-[12px] text-text-dim mt-1">{t('audit.subtitle')}</p>
        </div>
        <div className="flex gap-2">
          <button className="btn-surface" onClick={refresh} title={t('audit.refresh')} aria-label={t('audit.refresh')} data-testid="audit-refresh">
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
          <button className="btn-surface" onClick={() => handleExport('json')} disabled={downloading === 'json'} data-testid="export-json" title={t('audit.exportJson')}>
            {downloading === 'json' ? <Loader2 className="w-4 h-4 animate-spin" /> : <FileJson className="w-4 h-4" />}
            <span className="hidden sm:inline ml-2">{t('audit.exportJson')}</span>
          </button>
          <button className="btn-surface" onClick={() => handleExport('md')} disabled={downloading === 'md'} data-testid="export-md" title={t('audit.exportMd')}>
            {downloading === 'md' ? <Loader2 className="w-4 h-4 animate-spin" /> : <FileText className="w-4 h-4" />}
            <span className="hidden sm:inline ml-2">{t('audit.exportMd')}</span>
          </button>
        </div>
      </div>

      {stats && (
        <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
          <StatTile label={t('audit.totalEvents')} value={stats.total} />
          <StatTile label={t('audit.last24h')} value={stats.last_24h} color="var(--accent-cyan)" />
          <StatTile label={t('audit.failures24h')} value={stats.failures_24h} color="var(--danger)" />
          <StatTile label={t('audit.denied24h')} value={stats.denied_24h} color="var(--warning)" />
          <StatTile label={t('audit.retention')} value={`${stats.retention_days}d`} />
        </div>
      )}

      <AuditFiltersBar filters={filters} onChange={updateFilters} />

      {error ? (
        <div role="alert" className="rounded-[16px] bg-danger/10 border border-danger/40 p-6 text-center" data-testid="audit-error">
          <AlertTriangle className="w-8 h-8 mx-auto mb-2 text-danger" />
          <div className="font-bold text-danger">{t('audit.errorTitle')}</div>
          <p className="text-[13px] text-text-dim mt-1">{error}</p>
          <button className="btn-surface mt-3" onClick={refresh}>
            <RefreshCw className="w-4 h-4 mr-2" />{t('audit.retry')}
          </button>
        </div>
      ) : page && page.total === 0 ? (
        <div className="rounded-[16px] bg-surface-1 border border-border-light p-10 text-center text-text-dim" data-testid="audit-empty">
          <div className="text-4xl"><Inbox className="w-8 h-8 mx-auto text-text-dim" /></div>
          <div className="font-bold text-text mt-2">{t('audit.emptyTitle')}</div>
          <p className="text-[13px] mt-1 max-w-md mx-auto">{t('audit.emptyHint')}</p>
        </div>
      ) : page ? (
        <AuditTable page={page} filters={filters} onChange={updateFilters} />
      ) : null}

      <style>{`
        .btn-surface { display:inline-flex; align-items:center; gap:6px; padding:8px 14px; border-radius:10px;
          background:var(--surface-1); border:1px solid var(--border-light); color:var(--text);
          font-weight:600; font-size:12px; cursor:pointer; transition:all .15s ease; }
        .btn-surface:hover { background:var(--surface-4); border-color:var(--border-default); }
        .btn-surface:disabled { opacity:.5; cursor:default; }
      `}</style>
    </div>
  );
}

function StatTile({ label, value, color }: { label: string; value: number | string; color?: string }) {
  return (
    <div className="rounded-[12px] bg-surface-1 border border-border-light p-3">
      <div className="text-[10px] uppercase tracking-wider text-text-dim">{label}</div>
      <div className="text-[22px] font-extrabold mt-1" style={{ color: color || 'var(--text)' }}>{value}</div>
    </div>
  );
}

// Silence unused import guards
void AuditAuthError;
void Download;
