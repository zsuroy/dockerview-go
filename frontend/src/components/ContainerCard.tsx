import { useEffect, useRef } from 'react';
import { ArrowDownUp, HardDrive, Play, Square, RefreshCw, Terminal, HeartPulse, Command, FolderOpen } from 'lucide-react';
import type { Container } from '../types';
import { getMemoryPercent } from '../utils';
import { Sparkline, HighlightedText } from './Sparkline';
import { useTranslation } from '../i18n';

interface ContainerCardProps {
  container: Container;
  history?: { cpu: number[]; ram: number[] };
  onOp: (id: string, op: 'start' | 'stop' | 'restart', name: string) => Promise<void>;
  onLogs: (id: string, name: string, token?: string) => void;
  onExec: (id: string, name: string, token?: string) => void;
  onFiles?: (id: string) => void;
  searchQuery: string;
}

export function ContainerCard({ container, history, onOp, onLogs, onExec, onFiles, searchQuery }: ContainerCardProps) {
  const { t } = useTranslation();
  const wrapperRef = useRef<HTMLDivElement>(null);
  const cardRef = useRef<HTMLDivElement>(null);

  const isUp = container.status && !container.status.toLowerCase().includes('exit');

  // Mouse 3D hover-tilt effect
  useEffect(() => {
    const wrapper = wrapperRef.current;
    const card = cardRef.current;
    if (!wrapper || !card) return;

    const getDefaultShadow = () => {
      const dark = document.documentElement.getAttribute('data-theme') === 'dark';
      return dark
        ? '0 10px 30px rgba(0,0,0,0.3)'
        : '0 8px 25px rgba(0,0,0,0.10), 0 2px 8px rgba(0,0,0,0.06)';
    };

    // Set initial shadow
    card.style.boxShadow = getDefaultShadow();

    const handleMouseMove = (e: MouseEvent) => {
      const rect = wrapper.getBoundingClientRect();
      const x = e.clientX - rect.left;
      const y = e.clientY - rect.top;

      card.style.setProperty('--mouse-x', `${x}px`);
      card.style.setProperty('--mouse-y', `${y}px`);

      const rotX = -((y / rect.height) - 0.5) * 8;
      const rotY = ((x / rect.width) - 0.5) * 8;

      card.style.transform = `rotateX(${rotX}deg) rotateY(${rotY}deg) translateY(-2px)`;
      const glowColor = isUp ? 'rgba(0, 255, 170, 0.08)' : 'rgba(255, 0, 85, 0.08)';
      const dark = document.documentElement.getAttribute('data-theme') === 'dark';
      card.style.boxShadow = dark
        ? `0 15px 35px rgba(0,0,0,0.5), 0 0 15px ${glowColor}`
        : `0 15px 35px rgba(0,0,0,0.15), 0 0 20px ${glowColor}`;
    };

    const handleMouseLeave = () => {
      card.style.transform = 'rotateX(0deg) rotateY(0deg) translateY(0)';
      card.style.boxShadow = getDefaultShadow();
    };

    wrapper.addEventListener('mousemove', handleMouseMove);
    wrapper.addEventListener('mouseleave', handleMouseLeave);
    return () => {
      wrapper.removeEventListener('mousemove', handleMouseMove);
      wrapper.removeEventListener('mouseleave', handleMouseLeave);
    };
  }, [isUp]);

  // Clean values
  const cpuPercent = parseFloat(container.cpu) || 0;
  const ramPercent = getMemoryPercent(container.memory, container.memorypercent);

  // Choose fill color based on usage percent
  const getBarColorClass = (val: number, isRam = false) => {
    if (val > 80) return 'bg-danger';
    if (val > 50) return 'bg-warning';
    return isRam ? 'bg-accent-pink' : 'bg-accent-cyan';
  };

  return (
    <div ref={wrapperRef} className="card-wrapper">
      <div
        ref={cardRef}
        className="tilt-card glass-card rounded-[24px] p-7 shadow-lg flex flex-col justify-start"
      >
        {/* Header */}
        <div className="flex justify-between items-start gap-4 mb-6 z-10 relative">
          <div className="text-left">
            <h3 className="text-lg font-bold text-text break-all">
              <HighlightedText text={container.name} query={searchQuery} />
            </h3>
            <div className="text-[11px] text-text-dim font-mono mt-1">
              <HighlightedText text={container.id} query={searchQuery} />
            </div>
          </div>
          <div className="flex flex-col items-end gap-2 shrink-0">
            {container.healthscore !== undefined && container.healthstatus && (
              <div className={`flex items-center gap-1.5 px-2.5 py-1 rounded-lg border font-extrabold text-[11px] tracking-wide ${
                container.healthstatus === 'healthy'
                  ? 'bg-success/10 text-success border-success/20'
                  : container.healthstatus === 'warning'
                    ? 'bg-warning/10 text-warning border-warning/20'
                    : 'bg-danger/10 text-danger border-danger/20'
              }`}>
                <HeartPulse className="w-3 h-3" />
                <span className="tabular-nums">{container.healthscore}</span>
                <span className="text-[9px] opacity-80">
                  {container.healthstatus === 'healthy' ? t('container.healthHealthy') :
                   container.healthstatus === 'warning' ? t('container.healthWarning') : t('container.healthDanger')}
                </span>
              </div>
            )}
            <span className={`text-[9px] font-extrabold px-2 py-0.5 rounded-md border text-center tracking-wide ${
              isUp
                ? 'bg-success/5 text-success border-success/15'
                : 'bg-danger/5 text-danger border-danger/15'
            }`}>
              {isUp ? t('container.statusRunning') : t('container.statusStopped')}
            </span>
          </div>
        </div>

        {/* Metrics Area */}
        <div className="flex flex-col gap-4 mb-6 z-10 relative">
          {/* CPU Row */}
          <div className="flex items-center gap-4">
            <div className="flex flex-col w-[80px] shrink-0 text-left">
              <span className="text-[9px] font-bold text-text-dim tracking-wide">{t('container.cpuLoad')}</span>
              <span className="text-[13px] font-extrabold mt-0.5 tabular-nums text-text">{container.cpu}</span>
            </div>
            <div className="flex items-center grow gap-3.5">
              <div className="h-[5px] bg-surface-3 rounded-full grow overflow-hidden relative">
                <div
                  className={`h-full rounded-full transition-all duration-500 ${getBarColorClass(cpuPercent)}`}
                  style={{ width: `${cpuPercent}%` }}
                />
              </div>
              <div className="w-[90px] h-[18px] shrink-0 opacity-85">
                <Sparkline data={history?.cpu || [0,0]} color="var(--color-accent-cyan)" />
              </div>
            </div>
          </div>

          {/* Memory Row */}
          <div className="flex items-center gap-4">
            <div className="flex flex-col w-[80px] shrink-0 text-left">
              <span className="text-[9px] font-bold text-text-dim tracking-wide">{t('container.ramUsage')}</span>
              <span className="text-[13px] font-extrabold mt-0.5 tabular-nums text-text">{container.memory}</span>
            </div>
            <div className="flex items-center grow gap-3.5">
              <div className="h-[5px] bg-surface-3 rounded-full grow overflow-hidden relative">
                <div
                  className={`h-full rounded-full transition-all duration-500 ${getBarColorClass(ramPercent, true)}`}
                  style={{ width: `${ramPercent}%` }}
                />
              </div>
              <div className="w-[90px] h-[18px] shrink-0 opacity-85">
                <Sparkline data={history?.ram || [0,0]} color="var(--color-accent-pink)" />
              </div>
            </div>
          </div>
        </div>

        {/* Port Mappings Visualizer */}
        {container.ports && container.ports.length > 0 && (
          <div className="flex flex-wrap gap-1.5 mb-6 z-10 relative text-left">
            <span className="text-[9px] font-bold text-text-dim tracking-wide w-full mb-1">
              {t('container.ports')}
            </span>
            {container.ports.map((pm, idx) => {
              const hasMapping = pm.public_port !== undefined;
              const displayText = hasMapping
                ? `${pm.public_port} → ${pm.private_port}/${pm.type}`
                : `${pm.private_port}/${pm.type}`;

              if (hasMapping) {
                const targetHost = pm.ip === '0.0.0.0' || pm.ip === '::' ? 'localhost' : (pm.ip || 'localhost');
                const href = `http://${targetHost}:${pm.public_port}`;
                return (
                  <a
                    key={idx}
                    href={href}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-md bg-accent-cyan/10 hover:bg-accent-cyan/20 border border-accent-cyan/20 hover:border-accent-cyan/40 text-[10px] text-accent-cyan font-mono font-bold transition-all cursor-pointer"
                    title={`Open http://${targetHost}:${pm.public_port} in browser`}
                  >
                    <span>{displayText}</span>
                  </a>
                );
              }

              return (
                <span
                  key={idx}
                  className="inline-flex items-center px-2.5 py-0.5 rounded-md bg-surface-2 border border-border-light text-[10px] text-text-dim font-mono font-semibold"
                >
                  {displayText}
                </span>
              );
            })}
          </div>
        )}

        {/* Footer Metrics */}
        <div className="flex justify-between pt-4 border-t border-border-subtle text-[11px] text-text-dim z-10 relative">
          <span className="flex items-center gap-1">
            <ArrowDownUp className="w-3 h-3 text-text-dim" />
            <span>{container.network || '0 B / 0 B'}</span>
          </span>
          <span className="flex items-center gap-1">
            <HardDrive className="w-3 h-3 text-text-dim" />
            <span>{container.blkio || '0 B / 0 B'}</span>
          </span>
        </div>

        {/* Action Controls */}
        <div className="card-actions">
          {isUp ? (
            <>
              <button
                onClick={() => onOp(container.fullid, 'stop', container.name)}
                className="action-btn btn-stop"
              >
                <Square className="w-3 h-3" />
                {t('container.btnStop')}
              </button>
              <button
                onClick={() => onOp(container.fullid, 'restart', container.name)}
                className="action-btn btn-restart"
              >
                <RefreshCw className="w-3 h-3" />
                {t('container.btnRestart')}
              </button>
            </>
          ) : (
            <button
              onClick={() => onOp(container.fullid, 'start', container.name)}
              className="action-btn btn-start"
            >
              <Play className="w-3 h-3" />
              {t('container.btnStart')}
            </button>
          )}
          {isUp && (
            <button
              onClick={() => onExec(container.fullid, container.name, undefined)}
              className="action-btn btn-exec"
            >
              <Command className="w-3 h-3" />
              {t('container.btnExec')}
            </button>
          )}
          {onFiles && (
            <button
              onClick={() => onFiles(container.fullid)}
              className="action-btn btn-files"
              title={t('files.title')}
            >
              <FolderOpen className="w-3 h-3" />
              {t('files.nav')}
            </button>
          )}
          <button
            onClick={() => onLogs(container.fullid, container.name, undefined)}
            className="action-btn btn-logs"
          >
            <Terminal className="w-3 h-3" />
            {t('container.btnLogs')}
          </button>
        </div>
      </div>
    </div>
  );
}
