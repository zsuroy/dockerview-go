import { useCallback, useEffect, useRef, useState } from 'react';
import {
  FolderOpen, FileText, Upload, Download, Archive, Loader2, RefreshCw,
  ArrowUp, ShieldAlert, Eye,
} from 'lucide-react';
import { useTranslation } from '../i18n';
import type { Container } from '../types';
import { formatBytes } from '../utils';
import {
  fetchFilesList, fetchFilesConfig, previewIn, previewOut, previewArchive, uploadFile, fileDownloadUrl,
} from '../files/api';
import type {
  FilesEntry, ListReport, InPreviewReport, ArchivePreviewReport, FilesBusy,
} from '../files/types';
import {
  basename, canUpload, errorMessage, isAuthError, isMkdirGate, isOverwriteGate, joinPath,
  looksTraversal, quotaPct, shortSha,
} from '../files/logic';

interface Props {
  token: string;
  containers: Container[];
  initialContainerId?: string;
  onAuthRequired: () => void;
  onToast: (message: string, type?: 'info' | 'success' | 'error') => void;
}

export function FilesPanel({ token, containers, initialContainerId, onAuthRequired, onToast }: Props) {
  const { t } = useTranslation();
  const [cid, setCid] = useState<string>(initialContainerId || containers[0]?.id || '');
  const [dir, setDir] = useState<string>('.');
  const [list, setList] = useState<ListReport | null>(null);
  const [busy, setBusy] = useState<FilesBusy>('idle');

  // upload form state
  const [file, setFile] = useState<File | null>(null);
  const [targetPath, setTargetPath] = useState('');
  const [inPrev, setInPrev] = useState<InPreviewReport | null>(null);
  const [overwrite, setOverwrite] = useState(false);
  const [mkdir, setMkdir] = useState(false);
  const [progress, setProgress] = useState(0);
  const [serverLimit, setServerLimit] = useState<number>(0);

  const fileInputRef = useRef<HTMLInputElement>(null);
  // Target path autofill: default to "current browse dir + picked file
  // name" so the common case needs no typing. A manually edited path is
  // left untouched.
  const [pathEdited, setPathEdited] = useState(false);
  const applyAutoPath = useCallback((dirPath: string, name: string) => {
    setTargetPath(joinPath(dirPath === '.' ? '' : dirPath, name));
  }, []);

  // Show the server-side quota (never trust a hardcoded 8MiB).
  useEffect(() => {
    if (!token) return;
    fetchFilesConfig(token).then((c) => setServerLimit(c.max_file_bytes)).catch(() => undefined);
  }, [token]);

  useEffect(() => {
    if (!cid && containers.length > 0) setCid(containers[0].id);
  }, [containers, cid]);

  const guard = useCallback((e: unknown): boolean => {
    if (isAuthError(e)) {
      onAuthRequired();
      return true;
    }
    return false;
  }, [onAuthRequired]);

  // refresh() carries a sequence number so a slow response can never
  // overwrite the listing of a directory navigated to later.
  const listSeq = useRef(0);
  const refresh = useCallback(async (path = dir) => {
    if (!token) {
      onAuthRequired();
      return;
    }
    const seq = ++listSeq.current;
    setBusy('listing');
    setList(null); // drop the stale listing immediately
    setInPrev(null); // preview belongs to the old target path
    try {
      const report = await fetchFilesList(token, cid, path);
      if (seq !== listSeq.current) return; // superseded by newer navigation
      setList(report);
      setDir(path);
    } catch (e) {
      if (seq !== listSeq.current) return;
      if (!guard(e)) onToast(errorMessage(e), 'error');
    } finally {
      if (seq === listSeq.current) setBusy('idle');
    }
  }, [token, cid, dir, guard, onAuthRequired, onToast]);

  useEffect(() => {
    if (cid) refresh('.');
    // refresh() itself prompts for auth when the token is missing.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cid, token]);

  const handlePreviewIn = useCallback(async () => {
    if (!file) return;
    setBusy('previewIn');
    setInPrev(null);
    try {
      const report = await previewIn(token, cid, targetPath.trim());
      setInPrev(report);
      if (file.size > report.max_file_bytes) {
        onToast(t('files.tooLargeHint'), 'error');
      }
    } catch (e) {
      if (!guard(e)) onToast(errorMessage(e), 'error');
    } finally {
      setBusy('idle');
    }
  }, [token, cid, file, targetPath, guard, onToast, t]);

  const handleUpload = useCallback(async () => {
    if (!file) return;
    const decision = canUpload({ hasFile: true, path: targetPath, busy: busy !== 'idle' });
    if (!decision.ok) {
      onToast(t(`files.uploadBlocked.${decision.reason}` as never) || t('files.uploadBlocked.noPath'), 'error');
      return;
    }
    setBusy('uploading');
    setProgress(0);
    try {
      const res = await uploadFile(token, cid, targetPath.trim(), file, overwrite, mkdir, setProgress);
      onToast(t('files.uploaded', { name: basename(res.path), bytes: formatBytes(res.bytes), sha: shortSha(res.sha256) }), 'success');
      setFile(null);
      setTargetPath('');
      setPathEdited(false);
      setInPrev(null);
      setOverwrite(false);
      setMkdir(false);
      if (fileInputRef.current) fileInputRef.current.value = '';
      refresh(dir);
    } catch (e) {
      if (isOverwriteGate(e)) {
        onToast(t('files.overwriteGateHint'), 'error');
        setInPrev((p) => p ? { ...p, overwrite_required: true, exists: true } : p);
      } else if (isMkdirGate(e)) {
        onToast(t('files.mkdirGateHint'), 'error');
        setInPrev((p) => (p ? { ...p, mkdir_required: true } : p));
      } else if (!guard(e)) {
        onToast(errorMessage(e), 'error');
      }
    } finally {
      setBusy('idle');
    }
  }, [token, cid, file, targetPath, overwrite, mkdir, busy, dir, guard, refresh, onToast, t]);

  const handleArchive = useCallback(async (name: string) => {
    const sub = joinPath(dir === '.' ? '' : dir, name);
    setBusy('archiving');
    try {
      const prev: ArchivePreviewReport = await previewArchive(token, cid, sub);
      onToast(t('files.archivePlan', { entries: prev.entries, bytes: formatBytes(prev.bytes) }), 'info');
      window.location.href = fileDownloadUrl(token, 'archive', cid, sub);
    } catch (e) {
      if (!guard(e)) onToast(errorMessage(e), 'error');
    } finally {
      setBusy('idle');
    }
  }, [token, cid, dir, guard, onToast, t]);

  const handleDownload = useCallback(async (name: string) => {
    const rel = joinPath(dir === '.' ? '' : dir, name);
    setBusy('downloading');
    try {
      const prev = await previewOut(token, cid, rel);
      onToast(t('files.downloadPlan', { name: prev.name, bytes: formatBytes(prev.size), sha: shortSha(prev.sha256) }), 'info');
      window.location.href = fileDownloadUrl(token, 'out', cid, rel);
    } catch (e) {
      if (!guard(e)) onToast(errorMessage(e), 'error');
    } finally {
      setBusy('idle');
    }
  }, [token, cid, dir, guard, onToast, t]);

  const goInto = (name: string) => {
    const next = joinPath(dir === '.' ? '' : dir, name);
    if (file && !pathEdited) applyAutoPath(next, file.name);
    refresh(next);
  };
  const goUp = () => {
    if (dir === '.' || dir === '') return;
    const parts = dir.split('/').filter(Boolean);
    parts.pop();
    const next = parts.length ? parts.join('/') : '.';
    if (file && !pathEdited) applyAutoPath(next, file.name);
    refresh(next);
  };


  return (
    <div className="rounded-2xl border border-border-light bg-surface-1 p-5" data-testid="files-panel">
      <div className="flex items-center gap-2 mb-1">
        <FolderOpen className="w-5 h-5 text-accent-cyan" />
        <h2 className="text-lg font-extrabold">{t('files.title')}</h2>
        {busy !== 'idle' && <Loader2 className="w-4 h-4 animate-spin text-text-dim" />}
      </div>
      <p className="text-[12px] text-text-dim mb-4">{t('files.subtitle')}</p>

      <div className="flex flex-wrap items-center gap-3 mb-4">
        <select
          aria-label={t('files.containerLabel')}
          className="bg-surface-2 border border-border-light rounded-xl px-3 py-2 text-[13px]"
          value={cid}
          onChange={(e) => {
            setCid(e.target.value);
            setDir('.');
            setTargetPath('');
            setInPrev(null);
            setFile(null);
            setOverwrite(false);
            setMkdir(false);
            if (fileInputRef.current) fileInputRef.current.value = '';
          }}
        >
          {containers.length === 0 && <option value="">{t('files.noContainers')}</option>}
          {containers.map((c) => (
            <option key={c.id} value={c.id}>{c.name} ({c.id.slice(0, 12)})</option>
          ))}
        </select>
        <button
          className="flex items-center gap-1.5 px-3 py-2 rounded-xl text-[12px] font-bold border border-border-light hover:text-accent-cyan"
          onClick={() => refresh(dir)}
          disabled={!cid || busy !== 'idle'}
          data-testid="files-refresh"
        >
          <RefreshCw className="w-3.5 h-3.5" /> {t('files.refresh')}
        </button>
        <span className="text-[12px] text-text-dim font-mono">
          {t('files.jailRoot')}: {list?.path || '—'}
        </span>
        {serverLimit > 0 && (
          <span className="text-[12px] text-text-dim font-mono">
            {t('files.limit')}: {formatBytes(serverLimit)}
          </span>
        )}
      </div>

      {/* Upload */}
      <div className="rounded-xl border border-border-light bg-surface-2/40 p-4 mb-4">
        <div className="flex items-center gap-2 mb-2">
          <Upload className="w-4 h-4 text-accent-cyan" />
          <span className="text-[13px] font-bold">{t('files.uploadTitle')}</span>
          <ShieldAlert className="w-3.5 h-3.5 text-amber-400" />
          <span className="text-[11px] text-text-dim">{t('files.uploadHint')}</span>
        </div>
          <input
            ref={fileInputRef}
            type="file"
            className="text-[12px] file:mr-3 file:rounded-lg file:border-0 file:bg-accent-cyan/15 file:text-accent-cyan file:px-3 file:py-1.5"
            data-testid="files-input"
            onChange={(e) => {
              const picked = e.target.files?.[0] || null;
              setFile(picked);
              if (picked) applyAutoPath(dir, picked.name);
            }}
          />
          <input
            type="text"
            placeholder={t('files.targetPlaceholder')}
            className="bg-surface-1 border border-border-light rounded-xl px-3 py-2 text-[12px] font-mono min-w-[220px] flex-1"
            value={targetPath}
            onChange={(e) => { setPathEdited(true); setTargetPath(e.target.value); setInPrev(null); }}
          />
          <button
            className="flex items-center gap-1.5 px-3 py-2 rounded-xl text-[12px] font-bold border border-border-light hover:text-accent-cyan disabled:opacity-40"
            disabled={!file || !targetPath.trim() || busy !== 'idle'}
            onClick={handlePreviewIn}
          >
            <Eye className="w-3.5 h-3.5" /> {t('files.preview')}
          </button>
          <button
            className="flex items-center gap-1.5 px-3 py-2 rounded-xl text-[12px] font-bold bg-accent-cyan/15 text-accent-cyan disabled:opacity-40"
            disabled={!file || busy !== 'idle'}
            onClick={handleUpload}
            data-testid="files-upload"
          >
            <Upload className="w-3.5 h-3.5" /> {t('files.confirmUpload')}
          </button>
        {inPrev && (
          <div className="mt-3 text-[12px] rounded-lg border border-accent-cyan/30 bg-accent-cyan/5 p-3 font-mono">
            <div>{t('files.previewPath')}: {inPrev.path}</div>
            <div>{t('files.previewExists')}: {inPrev.exists ? t('files.yes') : t('files.no')} · {t('files.previewMax')}: {formatBytes(inPrev.max_file_bytes)}</div>
            {inPrev.mkdir_required && (inPrev.missing_dirs?.length ?? 0) > 0 && (
              <div className="mt-1 text-amber-400">
                {t('files.mkdirPlan', { dirs: inPrev.missing_dirs!.join(' · ') })}
              </div>
            )}
            {inPrev.mkdir_required && (
              <label className="flex items-center gap-2 mt-1 text-amber-400">
                <input type="checkbox" checked={mkdir} onChange={(e) => setMkdir(e.target.checked)} data-testid="files-mkdir" />
                {t('files.mkdirConfirm')}
              </label>
            )}
            {inPrev.overwrite_required && (
              <label className="flex items-center gap-2 mt-1 text-red-400">
                <input type="checkbox" checked={overwrite} onChange={(e) => setOverwrite(e.target.checked)} />
                {t('files.overwriteConfirm')}
              </label>
            )}
          </div>
        )}
        {busy === 'uploading' && (
          <div className="mt-3 h-2 rounded bg-surface-1 overflow-hidden">
            <div className="h-full bg-accent-cyan transition-all" style={{ width: `${progress}%` }} />
          </div>
        )}
        {!busy.startsWith('upload') && file && serverLimit > 0 && (
          <div className="mt-2 h-1.5 rounded bg-surface-1 overflow-hidden" title={t('files.limit')}>
            <div
              className={`h-full ${file.size > serverLimit ? 'bg-red-500' : 'bg-accent-cyan'}`}
              style={{ width: `${quotaPct(file.size, serverLimit)}%` }}
            />
          </div>
        )}
      </div>

      {/* Listing */}
      <div className="rounded-xl border border-border-light overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2 bg-surface-2/50">
          <span className="text-[12px] font-bold text-text-dim">{t('files.listing')}</span>
          <button className="flex items-center gap-1 text-[12px] text-text-dim hover:text-text" onClick={goUp}>
            <ArrowUp className="w-3.5 h-3.5" /> {t('files.up')}
          </button>
        </div>
        <table className="w-full text-[13px]">
          <thead>
            <tr className="text-left text-text-dim text-[11px] uppercase">
              <th className="px-4 py-2">{t('files.colName')}</th>
              <th className="px-4 py-2">{t('files.colSize')}</th>
              <th className="px-4 py-2 text-right">{t('files.colActions')}</th>
            </tr>
          </thead>
          <tbody>
            {list?.entries.map((e: FilesEntry) => (
              <tr key={e.name} className="border-t border-border-light/60">
                <td className="px-4 py-2">
                  {e.is_dir ? (
                    <button className="flex items-center gap-2 hover:text-accent-cyan" onClick={() => goInto(e.name)}>
                      <FolderOpen className="w-4 h-4 text-amber-400" /> {e.name}
                    </button>
                  ) : (
                    <span className="flex items-center gap-2 font-mono">
                      <FileText className="w-4 h-4 text-text-dim" /> {e.name}
                      {e.is_symlink && <span className="text-[10px] text-amber-400">link</span>}
                    </span>
                  )}
                </td>
                <td className="px-4 py-2 text-text-dim">{e.is_dir ? '—' : formatBytes(e.size)}</td>
                <td className="px-4 py-2 text-right">
                  {e.is_dir ? (
                    <button
                      className="inline-flex items-center gap-1 px-2 py-1 rounded-lg text-[11px] border border-border-light hover:text-accent-cyan disabled:opacity-40"
                      onClick={() => handleArchive(e.name)}
                      disabled={busy !== 'idle'}
                      title={t('files.archiveTitle')}
                    >
                      <Archive className="w-3.5 h-3.5" /> tar
                    </button>
                  ) : (
                    <button
                      className="inline-flex items-center gap-1 px-2 py-1 rounded-lg text-[11px] border border-border-light hover:text-accent-cyan disabled:opacity-40"
                      onClick={() => handleDownload(e.name)}
                      disabled={busy !== 'idle'}
                      title={t('files.downloadTitle')}
                    >
                      <Download className="w-3.5 h-3.5" /> {t('files.download')}
                    </button>
                  )}
                </td>
              </tr>
            ))}
            {busy === 'listing' && !list && (
              <tr><td colSpan={3} className="px-4 py-6 text-center text-text-dim text-[12px]">{t('files.loading')}</td></tr>
            )}
            {list && list.entries.length === 0 && (
              <tr><td colSpan={3} className="px-4 py-6 text-center text-text-dim text-[12px]">{t('files.empty')}</td></tr>
            )}
          </tbody>
        </table>
      </div>
      {looksTraversal(targetPath) && (
        <p className="mt-2 text-[12px] text-red-400">{t('files.traversalHint')}</p>
      )}
    </div>
  );
}
