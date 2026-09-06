import { useState, useEffect, useCallback, useRef } from 'react';
import { RefreshCcw, Download, RefreshCw, CheckCircle, AlertCircle, ClipboardList, LayoutGrid, Archive, FolderOpen, Bot } from 'lucide-react';
import type { ToastMessage } from './types';
import { formatBytes, basePath } from './utils';
import { useTelemetry } from './hooks/useTelemetry';
import { useTranslation } from './i18n';
import { useTheme } from './hooks/useTheme';
import { Header } from './components/Header';
import { SummaryDashboard } from './components/SummaryDashboard';
import { ContainerCard } from './components/ContainerCard';
import { AuthModal } from './components/AuthModal';
import { LogsModal } from './components/LogsModal';
import { ExecModal } from './components/ExecModal';
import { PruneModal } from './components/PruneModal';
import { AuditPanel } from './components/AuditPanel';
import { BackupPanel } from './components/BackupPanel';
import { FilesPanel } from './components/FilesPanel';
import { DutyPanel } from './components/duty/DutyPanel';

interface VersionInfo {
  current_version: string;
  latest_version: string;
  update_available: boolean;
  install_method: string;
  commit: string;
  build_date: string;
}

type UpgradeStatus = 'idle' | 'upgrading' | 'success' | 'error';

