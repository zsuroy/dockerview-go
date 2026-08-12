// Backup snapshot feature types — mirror the Go API shapes
// (internal/backup/types.go). Keep JSON field names in sync.

export interface BackupImageInfo {
  ref: string;
  id?: string;
  size_bytes: number;
}

export interface BackupOptions {
  include_images: boolean;
  include_stopped: boolean;
}

export interface PreviewReport {
  containers: number;
  images: BackupImageInfo[] | null;
  estimated_bytes: number;
  options: BackupOptions;
  warnings: string[];
  would_include: string[];
}

export interface CreateReport {
  name: string;
  path: string;
  size_bytes: number;
  containers: number;
  images: number;
  options: BackupOptions;
  note?: string;
  duration_ms: number;
  warnings: string[];
}

export interface BackupListItem {
  name: string;
  created_at: string;
  size_bytes: number;
  note?: string;
  include_images: boolean;
  containers: number;
  corrupt?: boolean;
}

export interface ListReport {
  backups: BackupListItem[];
  dir: string;
  max_archives: number;
}

// UI state machine (logic.ts).
export type PreviewPhase = 'idle' | 'loading' | 'ready' | 'error';
export type ListPhase = 'idle' | 'loading' | 'ready' | 'error';

export interface BackupState {
  // preview
  previewPhase: PreviewPhase;
  preview: PreviewReport | null;
  previewError: string;
  // create form
  note: string;
  includeImages: boolean;
  includeStopped: boolean;
  creating: boolean;
  createError: string;
  lastCreated: CreateReport | null;
  // history
  listPhase: ListPhase;
  list: ListReport | null;
  listError: string;
  deletingName: string;
}
