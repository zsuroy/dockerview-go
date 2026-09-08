import { useCallback, useEffect, useMemo, useRef, useState, type ReactElement } from 'react';
import { AlertTriangle, Download, Network as NetworkIcon, RefreshCw, X, ZoomIn, ZoomOut, Maximize } from 'lucide-react';
import { useTranslation } from '../i18n';
import { fetchTopology, TopologyFetchError } from './api';
import { buildLayout } from './layout';
import { downloadGraphSvg, svgFilename } from './exportSvg';
import type { GraphNode, NetworkTopology } from './types';
import './network.css';

type LoadState = 'loading' | 'ready' | 'error';

interface ViewTransform {
  x: number;
  y: number;
  scale: number;
}

function nodeWidth(name: string): number {
  return Math.min(112, Math.max(54, name.length * 8 + 30));
}

function truncateNetworkName(name: string, maxChars: number): string {
  if (name.length <= maxChars) return name;
  const keep = Math.max(1, maxChars - 1);
  return name.slice(0, keep) + '…';
}

export default function NetworkPanel(): ReactElement {
  const { t } = useTranslation();
  const [state, setState] = useState<LoadState>('loading');
  const [topology, setTopology] = useState<NetworkTopology | null>(null);
  const [loadError, setLoadError] = useState<TopologyFetchError | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const svgRef = useRef<SVGSVGElement | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [transform, setTransform] = useState<ViewTransform>({ x: 0, y: 0, scale: 1 });
  const isPanning = useRef(false);
  const panStart = useRef({ x: 0, y: 0 });

  const load = useCallback(async () => {
    setState('loading');
    setLoadError(null);
    try {
      const data = await fetchTopology();
      setTopology(data);
      setState('ready');
    } catch (err) {
      setLoadError(err instanceof TopologyFetchError ? err : new TopologyFetchError(0, 'network_error', String(err)));
      setState('error');
    }
  }, []);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const data = await fetchTopology();
        if (alive) {
          setTopology(data);
          setState('ready');
        }
      } catch (err) {
        if (alive) {
          setLoadError(
            err instanceof TopologyFetchError ? err : new TopologyFetchError(0, 'network_error', String(err)),
          );
          setState('error');
        }
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  const layout = useMemo(() => (topology ? buildLayout(topology) : null), [topology]);
  const selected: GraphNode | undefined = useMemo(
    () => layout?.nodes.find((n) => n.id === selectedId),
    [layout, selectedId],
  );

  const handleExport = useCallback(() => {
    if (!svgRef.current) return;
    downloadGraphSvg(svgRef.current, svgFilename(new Date()));
  }, []);

  const clampScale = useCallback((scale: number) => Math.min(4, Math.max(0.1, scale)), []);

  const handleWheel = useCallback((e: React.WheelEvent) => {
    if (!layout) return;
    e.preventDefault();
    const delta = -e.deltaY * 0.001;
    setTransform((prev) => {
      const newScale = clampScale(prev.scale * (1 + delta));
      const rect = containerRef.current?.getBoundingClientRect();
      if (!rect) return prev;
      const mx = e.clientX - rect.left;
      const my = e.clientY - rect.top;
      const ratio = newScale / prev.scale;
      const nx = mx - ratio * (mx - prev.x);
      const ny = my - ratio * (my - prev.y);
      return { x: nx, y: ny, scale: newScale };
    });
  }, [layout, clampScale]);

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    if (e.button !== 0) return;
    if ((e.target as SVGElement).closest('.container-node')) return;
    isPanning.current = true;
    panStart.current = { x: e.clientX - transform.x, y: e.clientY - transform.y };
  }, [transform]);

  const handleMouseMove = useCallback((e: React.MouseEvent) => {
    if (!isPanning.current) return;
    setTransform((prev) => ({
      ...prev,
      x: e.clientX - panStart.current.x,
      y: e.clientY - panStart.current.y,
    }));
  }, []);

  const handleMouseUp = useCallback(() => {
    isPanning.current = false;
  }, []);

  const resetView = useCallback(() => {
    setTransform({ x: 0, y: 0, scale: 1 });
  }, []);

  const zoomIn = useCallback(() => {
    setTransform((prev) => ({ ...prev, scale: clampScale(prev.scale * 1.25) }));
  }, [clampScale]);

  const zoomOut = useCallback(() => {
    setTransform((prev) => ({ ...prev, scale: clampScale(prev.scale / 1.25) }));
  }, [clampScale]);

  return (
    <div className="space-y-4" data-testid="network-panel">
      <div className="flex items-center gap-2 flex-wrap glass-panel px-4 py-3 rounded-xl border">
        <NetworkIcon size={16} className="text-accent-cyan" />
        <span className="text-xs font-bold tracking-wider uppercase text-text-dim">
          {t('network.nav')}
        </span>
        <span className="px-2 py-0.5 rounded-md border border-accent-cyan/40 text-accent-cyan text-[11px] font-bold">
          {t('network.guestBadge')}
        </span>
        {layout && (
          <span className="text-[11px] text-text-dim">
            {t('network.summary', { nets: layout.groups.length, nodes: layout.nodes.length })}
          </span>
        )}
        <div className="flex-1" />
        <button
          className="action-btn flex items-center gap-1.5"
          data-testid="network-refresh"
          onClick={() => void load()}
        >
          <RefreshCw size={13} />
          {t('network.refresh')}
        </button>
        <button
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-accent-cyan font-bold text-[11px] text-black disabled:opacity-50"
          data-testid="network-export"
          disabled={state !== 'ready' || !layout}
          onClick={handleExport}
        >
          <Download size={13} />
          {t('network.export')}
        </button>
        <button
          className="action-btn flex items-center gap-1.5"
          data-testid="network-zoom-in"
          onClick={zoomIn}
          disabled={!layout}
          title="Zoom in"
        >
          <ZoomIn size={13} />
        </button>
        <button
          className="action-btn flex items-center gap-1.5"
          data-testid="network-zoom-out"
          onClick={zoomOut}
          disabled={!layout}
          title="Zoom out"
        >
          <ZoomOut size={13} />
        </button>
        <button
          className="action-btn flex items-center gap-1.5"
          data-testid="network-reset-view"
          onClick={resetView}
          disabled={!layout}
          title="Reset view"
        >
          <Maximize size={13} />
        </button>
      </div>

      {state === 'error' && (
        <div
          className="rounded-xl border px-6 py-8 text-center"
          data-testid="network-error"
          style={{
            borderColor: 'rgba(255,0,85,.45)',
            background: 'rgba(255,0,85,.08)',
          }}
        >
          <AlertTriangle className="mx-auto mb-2" color="var(--theme-danger)" size={28} />
          <h3 className="text-sm font-extrabold mb-1" style={{ color: 'var(--theme-danger)' }}>
            {t('network.errorTitle')}
          </h3>
          <p className="text-[12px] text-text-dim mb-3">
            <code className="px-2 py-0.5 rounded bg-surface-3">
              {loadError?.status ? `${loadError.status} ` : ''}
              {loadError?.code ?? 'network_error'}
            </code>
            {loadError?.message ? <><br />{loadError.message}</> : null}
          </p>
          <button
            className="action-btn px-4 py-2 rounded-lg font-bold text-[12px]"
            onClick={() => void load()}
          >
            <RefreshCw size={13} className="inline mr-1" />
            {t('network.retry')}
          </button>
        </div>
      )}

      {state === 'loading' && (
        <div className="glass-panel rounded-xl border px-6 py-16 text-center text-[13px] text-text-dim">
          {t('network.loading')}
        </div>
      )}

      {state === 'ready' && layout && (
        <div ref={containerRef} className="relative overflow-hidden">
          {layout.groups.length === 0 ? (
            <div className="glass-panel rounded-xl border px-6 py-16 text-center text-[13px] text-text-dim">
              {t('network.emptyAll')}
            </div>
          ) : (
            <svg
              ref={svgRef}
              className="network-graph"
              data-testid="network-graph"
              viewBox={`0 0 ${layout.width} ${layout.height}`}
              role="img"
              aria-label={t('network.title')}
              onClick={() => setSelectedId(null)}
              onWheel={handleWheel}
              onMouseDown={handleMouseDown}
              onMouseMove={handleMouseMove}
              onMouseUp={handleMouseUp}
              onMouseLeave={handleMouseUp}
              style={{ cursor: isPanning.current ? 'grabbing' : 'grab', touchAction: 'none' }}
            >
              <g transform={`translate(${transform.x}, ${transform.y}) scale(${transform.scale})`}>
                <g className="networks-layer">
                  {layout.groups.map((g) => (
                    <g key={g.name} className="network-group" data-network={g.name}>
                      <rect
                        className={`network-frame ${g.empty ? 'empty' : ''} ${g.internal ? 'internal' : ''}`}
                        x={g.x}
                        y={g.y}
                        width={g.w}
                        height={g.h}
                        rx={16}
                      />
                      <text className="net-title" x={g.x + 16} y={g.y + 24}>
                        {truncateNetworkName(g.name, 28)}
                        <title>{g.name}</title>
                      </text>
                      <text className="net-count" x={g.x + 16} y={g.y + 42}>
                        {t('network.containerCount', { count: g.count })}
                      </text>
                      {g.empty ? (
                        <text className="net-empty-hint" x={g.x + 16} y={g.y + 72}>
                          {t('network.emptyHint')}
                        </text>
                      ) : null}
                      <text className="net-meta" x={g.x + g.w - 16} y={g.y + 24} textAnchor="end">
                        {g.driver} · {g.scope}
                        {g.internal ? ` · ${t('network.internal')}` : ''}
                      </text>
                    </g>
                  ))}
                </g>

                <g className="edges-layer">
                  {layout.links.map((l) => {
                    const s = l.source as GraphNode;
                    const d = l.target as GraphNode;
                    return (
                      <line
                        key={l.id}
                        className="net-edge"
                        data-edge={l.id}
                        data-network={l.network}
                        data-from={l.sourceName}
                        data-to={l.targetName}
                        x1={s.x ?? 0}
                        y1={s.y ?? 0}
                        x2={d.x ?? 0}
                        y2={d.y ?? 0}
                      />
                    );
                  })}
                </g>

                <g className="nodes-layer">
                  {layout.nodes.map((n) => {
                    const w = nodeWidth(n.name);
                    return (
                      <g
                        key={n.id}
                        className={`container-node ${n.running ? 'running' : 'stopped'} ${
                          selectedId === n.id ? 'selected' : ''
                        }`}
                        data-container={n.name}
                        data-container-id={n.id}
                        data-running={n.running ? '1' : '0'}
                        transform={`translate(${n.x ?? 0},${n.y ?? 0})`}
                        onClick={(e) => {
                          e.stopPropagation();
                          setSelectedId((cur) => (cur === n.id ? null : n.id));
                        }}
                      >
                        <rect className="body" x={-w / 2} y={-23} width={w} height={46} rx={11} />
                        <circle
                          className="status-dot"
                          cx={-w / 2 + 15}
                          cy={0}
                          r={4.5}
                          fill={n.running ? 'var(--theme-success)' : 'var(--theme-warning)'}
                        />
                        <text className="cname" x={-w / 2 + 27} y={-3}>
                          {n.name}
                        </text>
                        {!n.running && (
                          <text className="state-tag" x={-w / 2 + 27} y={14}>
                            {t('network.stoppedTag')}
                          </text>
                        )}
                      </g>
                    );
                  })}
                </g>
              </g>
            </svg>
          )}

          {selected && (
            <div
              className="absolute right-5 top-5 w-72 p-4 rounded-2xl border z-10 backdrop-blur-md"
              data-testid="network-node-card"
              style={{
                background: 'var(--theme-modal-bg)',
                borderColor: 'var(--theme-border-default)',
                boxShadow: '0 22px 60px rgba(0,0,0,.45)',
              }}
            >
              <div className="flex items-start justify-between gap-2">
                <h4 className="text-sm font-extrabold flex items-center gap-2">
                  <span
                    className="w-2.5 h-2.5 rounded-full"
                    style={{
                      background: selected.running
                        ? 'var(--theme-success)'
                        : 'var(--theme-warning)',
                    }}
                  />
                  {selected.name}
                </h4>
                <button
                  className="text-text-dim hover:text-text"
                  data-testid="network-node-card-close"
                  onClick={() => setSelectedId(null)}
                >
                  <X size={14} />
                </button>
              </div>
              <div className="text-[11px] font-bold tracking-wider uppercase text-text-dim mt-1 mb-2">
                {selected.running ? t('network.runningTag') : t('network.stoppedTag')}
              </div>
              {selected.members.map((m) => (
                <div
                  key={m.network}
                  className="flex justify-between gap-3 text-[12px] py-1.5 border-b border-dashed"
                  style={{ borderColor: 'var(--theme-border-light)' }}
                  data-testid="network-node-card-row"
                  data-network={m.network}
                >
                  <span>{m.network}</span>
                  <span className="font-mono text-text-dim">{m.ipv4 || 'empty'}</span>
                </div>
              ))}
              <p className="text-[11px] text-text-dim mt-2 leading-relaxed">{t('network.cardHint')}</p>
            </div>
          )}
        </div>
      )}

      {layout && (
        <div className="glass-panel rounded-xl border px-4 py-3">
          <div className="text-[12px] font-extrabold tracking-widest uppercase text-text-dim mb-2">
            {t('network.annotationTitle')}
          </div>
          <table className="w-full text-[12.5px]" data-testid="network-counts-table">
            <thead>
              <tr className="text-text-dim">
                <th className="text-left py-1">{t('network.colNetwork')}</th>
                <th className="text-left py-1">{t('network.colDriver')}</th>
                <th className="text-right py-1">{t('network.colTotal')}</th>
                <th className="text-right py-1">{t('network.colRunning')}</th>
                <th className="text-right py-1">{t('network.colStopped')}</th>
              </tr>
            </thead>
            <tbody>
              {topology?.networks.map((n) => {
                const running = n.containers.filter((c) => c.running).length;
                return (
                  <tr key={n.name} data-counts-row={n.name} style={{ borderTop: '1px solid var(--theme-border-light)' }}>
                    <td className="py-1 font-bold">{n.name}</td>
                    <td className="py-1 text-text-dim">{n.driver}</td>
                    <td className="py-1 text-right">{n.containers.length}</td>
                    <td className="py-1 text-right">{running}</td>
                    <td className="py-1 text-right">{n.containers.length - running}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          <p className="text-[11px] text-text-dim mt-2">{t('network.legendHint')}</p>
        </div>
      )}
    </div>
  );
}
