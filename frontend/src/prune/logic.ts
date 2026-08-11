// Pure, framework-agnostic prune state machine and helpers.
// Tested directly with node:test (no React/DOM required).

import type {
  Candidates, DeleteReport, DryRunReport, PruneState,
} from './types';

export const initialPruneState: PruneState = {
  step: 'list',
  listState: 'idle',
  listError: '',
  candidates: null,
  selectedImages: {},
  selectedVolumes: {},
  dryRun: null,
  dryRunLoading: false,
  dryRunError: '',
  confirmedChecked: false,
  deleting: false,
  result: null,
  audit: [],
};

export type PruneAction =
  | { type: 'list/loading' }
  | { type: 'list/success'; candidates: Candidates }
  | { type: 'list/error'; error: string }
  | { type: 'select/image'; id: string; selected: boolean }
  | { type: 'select/volume'; name: string; selected: boolean }
  | { type: 'select/allImages'; selected: boolean }
  | { type: 'select/allVolumes'; selected: boolean }
  | { type: 'select/all' }
  | { type: 'select/none' }
  | { type: 'dryrun/loading' }
  | { type: 'dryrun/success'; report: DryRunReport }
  | { type: 'dryrun/error'; error: string }
  | { type: 'confirm/toggle'; checked: boolean }
  | { type: 'confirm/start' }
  | { type: 'confirm/done'; report: DeleteReport }
  | { type: 'confirm/error'; error: string }
  | { type: 'navigate'; step: PruneState['step'] }
  | { type: 'reset' };

export function pruneReducer(state: PruneState, action: PruneAction): PruneState {
  switch (action.type) {
    case 'list/loading':
      return { ...state, listState: 'loading', listError: '' };
    case 'list/success': {
      const selImg: Record<string, boolean> = {};
      const selVol: Record<string, boolean> = {};
      action.candidates.images.forEach(i => { selImg[i.id] = true; });
      action.candidates.volumes.forEach(v => { selVol[v.name] = true; });
      return {
        ...state,
        listState: (action.candidates.images.length + action.candidates.volumes.length === 0) ? 'empty' : 'ready',
        candidates: action.candidates,
        selectedImages: selImg,
        selectedVolumes: selVol,
        listError: '',
      };
    }
    case 'list/error':
      return { ...state, listState: 'error', listError: action.error };

    case 'select/image':
      return { ...state, selectedImages: { ...state.selectedImages, [action.id]: action.selected } };
    case 'select/volume':
      return { ...state, selectedVolumes: { ...state.selectedVolumes, [action.name]: action.selected } };
    case 'select/allImages': {
      const sel: Record<string, boolean> = {};
      state.candidates?.images.forEach(i => { sel[i.id] = action.selected; });
      return { ...state, selectedImages: sel };
    }
    case 'select/allVolumes': {
      const sel: Record<string, boolean> = {};
      state.candidates?.volumes.forEach(v => { sel[v.name] = action.selected; });
      return { ...state, selectedVolumes: sel };
    }
    case 'select/all': {
      const si: Record<string, boolean> = {};
      const sv: Record<string, boolean> = {};
      state.candidates?.images.forEach(i => { si[i.id] = true; });
      state.candidates?.volumes.forEach(v => { sv[v.name] = true; });
      return { ...state, selectedImages: si, selectedVolumes: sv };
    }
    case 'select/none':
      return { ...state, selectedImages: {}, selectedVolumes: {} };

    case 'dryrun/loading':
      return { ...state, dryRunLoading: true, dryRunError: '' };
    case 'dryrun/success': {
      const si: Record<string, boolean> = {};
      const sv: Record<string, boolean> = {};
      action.report.candidates.images.forEach(i => {
        const prev = state.selectedImages[i.id];
        if (prev !== undefined) si[i.id] = prev;
      });
      action.report.candidates.volumes.forEach(v => {
        const prev = state.selectedVolumes[v.name];
        if (prev !== undefined) sv[v.name] = prev;
      });
      return {
        ...state,
        dryRunLoading: false,
        dryRun: action.report,
        selectedImages: si,
        selectedVolumes: sv,
        step: 'dryrun',
        dryRunError: '',
        confirmedChecked: false,
      };
    }
    case 'dryrun/error':
      return { ...state, dryRunLoading: false, dryRunError: action.error };

    case 'confirm/toggle':
      return { ...state, confirmedChecked: action.checked };
    case 'confirm/start':
      return { ...state, deleting: true };
    case 'confirm/done': {
      let updatedCandidates = state.candidates;
      if (updatedCandidates && action.report.items) {
        const deletedImageIds = new Set<string>();
        const deletedVolumeNames = new Set<string>();
        for (const item of action.report.items) {
          if (item.status === 'deleted') {
            if (item.type === 'image' && item.id) deletedImageIds.add(item.id);
            if (item.type === 'volume' && item.name) deletedVolumeNames.add(item.name);
          }
        }
        if (deletedImageIds.size > 0 || deletedVolumeNames.size > 0) {
          const newImages = updatedCandidates.images.filter(i => !deletedImageIds.has(i.id));
          const newVolumes = updatedCandidates.volumes.filter(v => !deletedVolumeNames.has(v.name));
          const newImgSize = newImages.reduce((s, i) => s + (i.size > 0 ? i.size : 0), 0);
          const newVolSize = newVolumes.reduce((s, v) => s + (v.size > 0 ? v.size : 0), 0);
          updatedCandidates = {
            ...updatedCandidates,
            images: newImages,
            volumes: newVolumes,
            images_count: newImages.length,
            volumes_count: newVolumes.length,
            images_size: newImgSize,
            volumes_size: newVolSize,
            total_size: newImgSize + newVolSize,
          };
        }
      }
      return { ...state, deleting: false, result: action.report, candidates: updatedCandidates, step: 'result' };
    }
    case 'confirm/error':
      // Return to the dry-run step and reset the acknowledgement latch so the
      // user must re-confirm after an error (e.g. stale fingerprint).
      return { ...state, deleting: false, confirmedChecked: false, dryRunError: action.error, step: 'dryrun' };

    case 'navigate':
      return { ...state, step: action.step, confirmedChecked: false, dryRunError: action.step === 'list' ? state.dryRunError : '' };
    case 'reset':
      return { ...initialPruneState };
    default:
      return state;
  }
}

