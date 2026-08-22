// Pure helpers for the files panel (unit-tested, no React/DOM).

export type FilesError = Error & { status?: number };

export function isAuthError(e: unknown): boolean {
  return (e as FilesError)?.status === 401;
}

export function isOverwriteGate(e: unknown): boolean {
  return (e as FilesError)?.status === 409;
}

// isMkdirGate detects the confirm-time "destination directory missing"
// rejection; the server distinguishes it by message code.
export function isMkdirGate(e: unknown): boolean {
  const err = e as FilesError;
  return err?.status === 409 && /directory does not exist/i.test(err?.message || '');
}

export function isTooLarge(e: unknown): boolean {
  return (e as FilesError)?.status === 413;
}

export function errorMessage(e: unknown): string {
  const err = e as FilesError;
  const base = err?.message || String(e);
  if (isTooLarge(err)) return `${base} — file exceeds the configured size limit`;
  return base;
}

// quotaPct returns upload/archive quota percentage, clamped at 100.
export function quotaPct(bytes: number, max: number): number {
  if (max <= 0) return 0;
  return Math.min(100, Math.round((bytes / max) * 100));
}

export function basename(p: string): string {
  const clean = p.replace(/\\/g, '/').replace(/\/+$/, '');
  const i = clean.lastIndexOf('/');
  return i >= 0 ? clean.slice(i + 1) : clean;
}

// joinPath joins a jail-relative directory and a name; the result is
// display-only (the server re-resolves everything safely).
export function joinPath(dir: string, name: string): string {
  if (!dir || dir === '.') return name;
  return `${dir.replace(/\/+$/, '')}/${name}`;
}

// looksTraversal gives the UI a cheap pre-check hint; the server remains the
// real gate. It must never block LEGAL names.
export function looksTraversal(p: string): boolean {
  if (p.includes('\\') || p.includes('\0')) return true;
  return p.split('/').some((seg) => seg === '..');
}

// uploadState decides the confirm button label/state.
export type UploadDecision =
  | { ok: true }
  | { ok: false; reason: 'no-file' | 'no-path' | 'traversal' | 'idle' };

export function canUpload(opts: { hasFile: boolean; path: string; busy: boolean }): UploadDecision {
  if (opts.busy) return { ok: false, reason: 'idle' };
  if (!opts.hasFile) return { ok: false, reason: 'no-file' };
  if (!opts.path.trim()) return { ok: false, reason: 'no-path' };
  if (looksTraversal(opts.path)) return { ok: false, reason: 'traversal' };
  return { ok: true };
}

// shortSha trims a sha256 for display (full value is in the API response).
export function shortSha(sha: string, n = 12): string {
  return sha.length > n ? `${sha.slice(0, n)}…` : sha;
}
