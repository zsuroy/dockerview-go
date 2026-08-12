// Pure, framework-agnostic backup state machine and helpers.
// Designed to be tested without React/DOM (vitest unit tests).

import type {
  BackupState, CreateReport, ListReport, PreviewReport,
} from './types';

export const initialBackupState: BackupState = {
  previewPhase: 'idle',
  preview: null,
  previewError: '',
  note: '',
  includeImages: false, // MUST default to false (BACKUP_DESIGN §1)
  includeStopped: false, // default: only running containers
  creating: false,
  createError: '',
  lastCreated: null,
  listPhase: 'idle',
  list: null,
  listError: '',
  deletingName: '',
};

export type BackupAction =
  | { type: 'preview/loading' }
  | { type: 'preview/success'; report: PreviewReport }
  | { type: 'preview/error'; error: string }
  | { type: 'form/note'; note: string }
  | { type: 'form/includeImages'; checked: boolean }
  | { type: 'form/includeStopped'; checked: boolean }
  | { type: 'create/start' }
  | { type: 'create/success'; report: CreateReport }
  | { type: 'create/error'; error: string }
  | { type: 'list/loading' }
  | { type: 'list/success'; report: ListReport }
  | { type: 'list/error'; error: string }
  | { type: 'delete/start'; name: string }
  | { type: 'delete/stop' }
  | { type: 'reset' };

export function backupReducer(state: BackupState, action: BackupAction): BackupState {
  switch (action.type) {
    case 'preview/loading':
      return { ...state, previewPhase: 'loading', previewError: '' };
    case 'preview/success':
      return { ...state, previewPhase: 'ready', preview: action.report, previewError: '' };
    case 'preview/error':
      return { ...state, previewPhase: 'error', previewError: action.error };

    case 'form/note':
      return { ...state, note: action.note, createError: '' };
    case 'form/includeImages':
      return { ...state, includeImages: action.checked, preview: null, previewPhase: 'idle', previewError: '' };
    case 'form/includeStopped':
      return { ...state, includeStopped: action.checked, preview: null, previewPhase: 'idle', previewError: '' };

    case 'create/start':
      return { ...state, creating: true, createError: '' };
    case 'create/success':
      return { ...state, creating: false, lastCreated: action.report, createError: '' };
    case 'create/error':
      return { ...state, creating: false, createError: action.error };

    case 'list/loading':
      return { ...state, listPhase: 'loading', listError: '' };
    case 'list/success':
      return {
        ...state,
        listPhase: 'ready',
        list: action.report,
        listError: '',
      };
    case 'list/error':
      return { ...state, listPhase: 'error', listError: action.error };

    case 'delete/start':
      return { ...state, deletingName: action.name };
    case 'delete/stop':
      return { ...state, deletingName: '' };

    case 'reset':
      return { ...initialBackupState };
    default:
      return state;
  }
}

// --- selectors / helpers ---

// canCreate guards against double-submit while busy (UI disables the button).
export function canCreate(state: BackupState): boolean {
  return !state.creating;
}

// includeImagesRisk returns true when the optional image export is enabled —
// the UI must show a size/disk warning in that case.
export function includeImagesRisk(state: BackupState): boolean {
  return state.includeImages;
}

// noteTooLong enforces the server-side 500-char cap on the client.
export function noteTooLong(note: string): boolean {
  return Array.from(note).length > 500;
}

// classifyBackupError maps an HTTP status to a stable UI error key plus
// whether the caller should force re-authentication.
export function classifyBackupError(status: number): { key: string; reauth: boolean } {
  if (status === 401) return { key: 'authFailed', reauth: true };
  if (status === 409) return { key: 'busy', reauth: false };
  if (status === 502) return { key: 'exportFailed', reauth: false };
  if (status === 507) return { key: 'diskFull', reauth: false };
  if (status === 503) return { key: 'unavailable', reauth: false };
  return { key: 'serverError', reauth: false };
}

// formatBytes renders a byte count using 1024 units.
export function formatBytes(b: number): string {
  if (b === null || b === undefined || isNaN(b)) return '0 B';
  if (b <= 0) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let val = b;
  while (val >= 1024 && i < u.length - 1) {
    val /= 1024;
    i++;
  }
  return `${val.toFixed(1)} ${u[i]}`;
}

// formatTimestamp renders an RFC3339 UTC string in the local timezone.
export function formatTimestamp(iso: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

// totalSize sums archive sizes for the header tile.
export function totalSize(list: ListReport | null): number {
  if (!list) return 0;
  return list.backups.reduce((s, b) => s + (b.size_bytes || 0), 0);
}

// archivesWithImages counts archives that include image exports.
export function archivesWithImages(list: ListReport | null): number {
  if (!list) return 0;
  return list.backups.filter(b => b.include_images).length;
}

// sortBackups returns newest-first by created_at (stable on name as tiebreak).
export function sortBackups<T extends { created_at: string; name: string }>(items: T[]): T[] {
  return [...items].sort((a, b) => {
    if (a.created_at !== b.created_at) return a.created_at < b.created_at ? 1 : -1;
    return a.name < b.name ? 1 : -1;
  });
}
