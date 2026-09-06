import { useState, useEffect, useRef, useCallback } from 'react';
import { Send, Loader2, ShieldAlert, CheckCircle, XCircle, ChevronDown, ChevronRight, Bot, User, Wrench, AlertTriangle, RefreshCw } from 'lucide-react';
import { askDuty, confirmDutyWrite, fetchDutyConfig, fetchDutyTickets } from './dutyApi';
import type { ChatMessage, DutyConfig, PreviewResult, Ticket, ToolTrace } from './dutyTypes';
import { useTranslation } from '../../i18n';

interface DutyPanelProps {
  serverToken: string;
  onAuthRequired: () => void;
}

// Hoisted outside component to avoid remounting on every render (rerender-no-inline-components).
function TraceBlock({ trace, expanded, onToggle }: {
  trace: ToolTrace;
  expanded: boolean;
  onToggle: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="rounded-lg bg-surface-1/70 border border-border-subtle overflow-hidden">
      <button
        onClick={onToggle}
        aria-label={t('duty.traceAria', { tool: trace.tool })}
        className="w-full flex items-center gap-2 px-2.5 py-1.5 text-left hover:bg-surface-2/50 transition-colors cursor-pointer"
      >
        {expanded ? <ChevronDown className="w-3 h-3 text-text-dim" /> : <ChevronRight className="w-3 h-3 text-text-dim" />}
        <Wrench className="w-3 h-3 text-accent-cyan" />
        <span className="text-[10px] font-bold text-text-dim font-mono">{trace.tool}</span>
        {trace.output_excerpt && (
          <span className="text-[10px] text-text-dim/70 truncate flex-1">{trace.output_excerpt.slice(0, 80)}</span>
        )}
      </button>
      {expanded && (
        <div className="px-3 pb-2 space-y-1">
          <div className="text-[9px] font-bold text-text-dim uppercase tracking-wider">{t('duty.traceInput')}</div>
          <pre className="text-[10px] font-mono text-text-dim bg-surface-2/50 rounded p-2 overflow-x-auto">{trace.input}</pre>
          {trace.output_excerpt && (
            <>
              <div className="text-[9px] font-bold text-text-dim uppercase tracking-wider">{t('duty.traceOutput')}</div>
              <pre className="text-[10px] font-mono text-text/80 bg-surface-2/50 rounded p-2 overflow-x-auto max-h-32 overflow-y-auto whitespace-pre-wrap">{trace.output_excerpt}</pre>
            </>
          )}
        </div>
      )}
    </div>
  );
}

function WriteProposal({ proposal, confirming, onConfirm, error }: {
  proposal: PreviewResult;
  confirming: boolean;
  onConfirm: () => void;
  error: string;
}) {
  const { t } = useTranslation();
  return (
    <div className="rounded-xl bg-amber-500/10 border border-amber-500/30 p-3 space-y-2">
      <div className="flex items-center gap-2">
        <AlertTriangle className="w-4 h-4 text-amber-400" />
        <span className="text-[11px] font-extrabold text-amber-400 uppercase tracking-wide">
          {t('duty.proposal', { op: proposal.op, name: proposal.name })}
        </span>
      </div>
      <p className="text-[11px] text-text-dim">{proposal.impact}</p>
      <div className="flex items-center gap-2">
        <button
          onClick={onConfirm}
          disabled={confirming}
          aria-label={t('duty.proposeAria', { op: proposal.op, name: proposal.name })}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-amber-500/20 hover:bg-amber-500/30 border border-amber-500/40 text-amber-400 text-[11px] font-extrabold transition-all cursor-pointer disabled:opacity-50"
        >
          {confirming ? <Loader2 className="w-3 h-3 animate-spin" /> : <CheckCircle className="w-3 h-3" />}
          {t('duty.confirmOp', { op: proposal.op })}
        </button>
        <span className="text-[9px] text-text-dim">{t('duty.requiresAdminToken')}</span>
      </div>
      {error && (
        <p className="text-[10px] text-danger flex items-center gap-1">
          <XCircle className="w-3 h-3" /> {error}
        </p>
      )}
    </div>
  );
}