// --- selectors / helpers ---

export function selectedImageIds(state: PruneState): string[] {
  return Object.entries(state.selectedImages).filter(([, v]) => v).map(([k]) => k);
}
export function selectedVolumeNames(state: PruneState): string[] {
  return Object.entries(state.selectedVolumes).filter(([, v]) => v).map(([k]) => k);
}

export function selectedCount(state: PruneState): number {
  return selectedImageIds(state).length + selectedVolumeNames(state).length;
}

export function canDryRun(state: PruneState): boolean {
  if (state.listState !== 'ready') return false;
  return selectedCount(state) > 0;
}

export function canConfirm(state: PruneState): boolean {
  return !!state.dryRun && state.confirmedChecked && !state.deleting && selectedCount(state) > 0;
}

// buildDryRunSelection returns the selection payload for the dry-run request.
export function buildDryRunSelection(state: PruneState) {
  return { images: selectedImageIds(state), volumes: selectedVolumeNames(state) };
}

// buildConfirmRequest returns the body for the confirm request, binding the
// fingerprint from the dry-run so the server can detect a TOCTOU change.
export function buildConfirmRequest(state: PruneState, confirmLiteral = 'PRUNE') {
  return {
    confirm: confirmLiteral,
    fingerprint: state.dryRun?.candidates.fingerprint ?? '',
    images: selectedImageIds(state),
    volumes: selectedVolumeNames(state),
  };
}

// classifyHttpError maps a fetch Response to a user-presentable error key and
// returns whether the caller should force a re-dry-run (409) or re-auth (401).
export function classifyHttpError(status: number): { key: string; reauth: boolean; reDryRun: boolean } {
  if (status === 401) return { key: 'authFailed', reauth: true, reDryRun: false };
  if (status === 409) return { key: 'staleDryRun', reauth: false, reDryRun: true };
  if (status === 400) return { key: 'badRequest', reauth: false, reDryRun: false };
  if (status === 503) return { key: 'unavailable', reauth: false, reDryRun: false };
  return { key: 'serverError', reauth: false, reDryRun: false };
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

// shortImageId returns a stable 12-char display id.
export function shortImageId(id: string): string {
  const trimmed = id.startsWith('sha256:') ? id.slice(7) : id;
  return trimmed.length > 12 ? trimmed.slice(0, 12) : trimmed;
}

// summarizeItems partitions delete results by status and type.
export function summarizeItems(items: DeleteReport['items']) {
  const out = { deleted: 0, failed: 0, skipped: 0, images: 0, volumes: 0, reclaimed: 0 };
  for (const it of items) {
    if (it.type === 'image') out.images++;
    if (it.type === 'volume') out.volumes++;
    if (it.status === 'deleted') out.deleted++;
    if (it.status === 'failed') out.failed++;
    if (it.status === 'skipped') out.skipped++;
    out.reclaimed += it.reclaimed_bytes || 0;
  }
  return out;
}

// allSelected returns true if every candidate of both kinds is selected.
export function allSelected(state: PruneState): boolean {
  if (!state.candidates) return false;
  const imgs = state.candidates.images.every(i => state.selectedImages[i.id]);
  const vols = state.candidates.volumes.every(v => state.selectedVolumes[v.name]);
  return imgs && vols && (state.candidates.images.length + state.candidates.volumes.length > 0);
}
