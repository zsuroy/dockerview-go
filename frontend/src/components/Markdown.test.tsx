import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import { Markdown } from './Markdown';

afterEach(() => cleanup());

describe('Markdown', () => {
  it('renders a GFM pipe table with header and rows', () => {
    render(
      <Markdown
        text={'| 容器 | 状态 |\n| --- | --- |\n| api | running |\n| web | exited |'}
      />,
    );
    const table = screen.getByRole('table');
    expect(within(table).getByText('容器')).toBeTruthy();
    expect(within(table).getByText('running')).toBeTruthy();
    expect(within(table).getByText('exited')).toBeTruthy();
  });

  it('renders bold, inline code and links', () => {
    render(<Markdown text={'**别慌** 用 `docker ps` 看 [文档](https://example.com/x)'} />);
    expect(screen.getByText('别慌').closest('strong')).not.toBeNull();
    expect(screen.getByText('docker ps').closest('code')).not.toBeNull();
    const link = screen.getByText('文档').closest('a');
    expect(link).not.toBeNull();
    expect(link!.getAttribute('href')).toBe('https://example.com/x');
  });

  it('does not execute raw HTML and does not link javascript: URLs', () => {
    const { container } = render(
      <Markdown text={'<script>alert(1)</script> [点我](javascript:alert(1))'} />,
    );
    expect(container.querySelector('script')).toBeNull();
    expect(screen.getByText('<script>alert(1)</script>', { exact: false })).toBeTruthy();
    expect(container.querySelector('a')).toBeNull();
  });

  it('keeps fenced code literal without markdown inside', () => {
    render(<Markdown text={'```\n**不是粗体** | 也不是表格\n```'} />);
    expect(screen.getByText('**不是粗体** | 也不是表格', { exact: false }).tagName).toBe('PRE');
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('renders lists and headings', () => {
    render(<Markdown text={'## 标题\n- 第一\n- 第二'} />);
    expect(screen.getByText('标题')).toBeTruthy();
    expect(screen.getByText('第一')).toBeTruthy();
    expect(screen.getByText('第二')).toBeTruthy();
  });
});