export function DutyPanel({ serverToken, onAuthRequired }: DutyPanelProps) {
  const { t } = useTranslation();
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [config, setConfig] = useState<DutyConfig>({ enabled: false });
  const [tickets, setTickets] = useState<Ticket[]>([]);
  const [showTickets, setShowTickets] = useState(false);
  const [expandedTraces, setExpandedTraces] = useState<Set<string>>(new Set());
  const [confirming, setConfirming] = useState<string | null>(null);
  const [confirmError, setConfirmError] = useState('');
  const endRef = useRef<HTMLDivElement>(null);

  // Parallel fetch on mount/token change (async-parallel).
  useEffect(() => {
    let cancelled = false;
    Promise.all([fetchDutyConfig(serverToken), fetchDutyTickets(serverToken)])
      .then(([cfg, tk]) => {
        if (cancelled) return;
        setConfig(cfg);
        setTickets(tk.tickets || []);
      })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [serverToken]);

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, isLoading]);

  // Uses functional setState to avoid `input` dependency (rerender-functional-setstate).
  const handleAsk = useCallback(async () => {
    let q = '';
    setInput(prev => { q = prev.trim(); return ''; });
    if (!q || isLoading) return;
    setConfirmError('');
    const userMsg: ChatMessage = {
      id: `u-${Date.now()}`,
      role: 'user',
      text: q,
      timestamp: Date.now(),
    };
    setMessages(prev => [...prev, userMsg]);
    setIsLoading(true);
    try {
      const res = await askDuty(q, serverToken);
      const aiMsg: ChatMessage = {
        id: `a-${Date.now()}`,
        role: 'assistant',
        text: res.answer,
        traces: res.tool_traces,
        proposedWrite: res.proposed_write,
        ticketId: res.ticket_id,
        timestamp: Date.now(),
      };
      setMessages(prev => [...prev, aiMsg]);
      fetchDutyTickets(serverToken).then(r => setTickets(r.tickets || []));
    } catch (err: any) {
      if (err.message?.includes('401') || err.message?.includes('Unauthorized')) {
        onAuthRequired();
      }
setMessages(prev => [...prev, {
        id: `e-${Date.now()}`,
        role: 'assistant',
        text: `${t('duty.errorPrefix')} ${err.message || t('duty.errorFallback')}`,
        timestamp: Date.now(),
      }]);
    } finally {
      setIsLoading(false);
    }
  }, [isLoading, serverToken, onAuthRequired]);

  const handleConfirm = useCallback(async (proposed: PreviewResult, ticketId?: number) => {
    if (!ticketId) return;
    setConfirming(`${ticketId}-${proposed.id}`);
    setConfirmError('');
    try {
      await confirmDutyWrite(ticketId, proposed.op, proposed.id, serverToken);
      setMessages(prev => [...prev, {
        id: `c-${Date.now()}`,
        role: 'assistant',
        text: t('duty.confirmedMsg', { op: proposed.op, name: proposed.name, id: proposed.id }),
        timestamp: Date.now(),
      }]);
      fetchDutyTickets(serverToken).then(r => setTickets(r.tickets || []));
    } catch (err: any) {
      if (err.message?.includes('401') || err.message?.includes('Unauthorized')) {
        onAuthRequired();
      }
      setConfirmError(err.message || t('duty.confirmFailed'));
    } finally {
      setConfirming(null);
    }
  }, [serverToken, onAuthRequired]);

  const toggleTrace = useCallback((id: string) => {
    setExpandedTraces(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  }, []);

  if (!config.enabled) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-text-dim py-20 gap-3">
        <Bot className="w-10 h-10 opacity-30" />
        <p className="font-bold text-sm">{t('duty.disabledTitle')}</p>
        <p className="text-xs max-w-sm text-center">
          {t('duty.enableHintPre')} <code className="bg-surface-2 px-1.5 py-0.5 rounded">agent: enabled: true</code> {t('duty.enableHintMid')} <code className="bg-surface-2 px-1.5 py-0.5 rounded">DOCKERVIEW_AGENT_ENABLED=1</code>.
        </p>
      </div>
    );
  }

  const suggestedQuestions = [
    t('duty.suggest1'),
    t('duty.suggest2'),
    t('duty.suggest3'),
    t('duty.suggest4'),
  ];

  return (
    <div className="flex flex-col h-full gap-4">
      {/* Header */}
      <div className="flex items-center justify-between shrink-0">
        <div className="flex items-center gap-3">
          <div className="w-9 h-9 rounded-xl bg-accent-cyan/15 border border-accent-cyan/30 flex items-center justify-center">
            <Bot className="w-4 h-4 text-accent-cyan" />
          </div>
          <div>
            <h2 className="text-sm font-extrabold text-text">{t('duty.title')}</h2>
            <div className="flex items-center gap-2 text-[10px] font-bold">
              <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full ${
                config.mode === 'fake'
                  ? 'bg-amber-500/15 text-amber-400 border border-amber-500/30'
                  : 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/30'
              }`}>
                <span className={`w-1.5 h-1.5 rounded-full ${config.mode === 'fake' ? 'bg-amber-400' : 'bg-emerald-400'} animate-pulse`} />
                {config.mode === 'fake' ? t('duty.modeDrill') : t('duty.modeLive')}
              </span>
              <span className="text-text-dim">{config.model}</span>
            </div>
          </div>
        </div>
        <button
          onClick={() => setShowTickets(prev => !prev)}
          aria-label={t('duty.ticketsAria')}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-surface-2 hover:bg-surface-3 border border-border-subtle text-text-dim hover:text-text text-[11px] font-bold transition-all cursor-pointer"
        >
          <RefreshCw className="w-3 h-3" />
          {t('duty.tickets', { count: tickets.length })}
        </button>
      </div>

      {/* Tickets drawer */}
      {showTickets ? (
        <div className="shrink-0 max-h-48 overflow-y-auto bg-surface-1/50 border border-border-subtle rounded-2xl p-3 space-y-2">
          {tickets.length === 0 ? <p className="text-xs text-text-dim italic p-2">{t('duty.noTickets')}</p> : null}
          {tickets.map(t => (
            <div key={t.id} className="flex items-start gap-2 p-2 rounded-xl bg-surface-2/50 border border-border-light">
              <div className="mt-0.5">
                {t.write_confirmed
                  ? <CheckCircle className="w-3.5 h-3.5 text-success" />
                  : <ShieldAlert className="w-3.5 h-3.5 text-amber-400" />}
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-[11px] font-bold text-text truncate">{t.question}</p>
                <p className="text-[10px] text-text-dim truncate">{t.conclusion?.slice(0, 120)}</p>
                <div className="flex gap-2 mt-1 text-[9px] text-text-dim">
                  <span>#{t.id}</span>
                  <span>{new Date(t.time).toLocaleString()}</span>
                  {t.write_action ? <span className="text-amber-400 font-bold">{t.write_action}</span> : null}
                </div>
              </div>
            </div>
          ))}
        </div>
      ) : null}

      {/* Messages */}
      <div className="flex-1 overflow-y-auto space-y-4 pr-1" role="log" aria-label={t('duty.chatAria')}>
        {messages.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-full text-text-dim gap-4 py-10">
            <Bot className="w-12 h-12 opacity-20" />
            <p className="text-sm font-bold text-center">{t('duty.askHint')}</p>
            <div className="flex flex-wrap gap-2 justify-center max-w-md">
              {suggestedQuestions.map(q => (
                <button
                  key={q}
                  onClick={() => setInput(q)}
                  className="px-3 py-1.5 rounded-xl bg-surface-2 hover:bg-surface-3 border border-border-subtle text-text-dim hover:text-text text-[11px] font-semibold transition-all cursor-pointer"
                >
                  {q}
                </button>
              ))}
            </div>
          </div>
        ) : null}

        {messages.map(msg => (
          <div key={msg.id} className={`flex gap-3 ${msg.role === 'user' ? 'flex-row-reverse' : ''}`}>
            <div className={`w-7 h-7 rounded-lg shrink-0 flex items-center justify-center ${
              msg.role === 'user'
                ? 'bg-accent-cyan/20 text-accent-cyan'
                : 'bg-surface-3 text-text-dim'
            }`}>
              {msg.role === 'user' ? <User className="w-3.5 h-3.5" /> : <Bot className="w-3.5 h-3.5" />}
            </div>
            <div className={`max-w-[80%] space-y-2 ${msg.role === 'user' ? 'items-end' : ''}`}>
              <div className={`rounded-2xl px-4 py-2.5 text-[13px] leading-relaxed ${
                msg.role === 'user'
                  ? 'bg-accent-cyan/15 text-text rounded-tr-sm'
                  : 'bg-surface-2 text-text rounded-tl-sm border border-border-light'
              }`}>
                <p className="whitespace-pre-wrap">{msg.text}</p>
              </div>

              {/* Tool traces */}
              {msg.traces && msg.traces.length > 0 ? (
                <div className="space-y-1">
                  {msg.traces.map((trace, i) => {
                    const traceId = `${msg.id}-${i}`;
                    return (
                      <TraceBlock
                        key={i}
                        trace={trace}
                        expanded={expandedTraces.has(traceId)}
                        onToggle={() => toggleTrace(traceId)}
                      />
                    );
                  })}
                </div>
              ) : null}

              {/* Proposed write */}
              {msg.proposedWrite ? (
                <WriteProposal
                  proposal={msg.proposedWrite}
                  confirming={confirming === `${msg.ticketId}-${msg.proposedWrite.id}`}
                  onConfirm={() => handleConfirm(msg.proposedWrite!, msg.ticketId)}
                  error={confirming === null ? confirmError : ''}
                />
              ) : null}
            </div>
          </div>
        ))}

        {isLoading ? (
          <div className="flex gap-3">
            <div className="w-7 h-7 rounded-lg bg-surface-3 flex items-center justify-center">
              <Bot className="w-3.5 h-3.5 text-text-dim" />
            </div>
            <div className="rounded-2xl bg-surface-2 border border-border-light px-4 py-3 flex items-center gap-2">
              <Loader2 className="w-4 h-4 animate-spin text-accent-cyan" />
              <span className="text-[12px] text-text-dim font-semibold">{t('duty.thinking')}</span>
            </div>
          </div>
        ) : null}
        <div ref={endRef} />
      </div>

      {/* Input */}
      <div className="shrink-0 flex gap-2">
        <label htmlFor="duty-chat-input" className="sr-only">{t('duty.inputAria')}</label>
        <input
          id="duty-chat-input"
          type="text"
          value={input}
          onChange={e => setInput(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && handleAsk()}
          placeholder={t('duty.inputPlaceholder')}
          disabled={isLoading}
          className="flex-1 bg-surface-2 hover:bg-surface-4 focus:bg-surface-4 disabled:opacity-50 border border-border-light focus:border-accent-cyan/40 rounded-xl py-2.5 px-4 text-text text-[13px] font-semibold transition-all focus:outline-none"
        />
        <button
          onClick={handleAsk}
          disabled={isLoading || !input.trim()}
          aria-label={t('duty.sendAria')}
          className="flex items-center gap-1.5 px-5 py-2.5 bg-accent-cyan hover:bg-accent-cyan/90 disabled:opacity-40 disabled:hover:bg-accent-cyan text-black font-extrabold text-[12px] rounded-xl transition-all cursor-pointer shrink-0"
        >
          {isLoading ? <Loader2 className="w-4 h-4 animate-spin" /> : <Send className="w-4 h-4" />}
          {t('duty.ask')}
        </button>
      </div>
    </div>
  );
}
