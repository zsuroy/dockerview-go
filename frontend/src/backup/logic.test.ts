import { describe, it, expect } from 'vitest';
import {
  initialBackupState, backupReducer, canCreate, includeImagesRisk,
  noteTooLong, classifyBackupError, formatBytes, formatTimestamp,
  totalSize, archivesWithImages, sortBackups,
} from './logic';
import type { BackupState, CreateReport, ListReport, PreviewReport } from './types';

const preview: PreviewReport = {
  containers: 3,
  images: [{ ref: 'nginx:1.27', size_bytes: 187 }],
  estimated_bytes: 4096,
  options: { include_images: false, include_stopped: false },
  warnings: [],
  would_include: ['manifest.json', 'containers.json'],
};

const createReport: CreateReport = {
  name: 'dockerview-backup-20260812T083015Z-a1b2c3.zip',
  path: 'data/backups/x.zip',
  size_bytes: 2048,
  containers: 3,
  images: 0,
  options: { include_images: false, include_stopped: false },
  note: 'n',
  duration_ms: 12,
  warnings: [],
};

const listReport: ListReport = {
  backups: [
    { name: 'b.zip', created_at: '2026-08-12T08:00:00Z', size_bytes: 100, include_images: false, containers: 1 },
    { name: 'a.zip', created_at: '2026-08-11T08:00:00Z', size_bytes: 200, include_images: true, containers: 2 },
  ],
  dir: 'data/backups',
  max_archives: 10,
};

describe('initialBackupState', () => {
  it('defaults include_images to false (design red line)', () => {
    expect(initialBackupState.includeImages).toBe(false);
  });

  it('starts with idle phases and no data', () => {
    expect(initialBackupState.previewPhase).toBe('idle');
    expect(initialBackupState.listPhase).toBe('idle');
    expect(initialBackupState.preview).toBeNull();
    expect(initialBackupState.list).toBeNull();
    expect(initialBackupState.creating).toBe(false);
  });
});

describe('backupReducer — preview', () => {
  it('preview/loading sets loading phase and clears error', () => {
    const s = backupReducer({ ...initialBackupState, previewError: 'old' }, { type: 'preview/loading' });
    expect(s.previewPhase).toBe('loading');
    expect(s.previewError).toBe('');
  });

  it('preview/success stores report and ready phase', () => {
    const s = backupReducer(initialBackupState, { type: 'preview/success', report: preview });
    expect(s.previewPhase).toBe('ready');
    expect(s.preview).toEqual(preview);
  });

  it('preview/error stores message and error phase', () => {
    const s = backupReducer(initialBackupState, { type: 'preview/error', error: 'boom' });
    expect(s.previewPhase).toBe('error');
    expect(s.previewError).toBe('boom');
  });
});

describe('backupReducer — form', () => {
  it('form/note updates note and clears createError', () => {
    const s = backupReducer({ ...initialBackupState, createError: 'x' }, { type: 'form/note', note: 'hello' });
    expect(s.note).toBe('hello');
    expect(s.createError).toBe('');
  });

  it('form/includeImages toggles and resets any stale preview', () => {
    const base: BackupState = {
      ...initialBackupState,
      preview,
      previewPhase: 'ready',
    };
    const s = backupReducer(base, { type: 'form/includeImages', checked: true });
    expect(s.includeImages).toBe(true);
    expect(s.preview).toBeNull();
    expect(s.previewPhase).toBe('idle');
  });
});

describe('backupReducer — create', () => {
  it('create/start sets creating and clears error', () => {
    const s = backupReducer({ ...initialBackupState, createError: 'e' }, { type: 'create/start' });
    expect(s.creating).toBe(true);
    expect(s.createError).toBe('');
  });

  it('create/success stores report and unsets creating', () => {
    const s = backupReducer({ ...initialBackupState, creating: true }, { type: 'create/success', report: createReport });
    expect(s.creating).toBe(false);
    expect(s.lastCreated).toEqual(createReport);
  });

  it('create/error stores message and unsets creating', () => {
    const s = backupReducer({ ...initialBackupState, creating: true }, { type: 'create/error', error: 'bad' });
    expect(s.creating).toBe(false);
    expect(s.createError).toBe('bad');
  });
});

