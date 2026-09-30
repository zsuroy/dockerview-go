import { useMemo, type ReactNode } from 'react';

// Minimal chat markdown renderer for model-generated replies.
//
// Zero dependencies (npm registry is unreachable from here): covers the
// subset the duty model actually emits — GFM pipe tables, fenced code,
// headings, lists, blockquotes, bold/italic/inline-code/links. Everything
// else renders as plain paragraphs. No dangerouslySetInnerHTML anywhere:
// output is React elements built from strings, so raw HTML in the source
// is displayed literally and can never execute.

type InlineNode = string | { b: InlineNode[] } | { i: InlineNode[] } | { c: string } | { a: { label: InlineNode[]; href: string } };

function parseInline(src: string): InlineNode[] {
  const out: InlineNode[] = [];
  // code | bold | italic | link — single pass, code wins over the rest.
  const re = /(`[^`\n]+`)|\*\*([^*]+)\*\*|\*([^*]+)\*|\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g;
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(src)) !== null) {
    if (m.index > last) out.push(src.slice(last, m.index));
    if (m[1] !== undefined) {
      out.push({ c: m[1].slice(1, -1) });
    } else if (m[2] !== undefined) {
      out.push({ b: parseInline(m[2]) });
    } else if (m[3] !== undefined) {
      out.push({ i: parseInline(m[3]) });
    } else {
      out.push({ a: { label: parseInline(m[4]), href: m[5] } });
    }
    last = m.index + m[0].length;
  }
  if (last < src.length) out.push(src.slice(last));
  return out;
}

function renderInline(nodes: InlineNode[], keyPrefix: string): ReactNode[] {
  return nodes.map((n, i) => {
    const key = `${keyPrefix}-${i}`;
    if (typeof n === 'string') return <span key={key}>{n}</span>;
    if ('c' in n) {
      return (
        <code key={key} className="font-mono text-[0.92em] bg-surface-2 border border-border-subtle rounded px-1 py-px">
          {n.c}
        </code>
      );
    }
    if ('b' in n) return <strong key={key} className="font-bold">{renderInline(n.b, key)}</strong>;
    if ('i' in n) return <em key={key}>{renderInline(n.i, key)}</em>;
    return (
      <a key={key} href={n.a.href} target="_blank" rel="noreferrer" className="text-accent-cyan underline underline-offset-2 break-all">
        {renderInline(n.a.label, key)}
      </a>
    );
  });
}

function withBreaks(text: string, keyPrefix: string): ReactNode[] {
  // Chat replies use newlines loosely; preserve them like the old
  // whitespace-pre-wrap rendering did instead of joining into one line.
  const parts = text.split('\n');
  const out: ReactNode[] = [];
  parts.forEach((p, i) => {
    if (i > 0) out.push(<br key={`${keyPrefix}-br-${i}`} />);
    renderInline(parseInline(p), `${keyPrefix}-l${i}`).forEach((n) => out.push(n));
  });
  return out;
}

interface TableData {
  head: string[];
  align: ('left' | 'center' | 'right')[];
  rows: string[][];
}

function splitRow(line: string): string[] {
  let s = line.trim();
  if (s.startsWith('|')) s = s.slice(1);
  if (s.endsWith('|')) s = s.slice(0, -1);
  return s.split('|').map((c) => c.trim());
}

function isDelim(line: string): boolean {
  const cells = splitRow(line);
  return cells.length > 0 && cells.every((c) => /^:?-+:?$/.test(c));
}

function parseAlign(line: string): ('left' | 'center' | 'right')[] {
  return splitRow(line).map((c) => {
    if (c.startsWith(':') && c.endsWith(':')) return 'center';
    if (c.endsWith(':')) return 'right';
    return 'left';
  });
}

export function Markdown({ text, className }: { text: string; className?: string }) {
  const blocks = useMemo(() => {
    const out: ReactNode[] = [];
    const lines = text.replace(/\r\n/g, '\n').split('\n');
    let i = 0;
    let k = 0;
    const key = () => `md-${k++}`;

    while (i < lines.length) {
      const line = lines[i];

      // Fenced code block.
      if (line.trimStart().startsWith('```')) {
        const buf: string[] = [];
        i++;
        while (i < lines.length && !lines[i].trimStart().startsWith('```')) {
          buf.push(lines[i]);
          i++;
        }
        i++; // consume closing fence (or EOF)
        out.push(
          <pre key={key()} className="font-mono text-[11.5px] leading-relaxed bg-surface-2 border border-border-subtle rounded-lg p-2.5 my-1.5 overflow-x-auto whitespace-pre">
            {buf.join('\n')}
          </pre>,
        );
        continue;
      }

      // GFM pipe table: header + delimiter + body.
      if (line.trimStart().startsWith('|') && i + 1 < lines.length && isDelim(lines[i + 1])) {
        const t: TableData = { head: splitRow(line), align: parseAlign(lines[i + 1]), rows: [] };
        i += 2;
        while (i < lines.length && lines[i].trim() !== '' && lines[i].includes('|')) {
          t.rows.push(splitRow(lines[i]));
          i++;
        }
        out.push(
          <div key={key()} className="my-1.5 overflow-x-auto rounded-lg border border-border-subtle">
            <table className="w-full border-collapse text-[11.5px] leading-snug">
              <thead>
                <tr className="bg-surface-2">
                  {t.head.map((h, ci) => (
                    <th
                      key={ci}
                      style={{ textAlign: t.align[ci] ?? 'left' }}
                      className="font-bold text-text px-2 py-1.5 border-b border-border-light whitespace-nowrap"
                    >
                      {renderInline(parseInline(h), `th-${k}-${ci}`)}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {t.rows.map((r, ri) => (
                  <tr key={ri} className="odd:bg-transparent even:bg-surface-1/60">
                    {t.head.map((_, ci) => (
                      <td
                        key={ci}
                        style={{ textAlign: t.align[ci] ?? 'left' }}
                        className="px-2 py-1.5 border-b border-border-subtle text-text/90 align-top"
                      >
                        {renderInline(parseInline(r[ci] ?? ''), `td-${k}-${ri}-${ci}`)}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>,
        );
        continue;
      }

      // ATX heading (cap at h4 size — chat, not a document).
      const hm = /^(#{1,4})\s+(.+)$/.exec(line);
      if (hm) {
        const level = hm[1].length;
        const cls =
          level === 1
            ? 'text-[14px] font-extrabold'
            : level === 2
              ? 'text-[13px] font-extrabold'
              : 'text-[12.5px] font-bold';
        out.push(
          <div key={key()} className={`${cls} text-text mt-2 mb-1 first:mt-0`}>
            {renderInline(parseInline(hm[2]), `h-${k}`)}
          </div>,
        );
        i++;
        continue;
      }

      // Blockquote run.
      if (/^>\s?/.test(line)) {
        const buf: string[] = [];
        while (i < lines.length && /^>\s?/.test(lines[i])) {
          buf.push(lines[i].replace(/^>\s?/, ''));
          i++;
        }
        out.push(
          <div key={key()} className="border-l-2 border-accent-cyan/50 pl-2.5 my-1.5 text-text/90">
            {withBreaks(buf.join('\n'), `q-${k}`)}
          </div>,
        );
        continue;
      }

      // List run.
      const listMatch = /^(\s*)([-*]|\d+[.)])\s+(.+)$/.exec(line);
      if (listMatch) {
        const ordered = /^\d/.test(listMatch[2].trim());
        const items: string[] = [];
        while (i < lines.length) {
          const lm = /^(\s*)([-*]|\d+[.)])\s+(.+)$/.exec(lines[i]);
          if (!lm) break;
          items.push(lm[3]);
          i++;
        }
        const ListTag = ordered ? 'ol' : 'ul';
        out.push(
          <ListTag key={key()} className={`${ordered ? 'list-decimal' : 'list-disc'} ml-5 my-1 space-y-0.5 marker:text-text-dim`}>
            {items.map((it, ii) => (
              <li key={ii} className="pl-0.5">
                {renderInline(parseInline(it), `li-${k}-${ii}`)}
              </li>
            ))}
          </ListTag>,
        );
        continue;
      }

      // Blank line separates paragraphs.
      if (line.trim() === '') {
        i++;
        continue;
      }

      // Paragraph run.
      const buf: string[] = [];
      while (
        i < lines.length &&
        lines[i].trim() !== '' &&
        !lines[i].trimStart().startsWith('```') &&
        !/^(#{1,4})\s+/.test(lines[i]) &&
        !/^>\s?/.test(lines[i]) &&
        !/^(\s*)([-*]|\d+[.)])\s+/.test(lines[i]) &&
        !(lines[i].trimStart().startsWith('|') && i + 1 < lines.length && isDelim(lines[i + 1]))
      ) {
        buf.push(lines[i]);
        i++;
      }
      out.push(
        <p key={key()} className="my-1 first:mt-0 last:mb-0">
          {withBreaks(buf.join('\n'), `p-${k}`)}
        </p>,
      );
    }
    return out;
  }, [text]);

  return <div className={className ?? 'text-[12.5px] leading-relaxed text-text break-words'}>{blocks}</div>;
}
