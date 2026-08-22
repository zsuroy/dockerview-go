import { describe, it, expect } from 'vitest';
import {
  basename, canUpload, errorMessage, isAuthError, isOverwriteGate, isTooLarge,
  joinPath, looksTraversal, quotaPct, shortSha,
} from './logic';

describe('files logic', () => {
  it('classifies http status errors', () => {
    expect(isAuthError(Object.assign(new Error('x'), { status: 401 }))).toBe(true);
    expect(isAuthError(Object.assign(new Error('x'), { status: 500 }))).toBe(false);
    expect(isOverwriteGate(Object.assign(new Error('x'), { status: 409 }))).toBe(true);
    expect(isTooLarge(Object.assign(new Error('x'), { status: 413 }))).toBe(true);
    expect(errorMessage(Object.assign(new Error('boom'), { status: 413 }))).toContain('boom');
  });

  it('computes quota percentages with clamp', () => {
    expect(quotaPct(4, 8)).toBe(50);
    expect(quotaPct(9, 8)).toBe(100);
    expect(quotaPct(0, 0)).toBe(0);
  });

  it('extracts basenames portably', () => {
    expect(basename('a/b/c.txt')).toBe('c.txt');
    expect(basename('x\\y\\z.bin')).toBe('z.bin');
    expect(basename('top')).toBe('top');
    expect(basename('a/b/')).toBe('b');
  });

  it('joins jail-relative paths', () => {
    expect(joinPath('', 'a.txt')).toBe('a.txt');
    expect(joinPath('.', 'a.txt')).toBe('a.txt');
    expect(joinPath('sub', 'b.txt')).toBe('sub/b.txt');
    expect(joinPath('sub//', 'b.txt')).toBe('sub/b.txt');
  });

  it('flags traversal shapes without false-positiving normal names', () => {
    expect(looksTraversal('../x')).toBe(true);
    expect(looksTraversal('a/../../b')).toBe(true);
    expect(looksTraversal('a\\b')).toBe(true);
    expect(looksTraversal('logs/app.log')).toBe(false);
    expect(looksTraversal('my..file')).toBe(false);
    expect(looksTraversal('日志-文件.txt')).toBe(false);
  });

  it('gates the upload confirm button', () => {
    expect(canUpload({ hasFile: false, path: 'a', busy: false }).ok).toBe(false);
    expect(canUpload({ hasFile: true, path: '', busy: false }).ok).toBe(false);
    expect(canUpload({ hasFile: true, path: '../a', busy: false }).ok).toBe(false);
    expect(canUpload({ hasFile: true, path: 'a', busy: true }).ok).toBe(false);
    expect(canUpload({ hasFile: true, path: 'logs/a', busy: false }).ok).toBe(true);
  });

  it('truncates shas for display only', () => {
    expect(shortSha('abcdef1234567890')).toBe('abcdef123456…');
    expect(shortSha('short')).toBe('short');
  });
});