describe('backupReducer — list/delete/reset', () => {
  it('list/loading → list/success lifecycle', () => {
    let s = backupReducer(initialBackupState, { type: 'list/loading' });
    expect(s.listPhase).toBe('loading');
    s = backupReducer(s, { type: 'list/success', report: listReport });
    expect(s.listPhase).toBe('ready');
    expect(s.list?.backups).toHaveLength(2);
  });

  it('list/error stores message', () => {
    const s = backupReducer(initialBackupState, { type: 'list/error', error: 'nope' });
    expect(s.listPhase).toBe('error');
    expect(s.listError).toBe('nope');
  });

  it('delete/start tracks the deleting archive; delete/stop clears it', () => {
    let s = backupReducer(initialBackupState, { type: 'delete/start', name: 'x.zip' });
    expect(s.deletingName).toBe('x.zip');
    s = backupReducer(s, { type: 'delete/stop' });
    expect(s.deletingName).toBe('');
  });

  it('reset returns to initial state', () => {
    const dirty: BackupState = {
      ...initialBackupState,
      note: 'x',
      includeImages: true,
      creating: true,
      preview,
      list: listReport,
    };
    expect(backupReducer(dirty, { type: 'reset' })).toEqual(initialBackupState);
  });

  it('unknown action returns state unchanged', () => {
    const s = backupReducer(initialBackupState, { type: 'nope' } as never);
    expect(s).toEqual(initialBackupState);
  });
});

describe('selectors', () => {
  it('canCreate is false while creating (double-submit guard)', () => {
    expect(canCreate(initialBackupState)).toBe(true);
    expect(canCreate({ ...initialBackupState, creating: true })).toBe(false);
  });

  it('includeImagesRisk mirrors the toggle', () => {
    expect(includeImagesRisk(initialBackupState)).toBe(false);
    expect(includeImagesRisk({ ...initialBackupState, includeImages: true })).toBe(true);
  });

  it('noteTooLong enforces the 500-char cap by code points', () => {
    expect(noteTooLong('x'.repeat(500))).toBe(false);
    expect(noteTooLong('x'.repeat(501))).toBe(true);
    // CJK characters count as one code point each
    expect(noteTooLong('值'.repeat(500))).toBe(false);
    expect(noteTooLong('值'.repeat(501))).toBe(true);
  });

  it('totalSize sums archive sizes and tolerates null', () => {
    expect(totalSize(null)).toBe(0);
    expect(totalSize(listReport)).toBe(300);
  });

  it('archivesWithImages counts image-bearing archives', () => {
    expect(archivesWithImages(null)).toBe(0);
    expect(archivesWithImages(listReport)).toBe(1);
  });

  it('sortBackups sorts newest-first, corrupt (empty created_at) last', () => {
    const items = [
      { name: 'old', created_at: '2026-01-01T00:00:00Z' },
      { name: 'corrupt', created_at: '' },
      { name: 'new', created_at: '2026-08-01T00:00:00Z' },
    ];
    const sorted = sortBackups(items);
    expect(sorted.map(i => i.name)).toEqual(['new', 'old', 'corrupt']);
  });
});

describe('classifyBackupError', () => {
  it('maps 401 to reauth', () => {
    expect(classifyBackupError(401)).toEqual({ key: 'authFailed', reauth: true });
  });

  it('maps 409 to busy without reauth', () => {
    const c = classifyBackupError(409);
    expect(c.key).toBe('busy');
    expect(c.reauth).toBe(false);
  });

  it('maps 502 to export failure', () => {
    expect(classifyBackupError(502).key).toBe('exportFailed');
  });

  it('maps 503 to unavailable and others to serverError', () => {
    expect(classifyBackupError(503).key).toBe('unavailable');
    expect(classifyBackupError(500).key).toBe('serverError');
    expect(classifyBackupError(0).key).toBe('serverError');
  });
});

describe('formatBytes', () => {
  it('handles zero, negative and NaN', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(-5)).toBe('0 B');
    expect(formatBytes(NaN)).toBe('0 B');
  });

  it('formats increasing units', () => {
    expect(formatBytes(512)).toBe('512.0 B');
    expect(formatBytes(1024)).toBe('1.0 KB');
    expect(formatBytes(1024 * 1024)).toBe('1.0 MB');
    expect(formatBytes(5 * 1024 ** 3)).toBe('5.0 GB');
  });
});

describe('formatTimestamp', () => {
  it('renders a local YYYY-MM-DD HH:mm:ss string', () => {
    const out = formatTimestamp('2026-08-12T08:30:15Z');
    expect(out).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);
  });

  it('returns dash for empty and the raw value when unparseable', () => {
    expect(formatTimestamp('')).toBe('—');
    expect(formatTimestamp('not-a-date')).toBe('not-a-date');
  });
});
