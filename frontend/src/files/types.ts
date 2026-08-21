// Types for the confined container file-transfer feature.
// Mirrors internal/server files_handlers JSON shapes.

export interface FilesEntry {
  name: string;
  size: number;
  is_dir: boolean;
  is_symlink: boolean;
}

export interface ListReport {
  path: string;
  entries: FilesEntry[];
}

export interface InPreviewReport {
  path: string;
  exists: boolean;
  overwrite_required: boolean;
  size_existing: number;
  max_file_bytes: number;
  /** Absolute container dirs confirm will create when mkdir is confirmed. */
  missing_dirs?: string[];
  mkdir_required?: boolean;
}

export interface OutPreviewReport {
  path: string;
  size: number;
  sha256: string;
  name: string;
}

export interface ArchivePreviewReport {
  path: string;
  entries: number;
  bytes: number;
  max_archive_bytes: number;
  name: string;
}

export interface InResult {
  path: string;
  bytes: number;
  sha256: string;
  overwritten: boolean;
}

export type FilesBusy =
  | 'idle'
  | 'listing'
  | 'previewIn'
  | 'uploading'
  | 'previewOut'
  | 'downloading'
  | 'archiving';