export default function App() {
  const { t } = useTranslation();
  const { theme, toggleTheme } = useTheme();

  // Auth state
  const [serverToken, setServerToken] = useState<string>(() => {
    const urlParams = new URLSearchParams(window.location.search);
    const tokenFromUrl = urlParams.get('token');
    if (tokenFromUrl) {
      localStorage.setItem('dockerview_token', tokenFromUrl);
      const newUrl = window.location.pathname;
      window.history.replaceState({}, document.title, newUrl);
      return tokenFromUrl;
    }
    return localStorage.getItem('dockerview_token') || '';
  });
  const [showAuthModal, setShowAuthModal] = useState<boolean>(false);
  const [authError, setAuthError] = useState<boolean>(false);
  const [showPruneModal, setShowPruneModal] = useState<boolean>(false);

  // Pending action stored as data (not a function closure) to avoid stale closure bugs.
  // The function-closure approach captured an old performOp/handleOpenLogs with an empty
  // serverToken, causing a second auth dialog to appear after the first token input.
  type PendingActionType =
    | { kind: 'op'; containerId: string; op: 'start' | 'stop' | 'restart'; containerName: string }
    | { kind: 'logs'; containerId: string; containerName: string }
    | { kind: 'exec'; containerId: string; containerName: string }
    | { kind: 'upgrade' }
    | { kind: 'prune' }
    | { kind: 'audit' }
    | { kind: 'backups' }
    | { kind: 'files'; containerId?: string };
  const [pendingAction, setPendingAction] = useState<PendingActionType | null>(null);

  // Active top-level view: containers, audit, backups or files
  const [view, setView] = useState<'containers' | 'audit' | 'backups' | 'files' | 'duty'>('containers');
  const [filesContainerId, setFilesContainerId] = useState<string | undefined>(undefined);

  // Modals & Toasts
  const [toasts, setToasts] = useState<ToastMessage[]>([]);
  const [logsContainer, setLogsContainer] = useState<{ id: string; name: string } | null>(null);
  const [execContainer, setExecContainer] = useState<{ id: string; name: string } | null>(null);
  const [showAllOffline, setShowAllOffline] = useState<boolean>(false);

  // Version & Upgrade state
  const [versionInfo, setVersionInfo] = useState<VersionInfo | null>(null);
  const [upgradeStatus, setUpgradeStatus] = useState<UpgradeStatus>('idle');
  const [upgradeMessage, setUpgradeMessage] = useState<string>('');
  const upgradeStatusRef = useRef<UpgradeStatus>('idle');
  const eventSourceRef = useRef<EventSource | null>(null);
  const serverTokenRef = useRef(serverToken);
  serverTokenRef.current = serverToken;

  const setUpgradeStatusSync = (status: UpgradeStatus) => {
    upgradeStatusRef.current = status;
    setUpgradeStatus(status);
  };

  // Custom hook for telemetry
  const {
    containers,
    filteredContainers,
    runningCount,
    stoppedCount,
    healthyCount,
    warningCount,
    dangerousCount,
    avgCpu,
    peakMemory,
    lastUpdate,
    searchQuery,
    setSearchQuery,
    sortKey,
    setSortKey,
    filterKey,
    setFilterKey,
    historyData,
    isRunning
  } = useTelemetry(serverToken);

  const showToast = useCallback((message: string, type: 'info' | 'success' | 'error' = 'info') => {
    const id = Date.now() + Math.random();
    setToasts(prev => [...prev, { id, message, type }]);
    setTimeout(() => {
      setToasts(prev => prev.filter(t => t.id !== id));
    }, 4000);
  }, []);

  // Fetch version info
  const fetchVersionInfo = useCallback(() => {
    fetch(`${basePath}api/version`)
      .then(resp => resp.ok ? resp.json() : null)
      .then((data: VersionInfo | null) => {
        if (data) {
          setVersionInfo(prev => {
            if (upgradeStatusRef.current === 'success') return prev;
            return data;
          });
        }
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    fetchVersionInfo();
    const interval = setInterval(fetchVersionInfo, 30 * 60 * 1000);
    return () => clearInterval(interval);
  }, [fetchVersionInfo]);

  // Handle upgrade
  const handleUpgrade = useCallback((token?: string) => {
    const authToken = token ?? serverTokenRef.current;
    if (!authToken) {
      setPendingAction({ kind: 'upgrade' });
      setAuthError(false);
      setShowAuthModal(true);
      return;
    }

    if (upgradeStatusRef.current === 'upgrading') return;

    // Close any existing EventSource
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }

    setUpgradeStatusSync('upgrading');
    const startMsg = t('app.upgradeToastStarting');
    setUpgradeMessage(startMsg);
    showToast(startMsg, 'info');

    const url = `${basePath}api/upgrade?token=${encodeURIComponent(authToken)}`;
    const es = new EventSource(url);
    eventSourceRef.current = es;

    es.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);

        if (data.status === 'success') {
          setUpgradeStatusSync('success');
          const msg = t('app.upgradeToastSuccess');
          setUpgradeMessage(msg);
          showToast(msg, 'success');
          es.close();
          eventSourceRef.current = null;
        } else if (data.status === 'error') {
          setUpgradeStatusSync('error');
          const msg = t('app.upgradeToastFailed', { error: data.message });
          setUpgradeMessage(msg);
          showToast(msg, 'error');
          es.close();
          eventSourceRef.current = null;
        } else if (data.status === 'downloading') {
          const msg = t('app.upgradeToastDownloading');
          setUpgradeMessage(msg);
          showToast(msg, 'info');
        } else if (data.status === 'applying') {
          const msg = t('app.upgradeToastApplying');
          setUpgradeMessage(msg);
          showToast(msg, 'info');
        }
      } catch {
        // Ignore parse errors
      }
    };

    es.onerror = () => {
      if (upgradeStatusRef.current !== 'success' && upgradeStatusRef.current !== 'error') {
        setUpgradeStatusSync('error');
        const msg = t('app.upgradeToastConnectionLost');
        setUpgradeMessage(msg);
        showToast(msg, 'error');
      }
      es.close();
      eventSourceRef.current = null;
    };
  }, [showToast, t]);

  // Handle operation action (Start/Stop/Restart)
  // Accepts an optional `token` param so callers can pass the latest token
  // without relying on closure state (avoids stale-closure double-auth bug).
  const performOp = async (containerId: string, op: 'start' | 'stop' | 'restart', name: string, token?: string) => {
    const authToken = token ?? serverToken;
    if (!containerId) {
      showToast(t('app.toastContainerIdMissing'), 'error');
      return;
    }
    if (!authToken) {
      setPendingAction({ kind: 'op', containerId, op, containerName: name });
      setAuthError(false);
      setShowAuthModal(true);
      return;
    }

    const opVerb = t(`app.op${op.charAt(0).toUpperCase() + op.slice(1)}`);
    showToast(t('app.toastOpStarting', { op: opVerb, name }), 'info');

    try {
      const response = await fetch(`${basePath}api/container/op?id=${containerId}&op=${op}&token=${authToken}`, {
        method: 'POST'
      });

      if (response.ok) {
        showToast(t('app.toastOpSuccess', { op: opVerb, name }), 'success');
      } else if (response.status === 401) {
        showToast(t('app.toastAuthFailed'), 'error');
        localStorage.removeItem('dockerview_token');
        setServerToken('');
        setPendingAction({ kind: 'op', containerId, op, containerName: name });
        setAuthError(true);
        setShowAuthModal(true);
      } else {
        const errMsg = await response.text();
        showToast(t('app.toastError', { error: errMsg }), 'error');
      }
    } catch (err: any) {
      showToast(t('app.toastConnectionError', { error: err.message }), 'error');
    }
  };

  // Open Log Modal helper
  // Accepts an optional `token` param to avoid stale-closure issues.
  const handleOpenLogs = (id: string, name: string, token?: string) => {
    const authToken = token ?? serverToken;
    if (!authToken) {
      setPendingAction({ kind: 'logs', containerId: id, containerName: name });
      setAuthError(false);
      setShowAuthModal(true);
      return;
    }
    setLogsContainer({ id, name });
  };

  // Open Exec Modal helper
  // Accepts an optional `token` param to avoid stale-closure issues.
  const handleOpenExec = (id: string, name: string, token?: string) => {
    const authToken = token ?? serverToken;
    if (!authToken) {
      setPendingAction({ kind: 'exec', containerId: id, containerName: name });
      setAuthError(false);
      setShowAuthModal(true);
      return;
    }
    setExecContainer({ id, name });
  };

  // Open the cleanup (prune) modal. The list/dry-run are guest-readable; if the
  // user later tries to delete without a token, PruneModal triggers auth.
  const handleOpenPrune = useCallback(() => {
    setShowPruneModal(true);
  }, []);

  const handleVerifyToken = (token: string) => {
    setServerToken(token);
    localStorage.setItem('dockerview_token', token);
    setShowAuthModal(false);
    showToast(t('app.toastTokenSaved'), 'success');
    if (pendingAction) {
      // Execute the pending action using the new token directly.
      // We pass the token explicitly instead of relying on state / closure,
      // which ensures the action uses the correct token on the first try.
      const action = pendingAction;
      setPendingAction(null);
      if (action.kind === 'op') {
        performOp(action.containerId, action.op, action.containerName, token);
      } else if (action.kind === 'logs') {
        handleOpenLogs(action.containerId, action.containerName, token);
      } else if (action.kind === 'exec') {
        handleOpenExec(action.containerId, action.containerName, token);
      } else if (action.kind === 'upgrade') {
        handleUpgrade(token);
      } else if (action.kind === 'prune') {
        setShowPruneModal(true);
      } else if (action.kind === 'audit') {
        setView('audit');
      } else if (action.kind === 'backups') {
        setView('backups');
      } else if (action.kind === 'files') {
        setFilesContainerId(action.containerId);
        setView('files');
      }
    }
  };

  // When opening audit without a token, prompt for auth.
  const handleAuditNav = useCallback(() => {
    if (!serverToken) {
      setPendingAction({ kind: 'audit' });
      setAuthError(false);
      setShowAuthModal(true);
      return;
    }
    setView('audit');
  }, [serverToken]);

  // When opening backups without a token, prompt for auth. The backup panel
  // is token-gated end-to-end (list/download all require a valid token).
  const handleBackupsNav = useCallback(() => {
    if (!serverToken) {
      setPendingAction({ kind: 'backups' });
      setAuthError(false);
      setShowAuthModal(true);
      return;
    }
    setView('backups');
  }, [serverToken]);

  // Memoized: BackupPanel effects depend on this callback, and App re-renders
  // on every SSE tick — an inline arrow would retrigger the panel's list
  // fetch once per second (flicker + request/audit flood).
  const handleBackupAuthRequired = useCallback(() => {
    localStorage.removeItem('dockerview_token');
    setServerToken('');
    setPendingAction({ kind: 'backups' });
    setAuthError(true);
    setShowAuthModal(true);
  }, []);

  // Files view is token-gated like backups. Opening from a container card
  // preselects that container.
  const handleFilesNav = useCallback((containerId?: string) => {
    if (!serverToken) {
      setPendingAction({ kind: 'files', containerId });
      setAuthError(false);
      setShowAuthModal(true);
      return;
    }
    setFilesContainerId(containerId);
    setView('files');
  }, [serverToken]);

  const handleFilesAuthRequired = useCallback(() => {
    localStorage.removeItem('dockerview_token');
    setServerToken('');
    setPendingAction({ kind: 'files' });
    setAuthError(true);
    setShowAuthModal(true);
  }, []);

  return (
    <div className="relative min-h-screen">
      <div className="mesh" />
      <div className="max-w-[1600px] mx-auto px-5 py-[50px] md:px-[30px]">

        {/* Floating Controller Panel */}
        <Header
          totalCount={containers.length}
          runningCount={runningCount}
          stoppedCount={stoppedCount}
          searchQuery={searchQuery}
          setSearchQuery={setSearchQuery}
          sortKey={sortKey}
          setSortKey={setSortKey}
          filterKey={filterKey}
          setFilterKey={setFilterKey}
          theme={theme}
          onToggleTheme={toggleTheme}
          onCleanup={handleOpenPrune}
        />

        {/* View toggle (containers | audit) */}
        <div className="flex items-center gap-2 mb-5 mt-[18px]">
          <button
            onClick={() => setView('containers')}
            className={`flex items-center gap-2 px-4 py-2 rounded-xl font-bold text-[12px] tracking-wider uppercase border transition-all ${view === 'containers' ? 'bg-accent-cyan/15 border-accent-cyan/50 text-accent-cyan' : 'bg-surface-1 border-border-light text-text-dim hover:text-text'}`}
          >
            <LayoutGrid className="w-3.5 h-3.5" />{t('app.navContainers')}
          </button>
          <button
            onClick={handleAuditNav}
            className={`flex items-center gap-2 px-4 py-2 rounded-xl font-bold text-[12px] tracking-wider uppercase border transition-all ${view === 'audit' ? 'bg-accent-cyan/15 border-accent-cyan/50 text-accent-cyan' : 'bg-surface-1 border-border-light text-text-dim hover:text-text'}`}
            data-testid="nav-audit"
          >
            <ClipboardList className="w-3.5 h-3.5" />{t('app.navAudit')}
          </button>
          <button
            onClick={handleBackupsNav}
            className={`flex items-center gap-2 px-4 py-2 rounded-xl font-bold text-[12px] tracking-wider uppercase border transition-all ${view === 'backups' ? 'bg-accent-cyan/15 border-accent-cyan/50 text-accent-cyan' : 'bg-surface-1 border-border-light text-text-dim hover:text-text'}`}
            data-testid="nav-backups"
          >
            <Archive className="w-3.5 h-3.5" />{t('app.navBackups')}
          </button>
          <button
            onClick={() => handleFilesNav(undefined)}
            className={`flex items-center gap-2 px-4 py-2 rounded-xl font-bold text-[12px] tracking-wider uppercase border transition-all ${view === 'files' ? 'bg-accent-cyan/15 border-accent-cyan/50 text-accent-cyan' : 'bg-surface-1 border-border-light text-text-dim hover:text-text'}`}
            data-testid="nav-files"
          >
            <FolderOpen className="w-3.5 h-3.5" />{t('files.nav')}
          </button>
          <button
            onClick={() => setView('duty')}
            className={`flex items-center gap-2 px-4 py-2 rounded-xl font-bold text-[12px] tracking-wider uppercase border transition-all ${view === 'duty' ? 'bg-accent-cyan/15 border-accent-cyan/50 text-accent-cyan' : 'bg-surface-1 border-border-light text-text-dim hover:text-text'}`}
            data-testid="nav-duty"
          >
            <Bot className="w-3.5 h-3.5" />{t('duty.nav')}
          </button>
        </div>

        {view === 'audit' ? (
          <AuditPanel
            token={serverToken}
            onAuthRequired={() => {
              localStorage.removeItem('dockerview_token');
              setServerToken('');
              setPendingAction({ kind: 'audit' });
              setAuthError(true);
              setShowAuthModal(true);
            }}
          />
         ) : view === 'backups' ? (
          <BackupPanel
            token={serverToken}
            onAuthRequired={handleBackupAuthRequired}
            onToast={showToast}
          />
         ) : view === 'files' ? (
          <FilesPanel
            token={serverToken}
            containers={containers}
            initialContainerId={filesContainerId}
            onAuthRequired={handleFilesAuthRequired}
            onToast={showToast}
          />
         ) : view === 'duty' ? (
          <DutyPanel
            serverToken={serverToken}
            onAuthRequired={() => {
              localStorage.removeItem('dockerview_token');
              setServerToken('');
              setAuthError(true);
              setShowAuthModal(true);
            }}
          />
         ) : (
           <>
             {/* Aggregate Stats Dashboard */}
             <SummaryDashboard
               total={containers.length}
               active={runningCount}
               avgCpu={avgCpu}
               peakMemory={formatBytes(peakMemory)}
               healthyCount={healthyCount}
               warningCount={warningCount}
               dangerousCount={dangerousCount}
             />

             {/* Container Lists */}
        {filteredContainers.length > 0 ? (
          <div className="space-y-[50px]">
            {/* Active grid */}
            {filteredContainers.some(isRunning) && (
              <div>
                <div className="text-[12px] font-extrabold tracking-[2px] text-text-dim mb-6 flex items-center gap-3 after:content-[''] after:grow after:h-[1px] after:bg-surface-3">
                  {t('app.activeDeployments')}
                </div>
                <div className="grid-container">
                  {filteredContainers.filter(isRunning).map(c => (
                    <ContainerCard
                      key={c.id}
                      container={c}
                      history={historyData[c.id]}
                      onOp={performOp}
                      onLogs={handleOpenLogs}
                      onExec={handleOpenExec}
                      onFiles={(id) => handleFilesNav(id)}
                      searchQuery={searchQuery}
                    />
                  ))}
                </div>
              </div>
            )}

            {/* Offline grid */}
            {filteredContainers.some(c => !isRunning(c)) && (
              <div>
                <div className="text-[12px] font-extrabold tracking-[2px] text-text-dim mb-6 flex items-center gap-3 after:content-[''] after:grow after:h-[1px] after:bg-surface-3">
                  {t('app.offlineInstances')} ({filteredContainers.filter(c => !isRunning(c)).length})
                </div>
                <div className="grid-container">
                  {filteredContainers
                    .filter(c => !isRunning(c))
                    .slice(0, showAllOffline ? undefined : 6)
                    .map(c => (
                      <ContainerCard
                        key={c.id}
                        container={c}
                        history={historyData[c.id]}
                        onOp={performOp}
                        onLogs={handleOpenLogs}
                        onExec={handleOpenExec}
                        onFiles={(id) => handleFilesNav(id)}
                        searchQuery={searchQuery}
                      />
                    ))}
                </div>
                {filteredContainers.filter(c => !isRunning(c)).length > 6 && (
                  <div className="flex justify-center mt-8">
                    <button
                      onClick={() => setShowAllOffline(!showAllOffline)}
                      className="px-5 py-2.5 rounded-xl bg-surface-1 hover:bg-surface-4 border border-border-light hover:border-border-default text-text-dim hover:text-text font-bold text-[11px] tracking-wider uppercase transition-all cursor-pointer"
                    >
                      {showAllOffline ? t('app.showLess') : t('app.showAllOffline', { count: filteredContainers.filter(c => !isRunning(c)).length })}
                    </button>
                  </div>
                )}
              </div>
            )}
          </div>
         ) : (
           <div className="text-center py-[60px] text-text-dim font-semibold text-[14px]">
             {t('app.noContainers')}
           </div>
           )}
          </>
        )}

        {/* Footer */}
        <footer className="mt-[100px] pt-10 border-t border-card-border text-[11px] text-text-dim">
          <div className="flex flex-wrap justify-between items-center gap-5">
            <div className="flex items-center gap-3.5">
              <span>{t('app.footerCopyright')}</span>
              <span className="bg-surface-2 border border-border-light px-1.5 py-0.5 rounded font-mono font-bold text-accent-cyan">
                {versionInfo?.current_version
                  ? (versionInfo.current_version.toLowerCase().startsWith('v')
                      ? versionInfo.current_version
                      : `v${versionInfo.current_version}`)
                  : '...'}
              </span>
              {versionInfo?.update_available && upgradeStatus === 'idle' && (
                <button
                  onClick={() => handleUpgrade()}
                  className="flex items-center gap-1.5 px-2 py-0.5 bg-accent-cyan/10 hover:bg-accent-cyan/20 border border-accent-cyan/30 hover:border-accent-cyan/50 rounded font-mono font-bold text-[10px] text-accent-cyan cursor-pointer transition-all animate-pulse group"
                  title={t('header.upgradeAvailableTooltip', { version: versionInfo.latest_version })}
                >
                  <Download className="w-2.5 h-2.5 group-hover:translate-y-[-1px] transition-transform" />
                  <span>{t('header.upgradeAvailable', { version: versionInfo.latest_version })}</span>
                </button>
              )}
              {upgradeStatus === 'upgrading' && (
                <div className="flex items-center gap-1.5 px-2 py-0.5 bg-accent-cyan/10 border border-accent-cyan/30 rounded font-mono font-bold text-[10px] text-accent-cyan animate-pulse">
                  <RefreshCw className="w-2.5 h-2.5 animate-spin" />
                  <span>{t('header.upgradeUpgrading')}</span>
                </div>
              )}
              {upgradeStatus === 'success' && (
                <div className="flex items-center gap-1.5 px-2 py-0.5 bg-success/10 border border-success/30 rounded font-mono font-bold text-[10px] text-success" title={upgradeMessage}>
                  <CheckCircle className="w-2.5 h-2.5" />
                  <span>{t('app.footerUpgradeSuccess')}</span>
                </div>
              )}
              {upgradeStatus === 'error' && (
                <button
                  onClick={() => handleUpgrade()}
                  className="flex items-center gap-1.5 px-2 py-0.5 bg-danger/10 hover:bg-danger/20 border border-danger/30 rounded font-mono font-bold text-[10px] text-danger cursor-pointer transition-all"
                  title={upgradeMessage}
                >
                  <AlertCircle className="w-2.5 h-2.5" />
                  <span>{t('header.upgradeFailed')}</span>
                </button>
              )}
            </div>
            <div className="flex items-center gap-6 font-semibold">
              <div className="flex items-center gap-1.5">
                <span className="w-2 h-2 rounded-full bg-success live-pulse" />
                <span>{t('app.sseRunning')}</span>
              </div>
              <div className="flex items-center gap-1.5">
                <RefreshCcw className="w-2.5 h-2.5 opacity-70 animate-spin" style={{ animationDuration: '6s' }} />
                <span>{t('app.lastUpdated')} <span>{lastUpdate}</span></span>
              </div>
            </div>
            <div>
              <a
                href="https://github.com/zsuroy/dockerview-go"
                target="_blank"
                rel="noreferrer"
                className="flex items-center gap-1.5 text-text-dim hover:text-text bg-surface-1 hover:bg-surface-4 border border-border-subtle hover:border-border-default px-3.5 py-1.5 rounded-lg transition-all font-semibold text-[11px]"
              >
                <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M15 22v-4a4.8 4.8 0 0 0-1-3.5c3 0 6-2 6-5.5.08-1.25-.27-2.48-1-3.5.28-1.15.28-2.35 0-3.5 0 0-1 0-3 1.5-2.64-.5-5.36-.5-8 0C6 2 5 2 5 2c-.3 1.15-.3 2.35 0 3.5A5.403 5.403 0 0 0 4 9c0 3.5 3 5.5 6 5.5-.39.49-.68 1.05-.85 1.65-.17.6-.22 1.23-.15 1.85v4" />
                  <path d="M9 18c-4.51 2-5-2-7-2" />
                </svg>
                <span>{t('app.github')}</span>
              </a>
            </div>
          </div>
        </footer>
      </div>

      {/* Auth Token Prompt Modal */}
      {showAuthModal && (
        <AuthModal
          onVerify={handleVerifyToken}
          onClose={() => {
            setShowAuthModal(false);
            setPendingAction(null);
          }}
          hasError={authError}
        />
      )}

      {/* Logs Viewing Modal */}
      {logsContainer && (
        <LogsModal
          containerId={logsContainer.id}
          containerName={logsContainer.name}
          serverToken={serverToken}
          onClose={() => setLogsContainer(null)}
          onAuthRequired={(containerId, containerName) => {
            setLogsContainer(null);
            localStorage.removeItem('dockerview_token');
            setServerToken('');
            setPendingAction({ kind: 'logs', containerId, containerName });
            setAuthError(true);
            setShowAuthModal(true);
          }}
        />
      )}

      {/* Command Exec Modal */}
      {execContainer && (
        <ExecModal
          containerId={execContainer.id}
          containerName={execContainer.name}
          serverToken={serverToken}
          onClose={() => setExecContainer(null)}
          onAuthRequired={(containerId, containerName) => {
            setExecContainer(null);
            localStorage.removeItem('dockerview_token');
            setServerToken('');
            setPendingAction({ kind: 'exec', containerId, containerName });
            setAuthError(true);
            setShowAuthModal(true);
          }}
        />
      )}

      {/* Disk Cleanup (prune) Modal — list → dry-run → confirm → result */}
      <PruneModal
        open={showPruneModal}
        onOpenChange={setShowPruneModal}
        serverToken={serverToken}
        onToast={showToast}
        onAuthRequired={() => {
          // Close the prune modal before opening the auth modal to avoid two
          // stacked dialogs fighting for focus; the pending action reopens it
          // after a successful token verification.
          setShowPruneModal(false);
          localStorage.removeItem('dockerview_token');
          setServerToken('');
          setPendingAction({ kind: 'prune' });
          setAuthError(true);
          setShowAuthModal(true);
        }}
      />

      {/* Dynamic Toast Messages */}
      <div className="fixed bottom-[30px] right-[30px] flex flex-col gap-2.5 z-[2000]">
        {toasts.map(t => (
          <div
            key={t.id}
            className={`flex items-center gap-2.5 px-4.5 py-3 rounded-xl bg-modal-bg border border-surface-5 text-text font-semibold text-[13px] shadow-lg backdrop-blur-md animate-modal-in min-w-[260px] ${
              t.type === 'success' ? 'border-l-4 border-l-success' :
              t.type === 'error' ? 'border-l-4 border-l-danger' :
              'border-l-4 border-l-accent-cyan'
            }`}
          >
            {t.message}
          </div>
        ))}
      </div>
    </div>
  );
}
