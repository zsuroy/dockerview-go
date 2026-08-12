import { useCallback, useEffect, useReducer } from 'react';
import {
  Archive, Loader2, AlertTriangle, Eye, PackagePlus, RefreshCw,
  Download, Trash2, Inbox, HardDrive, Image as ImageIcon,
} from 'lucide-react';
import { useTranslation } from '../i18n';
import {
  backupReducer, initialBackupState, canCreate, classifyBackupError,
  formatBytes, formatTimestamp, sortBackups, totalSize, archivesWithImages,
} from '../backup/logic';
import {
  fetchBackupPreview, createBackup, fetchBackupList, deleteBackup, backupDownloadUrl,
} from '../backup/api';
import type { BackupListItem } from '../backup/types';

interface Props {
  token: string;
  onAuthRequired: () => void;
  onToast: (message: string, type?: 'info' | 'success' | 'error') => void;
}

export function BackupPanel({ token, onAuthRequired, onToast }: Props) {
  const { t } = useTranslation();
  const [state, dispatch] = useReducer(backupReducer, initialBackupState);

  const isAdmin = !!token;

  // Load history on mount and whenever the token changes.
  const loadList = useCallback(async () => {
    if (!token) return;
    dispatch({ type: 'list/loading' });
    try {
      const report = await fetchBackupList(token);
      dispatch({ type: 'list/success', report });
    } catch (e) {
      const err = e as Error & { status?: number };
      if (err.status === 401) {
        onAuthRequired();
        return;
      }
      dispatch({ type: 'list/error', error: err.message });
    }
  }, [token, onAuthRequired]);

  useEffect(() => {
    loadList();
  }, [loadList]);

  // Preview the packing plan for the current include_images choice.
  const handlePreview = useCallback(async () => {
    if (!token) {
      onAuthRequired();
      return;
    }
    dispatch({ type: 'preview/loading' });
    try {
      const report = await fetchBackupPreview(token, state.includeImages, state.includeStopped);
      dispatch({ type: 'preview/success', report });
    } catch (e) {
      const err = e as Error & { status?: number };
      if (err.status === 401) {
        onAuthRequired();
        return;
      }
      dispatch({ type: 'preview/error', error: err.message });
    }
  }, [token, state.includeImages, state.includeStopped, onAuthRequired]);

  // Create one snapshot package. Busy state blocks double-submit.
  const handleCreate = useCallback(async () => {
    if (!token) {
      onAuthRequired();
      return;
    }
    if (!canCreate(state)) return;
    dispatch({ type: 'create/start' });
    try {
      const report = await createBackup(token, {
        include_images: state.includeImages,
        include_stopped: state.includeStopped,
        note: state.note,
      });
      dispatch({ type: 'create/success', report });
      onToast(t('backup.toastCreated', { name: report.name }), 'success');
      await loadList();
    } catch (e) {
      const err = e as Error & { status?: number };
      const cls = classifyBackupError(err.status ?? 0);
      if (cls.reauth) {
        dispatch({ type: 'create/error', error: t('backup.errAuth') });
        onAuthRequired();
        return;
      }
      dispatch({ type: 'create/error', error: err.message });
      onToast(t('backup.toastCreateFailed'), 'error');
    }
  }, [token, state, dispatch, onToast, t, loadList, onAuthRequired]);

  const handleDelete = useCallback(async (name: string) => {
    if (!token) {
      onAuthRequired();
      return;
    }
    // eslint-disable-next-line no-alert
    if (!window.confirm(t('backup.confirmDelete', { name }))) return;
    dispatch({ type: 'delete/start', name });
    try {
      await deleteBackup(token, name);
      onToast(t('backup.toastDeleted'), 'success');
      await loadList();
    } catch (e) {
      const err = e as Error & { status?: number };
      if (err.status === 401) {
        onAuthRequired();
      } else {
        onToast(t('backup.toastDeleteFailed', { error: err.message }), 'error');
      }
    } finally {
      dispatch({ type: 'delete/stop' });
    }
  }, [token, onToast, t, loadList, onAuthRequired]);

  const backups: BackupListItem[] = state.list ? sortBackups(state.list.backups) : [];

  return (
    <div className="space-y-5" data-testid="backup-panel">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-[20px] font-extrabold flex items-center gap-2 text-text">
            <Archive className="w-5 h-5 text-accent-cyan" />
            {t('backup.title')}
          </h2>
          <p className="text-[12px] text-text-dim mt-1">{t('backup.subtitle')}</p>
        </div>
        <div className="flex items-center gap-2">
          <span className={`text-[10px] font-bold px-2 py-1 rounded-lg border ${isAdmin ? 'bg-success/10 border-success/30 text-success' : 'bg-surface-2 border-surface-5 text-text-dim'}`}>
            {isAdmin ? t('backup.adminBadge') : t('backup.guestBadge')}
          </span>
          <button
            className="btn-surface"
            onClick={loadList}
            disabled={!isAdmin || state.listPhase === 'loading'}
            title={t('backup.refresh')}
            data-testid="backup-refresh"
          >
            <RefreshCw className={`w-4 h-4 ${state.listPhase === 'loading' ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {!isAdmin && (
        <div className="rounded-[16px] bg-surface-1 border border-border-light p-6 text-center" data-testid="backup-auth-prompt">
          <AlertTriangle className="w-6 h-6 mx-auto mb-2 text-warning" />
          <div className="font-bold text-text">{t('backup.authPromptTitle')}</div>
          <p className="text-[13px] text-text-dim mt-1">{t('backup.authPromptHint')}</p>
        </div>
      )}

      {isAdmin && (
        <>
          {/* Preview + Create */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            {/* Preview card */}
            <div className="rounded-[16px] bg-surface-1 border border-border-light p-5">
              <div className="flex items-center justify-between mb-3">
                <div className="font-bold text-text flex items-center gap-2">
                  <Eye className="w-4 h-4 text-accent-cyan" />{t('backup.previewTitle')}
                </div>
                <button
                  className="btn-accent"
                  onClick={handlePreview}
                  disabled={state.previewPhase === 'loading'}
                  data-testid="backup-preview-btn"
                >
                  {state.previewPhase === 'loading'
                    ? <><Loader2 className="w-4 h-4 animate-spin" />{t('backup.previewing')}</>
                    : <><Eye className="w-4 h-4" />{t('backup.previewBtn')}</>}
                </button>
              </div>
              <p className="text-[12px] text-text-dim mb-3">{t('backup.previewHint')}</p>

              {state.previewPhase === 'error' && (
                <div role="alert" className="rounded-[12px] bg-danger/10 border border-danger/40 p-3 text-[12px] text-danger" data-testid="backup-preview-error">
                  <AlertTriangle className="w-4 h-4 inline mr-1" />{state.previewError}
                </div>
              )}

              {state.previewPhase === 'ready' && state.preview && (
                <div className="space-y-2" data-testid="backup-preview-result">
                  <div className="grid grid-cols-2 gap-2">
                    <div className="tile"><div className="tile-k">{t('backup.containers')}</div><div className="tile-v">{state.preview.containers}</div></div>
                    <div className="tile"><div className="tile-k">{t('backup.estSize')}</div><div className="tile-v">{formatBytes(state.preview.estimated_bytes)}</div></div>
                  </div>
                  <div className="text-[11px] text-text-dim font-mono bg-surface-2 border border-border-subtle rounded-lg p-2 leading-relaxed">
                    {state.preview.would_include.join(' · ')}
                  </div>
                  {state.preview.options.include_images && state.preview.images && (
                    <div className="rounded-[12px] bg-warning/10 border border-warning/40 p-3" data-testid="backup-images-warning">
                      <div className="flex items-center gap-1.5 text-warning font-bold text-[12px] mb-1">
                        <HardDrive className="w-3.5 h-3.5" />{t('backup.imagesWarningTitle')}
                      </div>
                      <ul className="text-[11px] text-text-dim space-y-0.5">
                        {state.preview.images.map(img => (
                          <li key={img.ref} className="flex items-center gap-1.5">
                            <ImageIcon className="w-3 h-3" />
                            <span className="font-mono">{img.ref}</span>
                            <span className="opacity-70">({formatBytes(img.size_bytes)})</span>
                          </li>
                        ))}
                      </ul>
                      {state.preview.warnings.map((w, i) => (
                        <p key={i} className="text-[11px] text-warning mt-1">{w}</p>
                      ))}
                    </div>
                  )}
                </div>
              )}

              {state.previewPhase === 'idle' && (
                <div className="text-[12px] text-text-dim italic">{t('backup.previewEmpty')}</div>
              )}
            </div>

            {/* Create card */}
            <div className="rounded-[16px] bg-surface-1 border border-border-light p-5">
              <div className="font-bold text-text flex items-center gap-2 mb-3">
                <PackagePlus className="w-4 h-4 text-accent-cyan" />{t('backup.createTitle')}
              </div>

              <label className="block text-[11px] font-bold text-text-dim uppercase tracking-wider mb-1" htmlFor="backup-note">
                {t('backup.noteLabel')}
              </label>
              <textarea
                id="backup-note"
                className="w-full bg-surface-2 border border-border-light rounded-xl p-3 text-[12px] text-text focus:outline-none focus:border-accent-cyan/40 resize-none"
                rows={2}
                placeholder={t('backup.notePlaceholder')}
                value={state.note}
                onChange={e => dispatch({ type: 'form/note', note: e.target.value })}
                maxLength={500}
                data-testid="backup-note"
              />

              <label className="flex items-start gap-2.5 mt-3 p-3 rounded-xl bg-surface-2 border border-border-subtle cursor-pointer">
                <input
                  type="checkbox"
                  className="mt-0.5 accent-cyan-400"
                  checked={state.includeStopped}
                  onChange={e => dispatch({ type: 'form/includeStopped', checked: e.target.checked })}
                  data-testid="backup-include-stopped"
                />
                <span>
                  <span className="block text-[12px] font-bold text-text">{t('backup.includeStoppedLabel')}</span>
                  <span className="block text-[11px] text-text-dim">{t('backup.includeStoppedHint')}</span>
                </span>
              </label>

              <label className="flex items-start gap-2.5 mt-3 p-3 rounded-xl bg-surface-2 border border-border-subtle cursor-pointer">
                <input
                  type="checkbox"
                  className="mt-0.5 accent-cyan-400"
                  checked={state.includeImages}
                  onChange={e => dispatch({ type: 'form/includeImages', checked: e.target.checked })}
                  data-testid="backup-include-images"
                />
                <span>
                  <span className="block text-[12px] font-bold text-text">{t('backup.includeImagesLabel')}</span>
                  <span className="block text-[11px] text-text-dim">{t('backup.includeImagesHint')}</span>
                </span>
              </label>

              {state.includeImages && (
                <div className="rounded-[12px] bg-warning/10 border border-warning/40 p-3 mt-3 text-[12px] text-warning" data-testid="backup-risk-notice">
                  <AlertTriangle className="w-4 h-4 inline mr-1" />{t('backup.includeImagesRisk')}
                </div>
              )}

              {state.createError && (
                <div role="alert" className="rounded-[12px] bg-danger/10 border border-danger/40 p-3 mt-3 text-[12px] text-danger" data-testid="backup-create-error">
                  <AlertTriangle className="w-4 h-4 inline mr-1" />{state.createError}
                </div>
              )}

              {state.lastCreated && !state.createError && (
                <div className="rounded-[12px] bg-success/10 border border-success/40 p-3 mt-3 text-[12px] text-success" data-testid="backup-created-ok">
                  {t('backup.createdOk', { name: state.lastCreated.name, size: formatBytes(state.lastCreated.size_bytes) })}
                </div>
              )}

              <button
                className="btn-accent w-full justify-center mt-4"
                onClick={handleCreate}
                disabled={!canCreate(state)}
                data-testid="backup-create-btn"
              >
                {state.creating
                  ? <><Loader2 className="w-4 h-4 animate-spin" />{t('backup.creating')}</>
                  : <><PackagePlus className="w-4 h-4" />{t('backup.createBtn')}</>}
              </button>
            </div>
          </div>

          {/* History */}
          <div className="rounded-[16px] bg-surface-1 border border-border-light p-5">
            <div className="flex flex-wrap items-center justify-between gap-2 mb-4">
              <div className="font-bold text-text flex items-center gap-2">
                <Archive className="w-4 h-4 text-accent-cyan" />{t('backup.historyTitle')}
              </div>
              {state.list && (
                <div className="text-[11px] text-text-dim">
                  {t('backup.historyMeta', {
                    count: state.list.backups.length,
                    max: state.list.max_archives,
                    size: formatBytes(totalSize(state.list)),
                    withImages: archivesWithImages(state.list),
                  })}
                </div>
              )}
            </div>

            {state.listPhase === 'loading' && (
              <div className="p-8 text-center text-text-dim" data-testid="backup-list-loading">
                <Loader2 className="animate-spin w-5 h-5 inline mr-2" />{t('backup.listLoading')}
              </div>
            )}

            {state.listPhase === 'error' && (
              <div role="alert" className="rounded-[12px] bg-danger/10 border border-danger/40 p-6 text-center" data-testid="backup-list-error">
                <AlertTriangle className="w-6 h-6 mx-auto mb-2 text-danger" />
                <div className="font-bold text-danger">{t('backup.listErrorTitle')}</div>
                <p className="text-[12px] text-text-dim mt-1">{state.listError}</p>
                <button className="btn-surface mt-3" onClick={loadList}>
                  <RefreshCw className="w-4 h-4 mr-1" />{t('backup.retry')}
                </button>
              </div>
            )}

            {state.listPhase === 'ready' && backups.length === 0 && (
              <div className="p-8 text-center text-text-dim" data-testid="backup-list-empty">
                <Inbox className="w-8 h-8 mx-auto mb-2 opacity-60" />
                <div className="font-bold text-text">{t('backup.listEmptyTitle')}</div>
                <p className="text-[12px] mt-1">{t('backup.listEmptyHint')}</p>
              </div>
            )}

            {state.listPhase === 'ready' && backups.length > 0 && (
              <div className="overflow-x-auto">
                <table className="w-full text-[12px]" data-testid="backup-table">
                  <thead>
                    <tr className="text-left text-[10px] uppercase tracking-wider text-text-dim border-b border-border-subtle">
                      <th className="py-2 pr-3">{t('backup.colName')}</th>
                      <th className="py-2 pr-3">{t('backup.colTime')}</th>
                      <th className="py-2 pr-3">{t('backup.colSize')}</th>
                      <th className="py-2 pr-3">{t('backup.colNote')}</th>
                      <th className="py-2 pr-3">{t('backup.colImages')}</th>
                      <th className="py-2 text-right">{t('backup.colActions')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {backups.map(b => (
                      <tr key={b.name} className="border-b border-border-subtle/50 hover:bg-surface-2/50">
                        <td className="py-2.5 pr-3 font-mono text-[11px] text-text">
                          {b.name}
                          {b.corrupt && <span className="ml-2 text-danger font-bold">{t('backup.corrupt')}</span>}
                        </td>
                        <td className="py-2.5 pr-3 text-text-dim whitespace-nowrap">{formatTimestamp(b.created_at)}</td>
                        <td className="py-2.5 pr-3 text-text-dim whitespace-nowrap">{formatBytes(b.size_bytes)}</td>
                        <td className="py-2.5 pr-3 text-text-dim max-w-[220px] truncate" title={b.note}>{b.note || '—'}</td>
                        <td className="py-2.5 pr-3">
                          {b.include_images
                            ? <span className="badge-img">{t('backup.withImages')}</span>
                            : <span className="badge-meta">{t('backup.metaOnly')}</span>}
                        </td>
                        <td className="py-2.5 text-right whitespace-nowrap">
                          <a
                            className="btn-surface inline-flex"
                            href={backupDownloadUrl(token, b.name)}
                            download={b.name}
                            data-testid={`backup-download-${b.name}`}
                            title={t('backup.download')}
                          >
                            <Download className="w-3.5 h-3.5" />
                          </a>
                          <button
                            className="btn-danger ml-2"
                            onClick={() => handleDelete(b.name)}
                            disabled={state.deletingName === b.name}
                            data-testid={`backup-delete-${b.name}`}
                            title={t('backup.delete')}
                          >
                            {state.deletingName === b.name
                              ? <Loader2 className="w-3.5 h-3.5 animate-spin" />
                              : <Trash2 className="w-3.5 h-3.5" />}
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </>
      )}

      <style>{`
        .btn-accent { display:inline-flex; align-items:center; gap:6px; padding:8px 16px; border-radius:10px;
          background:rgba(34,211,238,.12); border:1px solid rgba(34,211,238,.5); color:var(--accent-cyan);
          font-weight:700; font-size:12px; cursor:pointer; transition:all .15s ease; }
        .btn-accent:hover:not(:disabled) { background:rgba(34,211,238,.2); }
        .btn-accent:disabled { opacity:.45; cursor:not-allowed; }
        .btn-surface { display:inline-flex; align-items:center; gap:6px; padding:8px 12px; border-radius:10px;
          background:var(--surface-1); border:1px solid var(--border-light); color:var(--text);
          font-weight:600; font-size:12px; cursor:pointer; transition:all .15s ease; text-decoration:none; }
        .btn-surface:hover:not(:disabled) { background:var(--surface-4); border-color:var(--border-default); }
        .btn-surface:disabled { opacity:.45; cursor:not-allowed; }
        .btn-danger { display:inline-flex; align-items:center; gap:6px; padding:8px 12px; border-radius:10px;
          background:rgba(248,81,73,.08); border:1px solid rgba(248,81,73,.35); color:var(--danger);
          font-weight:600; font-size:12px; cursor:pointer; transition:all .15s ease; }
        .btn-danger:hover:not(:disabled) { background:rgba(248,81,73,.16); }
        .btn-danger:disabled { opacity:.45; cursor:not-allowed; }
        .tile { background:var(--surface-2); border:1px solid var(--border-subtle); border-radius:10px; padding:10px 12px; }
        .tile-k { font-size:10px; text-transform:uppercase; letter-spacing:1px; color:var(--text-dim); }
        .tile-v { font-size:19px; font-weight:800; margin-top:2px; color:var(--text); }
        .badge-img { display:inline-block; font-size:10px; font-weight:800; padding:2px 8px; border-radius:6px;
          color:var(--warning); border:1px solid rgba(210,153,34,.5); background:rgba(210,153,34,.08); }
        .badge-meta { display:inline-block; font-size:10px; font-weight:800; padding:2px 8px; border-radius:6px;
          color:var(--success); border:1px solid rgba(63,185,80,.45); background:rgba(63,185,80,.08); }
      `}</style>
    </div>
  );
}
