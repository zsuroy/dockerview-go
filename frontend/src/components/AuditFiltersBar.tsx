import { useTranslation } from '../i18n';
import type { AuditFilters } from '../hooks/useAudit';

interface Props {
  filters: AuditFilters;
  onChange: (next: Partial<AuditFilters>) => void;
}

const ACTIONS = ['start', 'stop', 'restart', 'exec'] as const;
const RESULTS = ['success', 'failure', 'denied'] as const;

export function AuditFiltersBar({ filters, onChange }: Props) {
  const { t } = useTranslation();

  const toggle = (key: 'action' | 'result', value: string) => {
    const cur = (filters[key] as string[] | undefined) ?? [];
    const next = cur.includes(value) ? cur.filter(x => x !== value) : [...cur, value];
    onChange({ [key]: next.length ? next : undefined, offset: 0 } as Partial<AuditFilters>);
  };

  const reset = () => onChange({
    since: undefined, until: undefined, container_id: undefined, container_name: undefined,
    action: undefined, result: undefined, limit: 50, offset: 0, sort: 'time_desc',
  });

  const hasAny = Boolean(filters.since || filters.until || filters.container_id || filters.container_name
    || (filters.action && filters.action.length) || (filters.result && filters.result.length)
    || (filters.limit && filters.limit !== 50));

  return (
    <div className="rounded-[14px] bg-surface-1 border border-border-light p-4 grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-3" data-testid="audit-filters">
      <label className="block">
        <span className="text-[10px] uppercase tracking-wider text-text-dim">{t('audit.since')}</span>
        <input
          type="datetime-local"
          className="mt-1 w-full rounded-[8px] bg-surface-2 border border-border-subtle px-3 py-2 text-text text-[13px]"
          value={filters.since ?? ''}
          onChange={(e) => onChange({ since: e.target.value || undefined, offset: 0 })}
          aria-label={t('audit.since')}
        />
      </label>
      <label className="block">
        <span className="text-[10px] uppercase tracking-wider text-text-dim">{t('audit.until')}</span>
        <input
          type="datetime-local"
          className="mt-1 w-full rounded-[8px] bg-surface-2 border border-border-subtle px-3 py-2 text-text text-[13px]"
          value={filters.until ?? ''}
          onChange={(e) => onChange({ until: e.target.value || undefined, offset: 0 })}
          aria-label={t('audit.until')}
        />
      </label>
      <label className="block">
        <span className="text-[10px] uppercase tracking-wider text-text-dim">{t('audit.containerId')}</span>
        <input
          type="text"
          placeholder={t('audit.containerIdPlaceholder')}
          className="mt-1 w-full rounded-[8px] bg-surface-2 border border-border-subtle px-3 py-2 text-text text-[13px]"
          value={filters.container_id ?? ''}
          onChange={(e) => onChange({ container_id: e.target.value || undefined, offset: 0 })}
          aria-label={t('audit.containerId')}
        />
      </label>
      <label className="block">
        <span className="text-[10px] uppercase tracking-wider text-text-dim">{t('audit.containerName')}</span>
        <input
          type="text"
          placeholder={t('audit.containerNamePlaceholder')}
          className="mt-1 w-full rounded-[8px] bg-surface-2 border border-border-subtle px-3 py-2 text-text text-[13px]"
          value={filters.container_name ?? ''}
          onChange={(e) => onChange({ container_name: e.target.value || undefined, offset: 0 })}
          aria-label={t('audit.containerName')}
        />
      </label>
      <label className="block">
        <span className="text-[10px] uppercase tracking-wider text-text-dim">{t('audit.limit')}</span>
        <select
          className="mt-1 w-full rounded-[8px] bg-surface-2 border border-border-subtle px-3 py-2 text-text text-[13px]"
          value={filters.limit ?? 50}
          onChange={(e) => onChange({ limit: Number(e.target.value), offset: 0 })}
          aria-label={t('audit.limit')}
        >
          <option value={25}>25</option>
          <option value={50}>50</option>
          <option value={100}>100</option>
          <option value={200}>200</option>
        </select>
      </label>
      <label className="block">
        <span className="text-[10px] uppercase tracking-wider text-text-dim">{t('audit.sort')}</span>
        <select
          className="mt-1 w-full rounded-[8px] bg-surface-2 border border-border-subtle px-3 py-2 text-text text-[13px]"
          value={filters.sort ?? 'time_desc'}
          onChange={(e) => onChange({ sort: e.target.value as 'time_desc' | 'time_asc', offset: 0 })}
          aria-label={t('audit.sort')}
        >
          <option value="time_desc">{t('audit.sortDesc')}</option>
          <option value="time_asc">{t('audit.sortAsc')}</option>
        </select>
      </label>
      <div className="md:col-span-2 flex items-end">
        <button
          type="button"
          onClick={reset}
          disabled={!hasAny}
          className="px-3 py-2 rounded-[8px] bg-surface-2 border border-border-subtle text-text-dim text-[12px] font-semibold hover:text-text disabled:opacity-40 disabled:cursor-default"
          data-testid="audit-reset"
        >
          {t('audit.reset')}
        </button>
      </div>

      <div className="md:col-span-2 lg:col-span-2">
        <div className="text-[10px] uppercase tracking-wider text-text-dim mb-1">{t('audit.action')}</div>
        <div className="flex flex-wrap gap-1.5">
          {ACTIONS.map(a => {
            const active = (filters.action ?? []).includes(a);
            return (
              <button
                key={a}
                type="button"
                onClick={() => toggle('action', a)}
                aria-pressed={active}
                data-testid={`chip-action-${a}`}
                className={`px-3 py-1 rounded-full text-[11px] font-bold border ${active ? 'bg-accent-cyan text-[#041026] border-accent-cyan' : 'bg-surface-2 border-border-subtle text-text-dim'}`}
              >
                {t(`audit.${a}`)}
              </button>
            );
          })}
        </div>
      </div>
      <div className="md:col-span-2 lg:col-span-2">
        <div className="text-[10px] uppercase tracking-wider text-text-dim mb-1">{t('audit.result')}</div>
        <div className="flex flex-wrap gap-1.5">
          {RESULTS.map(r => {
            const active = (filters.result ?? []).includes(r);
            return (
              <button
                key={r}
                type="button"
                onClick={() => toggle('result', r)}
                aria-pressed={active}
                data-testid={`chip-result-${r}`}
                className={`px-3 py-1 rounded-full text-[11px] font-bold border ${active ? (r === 'success' ? 'bg-success text-[#041026] border-success' : r === 'failure' ? 'bg-danger text-white border-danger' : 'bg-warning text-[#2a1d00] border-warning') : 'bg-surface-2 border-border-subtle text-text-dim'}`}
              >
                {t(`audit.${r}`)}
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}
