import { useCallback, useEffect, useReducer, useState } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { Trash2, X, AlertTriangle, Loader2, RefreshCw, CheckCircle2, XCircle, SkipForward, ShieldAlert } from 'lucide-react';
import { useTranslation } from '../i18n';
import type { Candidates, DeleteReport, AuditEvent } from '../prune/types';
import {
  initialPruneState, pruneReducer,
  selectedCount, canDryRun, canConfirm, allSelected,
  buildDryRunSelection, buildConfirmRequest,
  classifyHttpError, formatBytes, shortImageId, summarizeItems,
} from '../prune/logic';
import { fetchCandidates, runDryRun, confirmPrune, fetchAudit } from '../prune/api';

interface PruneModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  serverToken: string;
  onAuthRequired: () => void;
  onToast: (message: string, type: 'info' | 'success' | 'error') => void;
}

export function PruneModal({ open, onOpenChange, serverToken, onAuthRequired, onToast }: PruneModalProps) {
  const { t } = useTranslation();
  const [state, dispatch] = useReducer(pruneReducer, initialPruneState);
  const [showAudit, setShowAudit] = useState(false);
  const [auditEvents, setAuditEvents] = useState<AuditEvent[]>([]);

  const isAdmin = !!serverToken;

  const loadCandidates = useCallback(async () => {
    dispatch({ type: 'list/loading' });
    try {
      const c: Candidates = await fetchCandidates(serverToken);
      dispatch({ type: 'list/success', candidates: c });
    } catch (e) {
      dispatch({ type: 'list/error', error: (e as Error).message });
    }
  }, [serverToken]);

  useEffect(() => {
    if (open) {
      dispatch({ type: 'reset' });
      setShowAudit(false);
      void loadCandidates();
    }
  }, [open, loadCandidates]);

  const handleDryRun = useCallback(async () => {
    dispatch({ type: 'dryrun/loading' });
    try {
      const report = await runDryRun(buildDryRunSelection(state), serverToken);
      dispatch({ type: 'dryrun/success', report });
      onToast(t('prune.toastDryRun'), 'info');
    } catch (e) {
      dispatch({ type: 'dryrun/error', error: (e as Error).message });
      onToast(t('prune.toastFailed', { error: (e as Error).message }), 'error');
    }
  }, [state, serverToken, onToast, t]);

  const handleConfirm = useCallback(async () => {
    if (!serverToken) {
      onAuthRequired();
      return;
    }
    // Belt-and-suspenders double-submit guard (the button is also disabled).
    if (state.deleting) return;
    dispatch({ type: 'confirm/start' });
    try {
      const body = buildConfirmRequest(state);
      const report: DeleteReport = await confirmPrune(serverToken, body);
      dispatch({ type: 'confirm/done', report });
      const s = report.summary;
      if (s.failed > 0) {
        onToast(t('prune.toastPartial', { deleted: s.deleted, failed: s.failed }), 'error');
      } else {
        onToast(t('prune.toastDeleted', { deleted: s.deleted, bytes: formatBytes(s.reclaimed_bytes) }), 'success');
      }
    } catch (e) {
      const err = e as Error & { status?: number };
      const info = classifyHttpError(err.status ?? 500);
      if (info.reauth) {
        // Reset the in-flight delete state before handing off to the auth modal
        // so the button doesn't stay in a loading state.
        dispatch({ type: 'confirm/error', error: 'auth' });
        onToast(t('prune.toastAuthFailed'), 'error');
        onAuthRequired();
      } else if (info.reDryRun) {
        onToast(t('prune.errStaleDryRun'), 'error');
        dispatch({ type: 'confirm/error', error: 'stale' });
      } else {
        onToast(t('prune.toastFailed', { error: err.message }), 'error');
        dispatch({ type: 'confirm/error', error: t('prune.errServer', { error: err.message }) });
      }
    }
  }, [state, serverToken, onAuthRequired, onToast, t]);

  const toggleAudit = useCallback(async () => {
    if (!showAudit && serverToken) {
      try {
        const a = await fetchAudit(serverToken);
        setAuditEvents(a.events);
      } catch (e) {
        // Don't open a misleading empty panel; surface the error and keep it closed.
        onToast(t('prune.toastFailed', { error: (e as Error).message }), 'error');
        return;
      }
    }
    setShowAudit(s => !s);
  }, [showAudit, serverToken, onToast, t]);

  const c = state.candidates;
  const selCount = selectedCount(state);

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-[2000] transition-all" style={{ backgroundColor: 'var(--theme-modal-overlay)', backdropFilter: 'blur(8px)' }} />
        <Dialog.Content className="fixed top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 bg-modal-bg border border-surface-5 rounded-3xl p-6 sm:p-7 w-[94%] sm:w-[92%] max-w-[860px] max-h-[88vh] overflow-y-auto shadow-2xl backdrop-blur-3xl z-[2001] focus:outline-none">
          <div className="flex items-start justify-between gap-4 mb-5">
            <div>
              <Dialog.Title className="text-lg font-bold text-text flex items-center gap-2">
                <Trash2 className="w-4 h-4 text-accent-cyan" />
                {t('prune.title')}
              </Dialog.Title>
              <Dialog.Description className="text-[12px] text-text-dim mt-1">
                {t('prune.subtitle')}
              </Dialog.Description>
            </div>
            <div className="flex items-center gap-2">
              <span className={`text-[10px] font-bold px-2 py-1 rounded-lg border ${isAdmin ? 'bg-success/10 border-success/30 text-success' : 'bg-surface-2 border-surface-5 text-text-dim'}`}>
                {isAdmin ? t('prune.adminBadge') : t('prune.guestBadge')}
              </span>
              <Dialog.Close asChild>
                <button className="text-text-dim hover:text-text p-1 rounded-lg hover:bg-surface-2 transition-colors" aria-label={t('prune.close')}>
                  <X className="w-4 h-4" />
                </button>
              </Dialog.Close>
            </div>
          </div>

          {/* STEP: LIST */}
          {state.step === 'list' && (
            <div>
              {state.listState === 'loading' && (
                <div className="text-center py-16 text-text-dim">
                  <Loader2 className="w-6 h-6 animate-spin mx-auto mb-3 text-accent-cyan" />
                  <div className="text-[13px]">{t('prune.loading')}</div>
                </div>
              )}
              {state.listState === 'error' && (
                <div className="text-center py-12">
                  <AlertTriangle className="w-8 h-8 mx-auto mb-3 text-danger" />
                  <div className="font-bold text-danger mb-1">{t('prune.errorTitle')}</div>
                  <div className="text-[12px] text-text-dim mb-4 px-6">{state.listError || t('prune.errorHint')}</div>
                  <button onClick={loadCandidates} className="px-4 py-2 rounded-xl bg-surface-2 hover:bg-surface-4 border border-surface-5 text-text text-[12px] font-bold inline-flex items-center gap-2 cursor-pointer">
                    <RefreshCw className="w-3.5 h-3.5" /> {t('prune.retry')}
                  </button>
                </div>
              )}
              {state.listState === 'empty' && (
                <div className="text-center py-16">
                  <div className="font-bold text-text mb-1">{t('prune.empty')}</div>
                  <div className="text-[12px] text-text-dim mb-5">{t('prune.emptyHint')}</div>
                  <button onClick={loadCandidates} className="px-4 py-2 rounded-xl bg-surface-2 hover:bg-surface-4 border border-surface-5 text-text text-[12px] font-bold inline-flex items-center gap-2 cursor-pointer">
                    <RefreshCw className="w-3.5 h-3.5" /> {t('prune.refresh')}
                  </button>
                </div>
              )}
              {state.listState === 'ready' && c && (
                <div>
                  <div className="flex items-center justify-between mb-4 text-[11px] font-bold tracking-wide text-text-dim">
                    <span>{t('prune.totalCandidates', { images: c.images_count, volumes: c.volumes_count })} · {t('prune.estReclaim')} <span className="text-danger font-mono">{formatBytes(c.total_size)}</span></span>
                    <button
                      onClick={() => dispatch({ type: allSelected(state) ? 'select/none' : 'select/all' })}
                      className="text-accent-cyan hover:underline cursor-pointer"
                    >
                      {allSelected(state) ? t('prune.selected', { count: selCount }) : t('prune.selectAll')}
                    </button>
                  </div>

                  {c.images.length > 0 && (
                    <div className="mb-5">
                      <h3 className="text-[11px] font-extrabold tracking-[2px] text-text-dim mb-2 flex items-center gap-2 after:content-[''] after:grow after:h-[1px] after:bg-surface-3">
                        {t('prune.danglingImages')}
                      </h3>
                      <div className="space-y-1.5">
                        {c.images.map(img => (
                          <label key={img.id} className="flex items-center gap-3 bg-surface-1 hover:bg-surface-2 border border-border-subtle rounded-xl px-3 py-2.5 cursor-pointer transition-colors">
                            <input type="checkbox" checked={!!state.selectedImages[img.id]} onChange={e => dispatch({ type: 'select/image', id: img.id, selected: e.target.checked })} className="accent-cyan-400" />
                            <span className="mono text-[11px] text-text font-mono flex-1">{shortImageId(img.id)}</span>
                            <span className={`text-[10px] px-2 py-0.5 rounded-full font-bold ${img.reason === 'unused' ? 'bg-amber-400/10 text-amber-400' : 'bg-accent-cyan/10 text-accent-cyan'}`}>
                              {img.reason === 'unused' ? t('prune.unusedImage') : t('prune.dangling')}
                            </span>
                            <span className="text-[11px] text-text-dim font-mono w-20 text-right">{formatBytes(img.size)}</span>
                          </label>
                        ))}
                      </div>
                    </div>
                  )}

                  {c.volumes.length > 0 && (
                    <div className="mb-5">
                      <h3 className="text-[11px] font-extrabold tracking-[2px] text-text-dim mb-2 flex items-center gap-2 after:content-[''] after:grow after:h-[1px] after:bg-surface-3">
                        {t('prune.unusedVolumes')}
                      </h3>
                      <div className="space-y-1.5">
                        {c.volumes.map(v => (
                          <label key={v.name} className="flex items-center gap-3 bg-surface-1 hover:bg-surface-2 border border-border-subtle rounded-xl px-3 py-2.5 cursor-pointer transition-colors">
                            <input type="checkbox" checked={!!state.selectedVolumes[v.name]} onChange={e => dispatch({ type: 'select/volume', name: v.name, selected: e.target.checked })} className="accent-cyan-400" />
                            <div className="flex-1 min-w-0">
                              <div className="text-[12px] text-text font-mono truncate">{v.name}</div>
                              <div className="text-[10px] text-text-dim font-mono truncate">{v.mountpoint}</div>
                            </div>
                            <span className="text-[10px] px-2 py-0.5 rounded-full bg-indigo-400/10 text-indigo-300 font-bold">{t('prune.unused')}</span>
                            <span className="text-[11px] text-text-dim font-mono w-20 text-right">{v.size < 0 ? '—' : formatBytes(v.size)}</span>
                          </label>
                        ))}
                      </div>
                    </div>
                  )}

                  <div className="flex justify-end gap-2 mt-5">
                    <button onClick={loadCandidates} className="px-4 py-2.5 rounded-xl bg-surface-1 hover:bg-surface-2 border border-border-subtle text-text-dim hover:text-text text-[12px] font-bold inline-flex items-center gap-2 cursor-pointer">
                      <RefreshCw className="w-3.5 h-3.5" /> {t('prune.refresh')}
                    </button>
                    <button
                      onClick={handleDryRun}
                      disabled={!canDryRun(state) || state.dryRunLoading}
                      className="btn-primary px-5 py-2.5 rounded-xl text-[12px] font-bold inline-flex items-center gap-2 disabled:opacity-40 disabled:cursor-not-allowed"
                    >
                      {state.dryRunLoading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <ShieldAlert className="w-3.5 h-3.5" />}
                      {state.dryRunLoading ? t('prune.previewing') : t('prune.previewBtn')}
                    </button>
                  </div>
                </div>
              )}
            </div>
          )}

          {/* STEP: DRY-RUN */}
          {state.step === 'dryrun' && state.dryRun && (
            <div>
              <h3 className="text-[11px] font-extrabold tracking-[2px] text-accent-cyan mb-3">{t('prune.dryrunTitle')}</h3>
              <p className="text-[12px] text-text-dim mb-4">{t('prune.dryrunIntro')}</p>
              <div className="grid grid-cols-3 gap-3 mb-5">
                <div className="bg-surface-1 border border-border-subtle rounded-xl p-3 text-center">
                  <div className="text-xl font-bold text-text">{state.dryRun.will_delete.images}</div>
                  <div className="text-[10px] text-text-dim uppercase tracking-wide">{t('prune.danglingImages')}</div>
                </div>
                <div className="bg-surface-1 border border-border-subtle rounded-xl p-3 text-center">
                  <div className="text-xl font-bold text-text">{state.dryRun.will_delete.volumes}</div>
                  <div className="text-[10px] text-text-dim uppercase tracking-wide">{t('prune.unusedVolumes')}</div>
                </div>
                <div className="bg-surface-1 border border-danger/20 rounded-xl p-3 text-center">
                  <div className="text-xl font-bold text-danger">{formatBytes(state.dryRun.will_delete.estimated_reclaim_bytes)}</div>
                  <div className="text-[10px] text-text-dim uppercase tracking-wide">{t('prune.estReclaim')}</div>
                </div>
              </div>

              <div className="max-h-48 overflow-y-auto space-y-1 mb-4">
                {state.dryRun.candidates.images.map(img => (
                  <div key={img.id} className="flex items-center justify-between bg-surface-1 border border-border-subtle rounded-lg px-3 py-2 text-[12px]">
                    <span className="font-mono text-text">{shortImageId(img.id)}</span>
                    <span className="text-text-dim font-mono">{formatBytes(img.size)}</span>
                  </div>
                ))}
                {state.dryRun.candidates.volumes.map(v => (
                  <div key={v.name} className="flex items-center justify-between bg-surface-1 border border-border-subtle rounded-lg px-3 py-2 text-[12px]">
                    <span className="font-mono text-text truncate">{v.name}</span>
                    <span className="text-text-dim font-mono">{v.size < 0 ? '—' : formatBytes(v.size)}</span>
                  </div>
                ))}
              </div>

              <div className="bg-danger/10 border border-danger/30 rounded-xl p-3 text-[12px] text-danger flex items-start gap-2 mb-3">
                <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
                <div><b>{t('prune.warningTitle')}.</b> {t('prune.warningBody')}</div>
              </div>
              <div className="text-[10px] text-text-dim font-mono mb-4">{t('prune.fingerprint')}: {state.dryRun.candidates.fingerprint}</div>

              {state.dryRunError && (
                <div className="text-[12px] text-danger mb-3 bg-danger/10 border border-danger/30 rounded-lg p-2">
                  {state.dryRunError === 'stale' ? t('prune.errStaleDryRun') : state.dryRunError}
                </div>
              )}

              <div className="flex justify-between items-center gap-2">
                <button onClick={() => dispatch({ type: 'navigate', step: 'list' })} className="px-4 py-2.5 rounded-xl bg-surface-1 hover:bg-surface-2 border border-border-subtle text-text-dim hover:text-text text-[12px] font-bold cursor-pointer">
                  ← {t('prune.backToList')}
                </button>
                {!isAdmin ? (
                  <button onClick={onAuthRequired} className="px-5 py-2.5 rounded-xl bg-accent-cyan/10 hover:bg-accent-cyan/20 border border-accent-cyan/30 text-accent-cyan text-[12px] font-bold inline-flex items-center gap-2 cursor-pointer">
                    <ShieldAlert className="w-3.5 h-3.5" /> {t('prune.enterToken')}
                  </button>
                ) : (
                  <button
                    onClick={() => dispatch({ type: 'navigate', step: 'confirm' })}
                    disabled={state.dryRun.will_delete.images + state.dryRun.will_delete.volumes === 0}
                    className="btn-primary px-5 py-2.5 rounded-xl text-[12px] font-bold inline-flex items-center gap-2 disabled:opacity-40 disabled:cursor-not-allowed"
                  >
                    {t('prune.continueConfirm')} →
                  </button>
                )}
              </div>
            </div>
          )}

          {/* STEP: CONFIRM */}
          {state.step === 'confirm' && (
            <div>
              <h3 className="text-[11px] font-extrabold tracking-[2px] text-danger mb-3">{t('prune.confirmTitle')}</h3>
              <p className="text-[12px] text-text-dim mb-4">{t('prune.confirmIntro')}</p>
              <div className="bg-danger/10 border border-danger/30 rounded-xl p-4 mb-4">
                <div className="flex items-center gap-2 text-danger font-bold text-[13px] mb-1">
                  <AlertTriangle className="w-4 h-4" />
                  {t('prune.warningTitle')}
                </div>
                <div className="text-[12px] text-text-dim">{t('prune.warningBody')}</div>
                {state.dryRun && (
                  <div className="text-[12px] text-text mt-2">
                    {t('prune.willDelete')}: <b>{state.dryRun.will_delete.images + state.dryRun.will_delete.volumes}</b> · {t('prune.estReclaim')} <b className="text-danger">{formatBytes(state.dryRun.will_delete.estimated_reclaim_bytes)}</b>
                  </div>
                )}
              </div>
              <label className="flex items-start gap-3 mb-5 cursor-pointer">
                <input type="checkbox" checked={state.confirmedChecked} onChange={e => dispatch({ type: 'confirm/toggle', checked: e.target.checked })} className="mt-0.5 accent-red-500 w-4 h-4" />
                <span className="text-[12px] text-text-dim leading-relaxed">{t('prune.confirmCheck')}</span>
              </label>
              <div className="flex justify-between gap-2">
                <button onClick={() => dispatch({ type: 'navigate', step: 'dryrun' })} className="px-4 py-2.5 rounded-xl bg-surface-1 hover:bg-surface-2 border border-border-subtle text-text-dim hover:text-text text-[12px] font-bold cursor-pointer">
                  ← {t('prune.backToList')}
                </button>
                <button
                  onClick={handleConfirm}
                  disabled={!canConfirm(state)}
                  className="px-5 py-2.5 rounded-xl bg-danger hover:brightness-110 text-white text-[12px] font-bold inline-flex items-center gap-2 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
                >
                  {state.deleting ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Trash2 className="w-3.5 h-3.5" />}
                  {state.deleting ? t('prune.deleting') : t('prune.deleteBtn')}
                </button>
              </div>
            </div>
          )}

          {/* STEP: RESULT */}
          {state.step === 'result' && state.result && (
            <div>
              {(() => {
                const s = summarizeItems(state.result.items);
                const hasFail = s.failed > 0;
                return (
                  <>
                    <h3 className={`text-[14px] font-extrabold mb-4 flex items-center gap-2 ${hasFail ? 'text-danger' : 'text-success'}`}>
                      {hasFail ? <XCircle className="w-5 h-5" /> : <CheckCircle2 className="w-5 h-5" />}
                      {hasFail ? t('prune.resultPartial') : t('prune.resultTitle')}
                    </h3>
                    <div className="grid grid-cols-4 gap-3 mb-5">
                      <div className="bg-surface-1 border border-border-subtle rounded-xl p-3 text-center">
                        <div className="text-lg font-bold text-success">{s.deleted}</div>
                        <div className="text-[10px] text-text-dim uppercase">{t('prune.deleted')}</div>
                      </div>
                      <div className="bg-surface-1 border border-border-subtle rounded-xl p-3 text-center">
                        <div className="text-lg font-bold text-text-dim">{s.skipped}</div>
                        <div className="text-[10px] text-text-dim uppercase">{t('prune.skipped')}</div>
                      </div>
                      <div className="bg-surface-1 border border-border-subtle rounded-xl p-3 text-center">
                        <div className="text-lg font-bold text-danger">{s.failed}</div>
                        <div className="text-[10px] text-text-dim uppercase">{t('prune.failed')}</div>
                      </div>
                      <div className="bg-surface-1 border border-border-subtle rounded-xl p-3 text-center">
                        <div className="text-lg font-bold text-accent-cyan">{formatBytes(s.reclaimed)}</div>
                        <div className="text-[10px] text-text-dim uppercase">{t('prune.reclaimed')}</div>
                      </div>
                    </div>
                    <div className="max-h-56 overflow-y-auto space-y-1 mb-4">
                      {state.result.items.map((it, i) => (
                        <div key={i} className="flex items-center justify-between bg-surface-1 border border-border-subtle rounded-lg px-3 py-2 text-[12px]">
                          <span className="font-mono text-text truncate flex items-center gap-2">
                            {it.status === 'deleted' && <CheckCircle2 className="w-3.5 h-3.5 text-success" />}
                            {it.status === 'failed' && <XCircle className="w-3.5 h-3.5 text-danger" />}
                            {it.status === 'skipped' && <SkipForward className="w-3.5 h-3.5 text-text-dim" />}
                            {it.type === 'image' ? shortImageId(it.id || '') : it.name}
                          </span>
                          <span className={`text-[10px] font-bold px-2 py-0.5 rounded-full ${it.status === 'deleted' ? 'bg-success/10 text-success' : it.status === 'failed' ? 'bg-danger/10 text-danger' : 'bg-surface-3 text-text-dim'}`}>
                            {t(`prune.${it.status}`)}
                          </span>
                        </div>
                      ))}
                    </div>

                    {isAdmin && (
                      <div className="mb-4">
                        <button onClick={toggleAudit} className="text-[11px] font-bold text-accent-cyan hover:underline cursor-pointer">
                          {showAudit ? t('prune.hideAudit') : t('prune.viewAudit')}
                        </button>
                        {showAudit && (
                          <div className="mt-2 bg-surface-1 border border-border-subtle rounded-xl p-3 max-h-40 overflow-y-auto">
                            {auditEvents.length === 0 ? (
                              <div className="text-[11px] text-text-dim">{t('prune.auditEmpty')}</div>
                            ) : (
                              auditEvents.slice().reverse().slice(0, 10).map(ev => (
                                <div key={ev.id} className="text-[11px] text-text-dim py-1 border-b border-border-subtle last:border-0">
                                  <span className="font-mono text-text">{ev.time}</span> · <b className="text-text">{ev.actor}</b> · {ev.status} · {ev.images_deleted + ev.volumes_deleted} {t('prune.deleted').toLowerCase()} · {formatBytes(ev.reclaimed_bytes)}
                                  {ev.detail && <span className="block text-[10px] text-danger">{ev.detail}</span>}
                                </div>
                              ))
                            )}
                          </div>
                        )}
                      </div>
                    )}

                    <div className="flex justify-end gap-2">
                      <button onClick={() => { void loadCandidates(); dispatch({ type: 'navigate', step: 'list' }); }} className="btn-primary px-5 py-2.5 rounded-xl text-[12px] font-bold cursor-pointer">
                        {t('prune.done')}
                      </button>
                    </div>
                  </>
                );
              })()}
            </div>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
