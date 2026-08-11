// Prune feature types — shared between the UI component and the pure logic
// module so the state machine can be tested without React/DOM.

export interface ImageCandidate {
  id: string;
  short_id: string;
  tags: string[];
  size: number;
  shared_size: number;
  created: number;
  containers: number;
  reason: string;
}

export interface VolumeCandidate {
  name: string;
  driver: string;
  mountpoint: string;
  size: number; // -1 when unknown
  ref_count: number;
  created_at?: string;
  reason: string;
}

export interface Candidates {
  images: ImageCandidate[];
  volumes: VolumeCandidate[];
  images_count: number;
  volumes_count: number;
  images_size: number;
  volumes_size: number;
  total_size: number;
  generated_at: string;
  fingerprint: string;
}

export interface ScopeCount {
  images: number;
  volumes: number;
  estimated_reclaim_bytes: number;
}

export interface DryRunReport {
  dry_run: boolean;
  candidates: Candidates;
  will_delete: ScopeCount;
  warnings: string[];
  generated_at: string;
}

export type ItemStatus = 'deleted' | 'failed' | 'skipped';

export interface DeleteItemResult {
  type: 'image' | 'volume';
  id?: string;
  name?: string;
  status: ItemStatus;
  error?: string;
  reclaimed_bytes: number;
}

export interface DeleteSummary {
  deleted: number;
  failed: number;
  skipped: number;
  reclaimed_bytes: number;
}

export interface DeleteReport {
  dry_run: boolean;
  confirmed: boolean;
  fingerprint_matched: boolean;
  items: DeleteItemResult[];
  summary: DeleteSummary;
  warnings: string[];
  started_at: string;
  finished_at: string;
}

export interface AuditEvent {
  id: string;
  time: string;
  actor: string;
  actor_ip: string;
  action: string;
  status: string;
  images_deleted: number;
  volumes_deleted: number;
  images_failed: number;
  volumes_failed: number;
  skipped: number;
  reclaimed_bytes: number;
  detail?: string;
}

export type Step = 'list' | 'dryrun' | 'confirm' | 'result';
export type LoadState = 'idle' | 'loading' | 'ready' | 'empty' | 'error';

export interface PruneState {
  step: Step;
  listState: LoadState;
  listError: string;
  candidates: Candidates | null;
  selectedImages: Record<string, boolean>;
  selectedVolumes: Record<string, boolean>;
  dryRun: DryRunReport | null;
  dryRunLoading: boolean;
  dryRunError: string;
  confirmedChecked: boolean;
  deleting: boolean;
  result: DeleteReport | null;
  audit: AuditEvent[];
}
