import { Ionicons } from '@expo/vector-icons';
import { useRouter } from 'expo-router';
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  FlatList,
  Modal,
  Platform,
  RefreshControl,
  ScrollView,
  StyleSheet,
  TextInput,
  TouchableOpacity,
  useWindowDimensions,
  View,
} from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { EmptyState, ErrorState, LoadingState, SkeletonCard } from '@/components/state-views';
import { StatusBadge, isContainerRunning } from '@/components/status-badge';
import { ThemedText } from '@/components/themed-text';
import { ThemedView } from '@/components/themed-view';
import { BottomTabInset, MaxContentWidth, Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';
import {
  ContainerInfo,
  ExecResult,
  fetchContainerLogs,
  fetchContainers,
  performContainerOp,
  runContainerExec,
} from '@/utils/api';
import { useTranslation } from '@/utils/i18n';
import { getAuthToken, getServerUrl } from '@/utils/storage';

const AUTO_POLL_INTERVAL = 3000;

type StatusFilter = 'all' | 'running' | 'stopped';
type DetailTab = 'metrics' | 'logs' | 'exec';

function parsePercent(value?: string): number {
  if (!value) return 0;
  const match = value.match(/[\d.]+/);
  return match ? Number(match[0]) || 0 : 0;
}

function getTargetId(container: ContainerInfo): string {
  return container.FullID || container.ID;
}

function ProgressBar({ value, tone }: { value: number; tone: string }) {
  return (
    <View style={styles.progressTrack}>
      <View style={[styles.progressFill, { width: `${Math.min(value, 100)}%`, backgroundColor: tone }]} />
    </View>
  );
}

function FilterChip({
  label,
  count,
  active,
  onPress,
}: {
  label: string;
  count: number;
  active: boolean;
  onPress: () => void;
}) {
  const theme = useTheme();
  return (
    <TouchableOpacity
      activeOpacity={0.75}
      onPress={onPress}
      style={[
        styles.filterChip,
        {
          backgroundColor: active ? theme.primary : theme.backgroundElement,
          borderColor: active ? theme.primary : theme.border,
        },
      ]}>
      <ThemedText type="smallBold" style={{ color: active ? '#ffffff' : theme.text }}>
        {label}
      </ThemedText>
      <View style={[styles.filterCount, { backgroundColor: active ? 'rgba(255,255,255,0.22)' : theme.backgroundSelected }]}>
        <ThemedText type="code" style={{ color: active ? '#ffffff' : theme.textSecondary }}>
          {count}
        </ThemedText>
      </View>
    </TouchableOpacity>
  );
}

function SummaryTile({ label, value, color }: { label: string; value: number; color: string }) {
  const theme = useTheme();
  return (
    <View style={[styles.summaryTile, { backgroundColor: theme.backgroundElement, borderColor: theme.backgroundSelected }]}>
      <ThemedText type="small" themeColor="textSecondary">
        {label}
      </ThemedText>
      <ThemedText type="subtitle" style={[styles.summaryValue, { color }]}>
        {value}
      </ThemedText>
    </View>
  );
}

function ContainerCard({ container, onPress }: { container: ContainerInfo; onPress: () => void }) {
  const theme = useTheme();
  const running = isContainerRunning(container.Status);
  const cpu = parsePercent(container.CPU);
  const memoryLabel = container.Memory?.split('/')[0]?.trim() || container.Memory || '0';
  const cpuColor = cpu > 80 ? theme.danger : cpu > 50 ? theme.warning : theme.primary;

  return (
    <TouchableOpacity activeOpacity={0.78} onPress={onPress}>
      <View style={[styles.card, { backgroundColor: theme.backgroundElement, borderColor: theme.border }]}>
        <View style={styles.cardHeader}>
          <View style={styles.cardTitleWrap}>
            <View style={[styles.statusDot, { backgroundColor: running ? theme.success : theme.textSecondary }]} />
            <View style={styles.cardTitleText}>
              <ThemedText type="smallBold" numberOfLines={1} style={styles.cardName}>
                {container.Name}
              </ThemedText>
              <ThemedText type="code" themeColor="textSecondary">
                {getTargetId(container).slice(0, 12)}
              </ThemedText>
            </View>
          </View>
          <StatusBadge status={container.Status} healthStatus={container.HealthStatus} />
        </View>

        <View style={styles.metricGrid}>
          <View style={styles.metricBlock}>
            <View style={styles.metricHeader}>
              <ThemedText type="code" themeColor="textSecondary">
                CPU
              </ThemedText>
              <ThemedText type="code" style={{ color: cpuColor }}>
                {container.CPU || '0%'}
              </ThemedText>
            </View>
            <ProgressBar value={cpu} tone={cpuColor} />
          </View>
          <View style={styles.metricBlock}>
            <View style={styles.metricHeader}>
              <ThemedText type="code" themeColor="textSecondary">
                RAM
              </ThemedText>
              <ThemedText type="code">{memoryLabel}</ThemedText>
            </View>
            <ProgressBar value={parsePercent(container.Memory)} tone={theme.success} />
          </View>
        </View>

        {container.ports?.length ? (
          <View style={styles.portsRow}>
            {container.ports.slice(0, 3).map((port, index) => (
              <View key={`${port.private_port}-${index}`} style={[styles.portPill, { backgroundColor: theme.backgroundSelected }]}>
                <ThemedText type="code" style={styles.portText}>
                  {port.public_port ? `${port.public_port}->${port.private_port}` : port.private_port}/{port.type}
                </ThemedText>
              </View>
            ))}
            {container.ports.length > 3 ? (
              <ThemedText type="code" themeColor="textSecondary">
                +{container.ports.length - 3}
              </ThemedText>
            ) : null}
          </View>
        ) : null}
      </View>
    </TouchableOpacity>
  );
}

function InfoRow({ label, value }: { label: string; value?: string }) {
  return (
    <View style={styles.infoRow}>
      <ThemedText type="small" themeColor="textSecondary">
        {label}
      </ThemedText>
      <ThemedText type="smallBold" style={styles.infoValue} numberOfLines={1}>
        {value || '-'}
      </ThemedText>
    </View>
  );
}

function MetricTile({
  icon,
  label,
  value,
  color,
}: {
  icon: keyof typeof Ionicons.glyphMap;
  label: string;
  value?: string;
  color: string;
}) {
  const theme = useTheme();
  return (
    <View style={[styles.detailMetricTile, { backgroundColor: theme.backgroundSelected }]}>
      <View style={styles.detailMetricHeader}>
        <Ionicons name={icon} size={16} color={color} />
        <ThemedText type="code" themeColor="textSecondary">
          {label}
        </ThemedText>
      </View>
      <ThemedText type="smallBold" numberOfLines={1} style={styles.detailMetricValue}>
        {value || '-'}
      </ThemedText>
    </View>
  );
}

function DetailSheet({
  container,
  visible,
  serverUrl,
  authToken,
  onClose,
  onChanged,
}: {
  container: ContainerInfo | null;
  visible: boolean;
  serverUrl: string;
  authToken: string;
  onClose: () => void;
  onChanged: () => Promise<void>;
}) {
  const theme = useTheme();
  const { t } = useTranslation();
  const [tab, setTab] = useState<DetailTab>('metrics');
  const [opLoading, setOpLoading] = useState(false);
  const [logsLoading, setLogsLoading] = useState(false);
  const [logsText, setLogsText] = useState('');
  const [logTail, setLogTail] = useState('100');
  const [logGrep, setLogGrep] = useState('');
  const [logLevel, setLogLevel] = useState('');
  const [execCmd, setExecCmd] = useState('');
  const [execResult, setExecResult] = useState<ExecResult | null>(null);
  const [execLoading, setExecLoading] = useState(false);

  const opBusyRef = useRef(false);
  const OP_COOLDOWN_MS = 2000;
  const grepTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);


  const running = container ? isContainerRunning(container.Status) : false;

  const loadLogs = useCallback(
    async (overrides?: { tail?: string; grep?: string; level?: string }) => {
      if (!container) return;
      const tail = overrides?.tail ?? logTail;
      const grep = overrides?.grep ?? logGrep;
      const level = overrides?.level ?? logLevel;
      setLogsLoading(true);
      try {
        const logs = await fetchContainerLogs(serverUrl, authToken, getTargetId(container), tail, grep, level);
        setLogsText(logs || t('noLogs'));
      } catch (error: any) {
        setLogsText(error?.message || t('noLogs'));
      } finally {
        setLogsLoading(false);
      }
    },
    [authToken, container, logGrep, logLevel, logTail, serverUrl, t],
  );

  const selectTab = (nextTab: DetailTab) => {
    setTab(nextTab);
    if (nextTab === 'logs') {
      loadLogs();
    }
  };

  const performOp = async (op: 'start' | 'stop' | 'restart') => {
    if (!container) return;
    setOpLoading(true);
    try {
      await performContainerOp(serverUrl, authToken, getTargetId(container), op);
      await onChanged();
      if (Platform.OS === 'web') {
        alert(`${op} completed`);
      }
    } catch (error: any) {
      const message = error?.message || `Failed to ${op}`;
      if (Platform.OS === 'web') {
        alert(message);
      } else {
        Alert.alert('Operation failed', message);
      }
    } finally {
      setOpLoading(false);
      setTimeout(() => {
        opBusyRef.current = false;
      }, OP_COOLDOWN_MS);
    }
  };

  const confirmOp = (op: 'start' | 'stop' | 'restart') => {
    if (!container || opBusyRef.current) return;
    opBusyRef.current = true;
    setOpLoading(true);
    const label = op.charAt(0).toUpperCase() + op.slice(1);
    const message = `${label} "${container.Name}"?`;
    if (Platform.OS === 'web') {
      if (confirm(message)) {
        performOp(op);
      } else {
        opBusyRef.current = false;
        setOpLoading(false);
      }
      return;
    }
    Alert.alert(`${label} container`, message, [
      { text: 'Cancel', style: 'cancel', onPress: () => { opBusyRef.current = false; setOpLoading(false); } },
      {
        text: label,
        style: op === 'stop' ? 'destructive' : 'default',
        onPress: () => performOp(op),
      },
    ]);
  };

  const runExec = async (command?: string) => {
    if (!container) return;
    const nextCommand = command || execCmd;
    if (!nextCommand.trim()) return;
    setExecLoading(true);
    setExecResult(null);
    try {
      const result = await runContainerExec(serverUrl, authToken, getTargetId(container), nextCommand);
      setExecResult(result);
    } catch (error: any) {
      setExecResult({ exit_code: -1, stdout: '', stderr: error?.message || 'Command failed' });
    } finally {
      setExecLoading(false);
    }
  };

  if (!container) return null;

  return (
    <Modal animationType="slide" transparent visible={visible} onRequestClose={onClose}>
      <View style={styles.modalOverlay}>
        <ThemedView type="backgroundElement" style={styles.sheet}>
          <View style={styles.sheetHandle} />
          <View style={styles.sheetHeader}>
            <View style={styles.sheetTitleWrap}>
              <ThemedText type="smallBold" numberOfLines={1} style={styles.sheetTitle}>
                {container.Name}
              </ThemedText>
              <ThemedText type="code" themeColor="textSecondary" numberOfLines={1}>
                {getTargetId(container)}
              </ThemedText>
            </View>
            <TouchableOpacity style={styles.iconButton} onPress={onClose}>
              <Ionicons name="close" size={22} color={theme.text} />
            </TouchableOpacity>
          </View>

          <View style={styles.actionRow}>
            {running ? (
              <>
                <TouchableOpacity style={[styles.actionButton, styles.stopButton, opLoading && styles.actionDisabled, { backgroundColor: opLoading ? theme.surfaceMuted : theme.danger }]} onPress={() => confirmOp('stop')} disabled={opLoading} activeOpacity={opLoading ? 1 : 0.78}>
                  <Ionicons name="stop" size={16} color={opLoading ? theme.textSecondary : '#ffffff'} />
                  <ThemedText type="smallBold" style={[styles.actionText, opLoading && { color: theme.textSecondary }]}>
                    {t('stop')}
                  </ThemedText>
                </TouchableOpacity>
                <TouchableOpacity style={[styles.actionButton, styles.restartButton, opLoading && styles.actionDisabled, { backgroundColor: opLoading ? theme.surfaceMuted : theme.warning }]} onPress={() => confirmOp('restart')} disabled={opLoading} activeOpacity={opLoading ? 1 : 0.78}>
                  <Ionicons name="refresh" size={16} color={opLoading ? theme.textSecondary : '#ffffff'} />
                  <ThemedText type="smallBold" style={[styles.actionText, opLoading && { color: theme.textSecondary }]}>
                    {t('restart')}
                  </ThemedText>
                </TouchableOpacity>
              </>
            ) : (
              <TouchableOpacity style={[styles.actionButton, styles.startButton, opLoading && styles.actionDisabled, { backgroundColor: opLoading ? theme.surfaceMuted : theme.success }]} onPress={() => confirmOp('start')} disabled={opLoading} activeOpacity={opLoading ? 1 : 0.78}>
                {opLoading ? <ActivityIndicator color={theme.textSecondary} size="small" /> : <Ionicons name="play" size={16} color={opLoading ? theme.textSecondary : '#ffffff'} />}
                <ThemedText type="smallBold" style={[styles.actionText, opLoading && { color: theme.textSecondary }]}>
                  {t('start')}
                </ThemedText>
              </TouchableOpacity>
            )}
          </View>

          <View style={[styles.sheetTabs, { backgroundColor: theme.surfaceMuted }]}>
            {(['metrics', 'logs', 'exec'] as DetailTab[]).map((nextTab) => (
              <TouchableOpacity
                key={nextTab}
                style={[styles.sheetTab, tab === nextTab && { backgroundColor: theme.backgroundSelected }]}
                onPress={() => selectTab(nextTab)}>
                <ThemedText type="smallBold" themeColor={tab === nextTab ? 'text' : 'textSecondary'}>
                  {nextTab === 'metrics' ? t('metrics') : nextTab === 'logs' ? t('logs') : t('console')}
                </ThemedText>
              </TouchableOpacity>
            ))}
          </View>

          {tab === 'metrics' ? (
            <ScrollView style={styles.sheetBody} contentContainerStyle={styles.sheetBodyContent}>
              <View style={styles.detailHero}>
                <StatusBadge status={container.Status} healthStatus={container.HealthStatus} />
                {container.HealthScore !== undefined ? (
                  <ThemedText type="code" themeColor="textSecondary">
                    Health {container.HealthScore}/100
                  </ThemedText>
                ) : null}
              </View>
              <View style={styles.detailMetricGrid}>
                <MetricTile icon="hardware-chip-outline" label="CPU" value={container.CPU} color={parsePercent(container.CPU) > 50 ? theme.warning : theme.primary} />
                <MetricTile icon="server-outline" label="RAM" value={container.Memory} color={theme.success} />
                <MetricTile icon="save-outline" label="Disk I/O" value={container.Blkio} color={theme.primary} />
                <MetricTile icon="globe-outline" label="Network" value={container.Network} color={theme.success} />
              </View>
              <InfoRow label={t('status')} value={container.Status} />
              <ThemedText type="smallBold" style={styles.sectionLabel}>
                {t('ports')}
              </ThemedText>
              {container.ports?.length ? (
                container.ports.map((port, index) => (
                  <View key={`${port.private_port}-${index}`} style={[styles.portRow, { backgroundColor: theme.backgroundSelected }]}>
                    <ThemedText type="code">
                      {port.ip ? `${port.ip}:` : ''}
                      {port.public_port ? `${port.public_port} -> ` : ''}
                      {port.private_port}/{port.type}
                    </ThemedText>
                  </View>
                ))
              ) : (
                <ThemedText type="small" themeColor="textSecondary">
                  No ports exposed
                </ThemedText>
              )}
            </ScrollView>
          ) : null}

          {tab === 'logs' ? (
            <View style={styles.sheetBody}>
              <View style={styles.logTools}>
                <TextInput
                  style={[styles.input, { backgroundColor: theme.backgroundSelected, color: theme.text }]}
                  placeholder={t('searchLogs')}
                  placeholderTextColor={theme.textSecondary}
                  value={logGrep}
                  onChangeText={(text) => {
                    setLogGrep(text);
                    if (grepTimerRef.current) clearTimeout(grepTimerRef.current);
                    grepTimerRef.current = setTimeout(() => loadLogs({ grep: text }), 400);
                  }}
                />
                <View style={styles.optionRow}>
                  {['', 'INFO', 'WARN', 'ERROR'].map((level) => (
                    <TouchableOpacity
                      key={level || 'ALL'}
                      style={[
                        styles.optionPill,
                        { backgroundColor: theme.surfaceMuted },
                        logLevel === level && { backgroundColor: theme.primary },
                      ]}
                      onPress={() => { setLogLevel(level); loadLogs({ level }); }}>
                      <ThemedText type="code" style={{ color: logLevel === level ? '#ffffff' : theme.textSecondary }}>
                        {level || 'ALL'}
                      </ThemedText>
                    </TouchableOpacity>
                  ))}
                </View>
                <View style={styles.optionRow}>
                  {['100', '500', '1000'].map((tail) => (
                    <TouchableOpacity
                      key={tail}
                      style={[
                        styles.optionPill,
                        { backgroundColor: theme.surfaceMuted },
                        logTail === tail && { backgroundColor: theme.primary },
                      ]}
                      onPress={() => { setLogTail(tail); loadLogs({ tail }); }}>
                      <ThemedText type="code" style={{ color: logTail === tail ? '#ffffff' : theme.textSecondary }}>
                        {tail}
                      </ThemedText>
                    </TouchableOpacity>
                  ))}
                  <TouchableOpacity style={[styles.optionPill, styles.refreshLogs, { backgroundColor: theme.primary }]} onPress={() => loadLogs()}>
                    <Ionicons name="reload" size={14} color="#ffffff" />
                  </TouchableOpacity>
                </View>
              </View>
              <ScrollView style={[styles.console, { backgroundColor: theme.console }]} contentContainerStyle={styles.consoleContent}>
                {logsLoading ? (
                  <ActivityIndicator color={theme.success} />
                ) : (
                  <ThemedText type="code" style={styles.consoleText}>
                    {logsText || t('loadingLogs')}
                  </ThemedText>
                )}
              </ScrollView>
            </View>
          ) : null}

          {tab === 'exec' ? (
            <View style={styles.sheetBody}>
              <View style={styles.execRow}>
                <TextInput
                  style={[styles.input, styles.execInput, { backgroundColor: theme.backgroundSelected, color: theme.text }]}
                  placeholder="df -h"
                  placeholderTextColor={theme.textSecondary}
                  value={execCmd}
                  onChangeText={setExecCmd}
                  autoCapitalize="none"
                  autoCorrect={false}
                />
                <TouchableOpacity style={[styles.runButton, { backgroundColor: theme.primary }]} onPress={() => runExec()} disabled={execLoading}>
                  {execLoading ? <ActivityIndicator color="#ffffff" size="small" /> : <Ionicons name="terminal" size={16} color="#ffffff" />}
                </TouchableOpacity>
              </View>
              <View style={styles.shortcutRow}>
                {['ls -la', 'df -h', 'env', 'uname -a'].map((command) => (
                  <TouchableOpacity key={command} style={[styles.shortcut, { backgroundColor: theme.backgroundSelected }]} onPress={() => runExec(command)}>
                    <ThemedText type="code">{command}</ThemedText>
                  </TouchableOpacity>
                ))}
              </View>
              <ScrollView style={[styles.console, { backgroundColor: theme.console }]} contentContainerStyle={styles.consoleContent}>
                {execResult ? (
                  <>
                    <ThemedText type="code" style={{ color: theme.warning, marginBottom: Spacing.one }}>
                      {t('exitCode')}: {execResult.exit_code}
                    </ThemedText>
                    {execResult.stdout ? <ThemedText type="code" style={styles.consoleText}>{execResult.stdout}</ThemedText> : null}
                    {execResult.stderr ? <ThemedText type="code" style={[styles.consoleText, { color: theme.danger }]}>{execResult.stderr}</ThemedText> : null}
                  </>
                ) : (
                  <ThemedText type="code" themeColor="textSecondary">
                    {t('terminalOutput')}
                  </ThemedText>
                )}
              </ScrollView>
            </View>
          ) : null}
        </ThemedView>
      </View>
    </Modal>
  );
}

export default function DashboardScreen() {
  const router = useRouter();
  const theme = useTheme();
  const { t } = useTranslation();
  const insets = useSafeAreaInsets();
  const { width } = useWindowDimensions();
  const [serverUrl, setServerUrl] = useState('');
  const [authToken, setAuthToken] = useState('');
  const [containers, setContainers] = useState<ContainerInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState<StatusFilter>('all');
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [selectedSnapshot, setSelectedSnapshot] = useState<ContainerInfo | null>(null);
  const [detailVisible, setDetailVisible] = useState(false);

  const bottomInset = insets.bottom + BottomTabInset + Spacing.four;

  const loadSettings = useCallback(async () => {
    const [url, token] = await Promise.all([getServerUrl(), getAuthToken()]);
    setServerUrl(url);
    setAuthToken(token);
    return { url, token };
  }, []);

  const loadContainers = useCallback(async (url: string, token: string, silent = false) => {
    if (!silent) setLoading(true);
    try {
      const data = await fetchContainers(url, token);
      setContainers(data);
      setError(null);
    } catch (nextError: any) {
      setError(nextError?.message || 'Failed to connect to DockerView-Go server.');
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    let active = true;
    Promise.all([getServerUrl(), getAuthToken()]).then(([url, token]) => {
      if (active) {
        setServerUrl(url);
        setAuthToken(token);
        loadContainers(url, token);
      }
    });
    return () => {
      active = false;
    };
  }, [loadContainers, loadSettings]);

  useEffect(() => {
    if (!serverUrl) return;
    const id = setInterval(() => {
      loadContainers(serverUrl, authToken, true);
    }, AUTO_POLL_INTERVAL);
    return () => clearInterval(id);
  }, [authToken, loadContainers, serverUrl]);

  const counts = useMemo(() => {
    const running = containers.filter((container) => isContainerRunning(container.Status)).length;
    const alerts = containers.filter((container) => parsePercent(container.CPU) > 50 || container.HealthStatus === 'unhealthy').length;
    return {
      all: containers.length,
      running,
      stopped: containers.length - running,
      alerts,
    };
  }, [containers]);

  const filteredContainers = useMemo(() => {
    const query = search.trim().toLowerCase();
    return containers.filter((container) => {
      const running = isContainerRunning(container.Status);
      const statusMatches =
        filter === 'all' || (filter === 'running' && running) || (filter === 'stopped' && !running);
      const searchMatches =
        !query ||
        container.Name.toLowerCase().includes(query) ||
        getTargetId(container).toLowerCase().includes(query) ||
        container.ports?.some((port) => String(port.public_port || port.private_port).includes(query));
      return statusMatches && searchMatches;
    });
  }, [containers, filter, search]);

  const selectedContainer = useMemo(() => {
    if (!selectedId) return null;
    return containers.find((container) => getTargetId(container) === selectedId || container.ID === selectedId) || selectedSnapshot;
  }, [containers, selectedId, selectedSnapshot]);

  const refresh = useCallback(async () => {
    setRefreshing(true);
    const config = await loadSettings();
    await loadContainers(config.url, config.token, true);
  }, [loadContainers, loadSettings]);

  const openContainer = useCallback((container: ContainerInfo) => {
    setSelectedId(getTargetId(container));
    setSelectedSnapshot(container);
    setDetailVisible(true);
  }, []);

  const renderHeader = () => (
    <View style={styles.headerContent}>
      <View style={styles.topBar}>
        <View style={styles.titleWrap}>
          <ThemedText type="subtitle" style={styles.title}>
            {t('dashboard')}
          </ThemedText>
          <View style={styles.connectionLine}>
            <View style={[styles.liveDot, { backgroundColor: error ? theme.danger : theme.success }]} />
            <ThemedText type="code" themeColor="textSecondary" numberOfLines={1}>
              {serverUrl ? `${error ? t('disconnected') : t('connected')} ${serverUrl.replace(/^https?:\/\//, '')}` : t('disconnected')}
            </ThemedText>
          </View>
        </View>
        <TouchableOpacity style={[styles.iconButton, { backgroundColor: theme.backgroundElement }]} onPress={refresh}>
          <Ionicons name="refresh" size={20} color={theme.primary} />
        </TouchableOpacity>
      </View>

      <View style={styles.summaryGrid}>
        <SummaryTile label={t('total')} value={counts.all} color={theme.text} />
        <SummaryTile label={t('running')} value={counts.running} color={theme.success} />
        <SummaryTile label={t('stopped')} value={counts.stopped} color={theme.textSecondary} />
        <SummaryTile label={t('alerts')} value={counts.alerts} color={counts.alerts ? theme.warning : theme.textSecondary} />
      </View>

      {error ? (
        <View style={[styles.inlineError, { backgroundColor: theme.backgroundElement, borderColor: theme.danger }]}>
          <Ionicons name="warning-outline" size={18} color={theme.danger} />
          <ThemedText type="small" style={styles.inlineErrorText}>
            {error}
          </ThemedText>
          <TouchableOpacity onPress={() => router.replace('/settings')}>
            <ThemedText type="smallBold" style={{ color: theme.primary }}>
              {t('configureConn')}
            </ThemedText>
          </TouchableOpacity>
        </View>
      ) : null}

      <View style={[styles.searchBox, { backgroundColor: theme.backgroundElement, borderColor: theme.backgroundSelected }]}>
        <Ionicons name="search-outline" size={18} color={theme.textSecondary} />
        <TextInput
          style={[styles.searchInput, { color: theme.text }]}
          placeholder={t('searchPlaceholder')}
          placeholderTextColor={theme.textSecondary}
          value={search}
          onChangeText={setSearch}
          autoCapitalize="none"
          autoCorrect={false}
        />
        {search ? (
          <TouchableOpacity onPress={() => setSearch('')}>
            <Ionicons name="close-circle" size={18} color={theme.textSecondary} />
          </TouchableOpacity>
        ) : null}
      </View>

      <View style={styles.filters}>
        <FilterChip label={t('all')} count={counts.all} active={filter === 'all'} onPress={() => setFilter('all')} />
        <FilterChip label={t('running')} count={counts.running} active={filter === 'running'} onPress={() => setFilter('running')} />
        <FilterChip label={t('stopped')} count={counts.stopped} active={filter === 'stopped'} onPress={() => setFilter('stopped')} />
      </View>
    </View>
  );

  if (loading && containers.length === 0) {
    return (
      <View style={[styles.screen, { backgroundColor: theme.background }]}>
        <LoadingState message={t('connecting')} />
      </View>
    );
  }

  if (error && containers.length === 0) {
    return (
      <View style={[styles.screen, { backgroundColor: theme.background }]}>
        <ErrorState message={error} actionLabel={t('configureConn')} onAction={() => router.replace('/settings')} />
      </View>
    );
  }

  return (
    <View style={[styles.screen, { backgroundColor: theme.background }]}>
      <FlatList
        data={filteredContainers}
        keyExtractor={(item) => getTargetId(item)}
        renderItem={({ item }) => <ContainerCard container={item} onPress={() => openContainer(item)} />}
        ListHeaderComponent={renderHeader}
        ListEmptyComponent={
          loading ? (
            <View style={styles.skeletonWrap}>
              {[0, 1, 2].map((item) => (
                <SkeletonCard key={item} />
              ))}
            </View>
          ) : (
            <EmptyState
              title={search ? 'No matching containers' : t('emptyContainers')}
              message={search ? 'Try another name, ID, or port.' : undefined}
            />
          )
        }
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={refresh} tintColor={theme.primary} />}
        contentContainerStyle={[
          styles.listContent,
          {
            paddingBottom: bottomInset,
            paddingTop: Platform.OS === 'web' ? Spacing.four : Math.max(insets.top, Spacing.three),
            maxWidth: width >= 900 ? MaxContentWidth : undefined,
            alignSelf: width >= 900 ? 'center' : 'stretch',
          },
        ]}
        showsVerticalScrollIndicator={false}
      />

      <DetailSheet
        key={selectedId || 'container-detail'}
        container={selectedContainer}
        visible={detailVisible}
        serverUrl={serverUrl}
        authToken={authToken}
        onClose={() => setDetailVisible(false)}
        onChanged={refresh}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  actionButton: {
    alignItems: 'center',
    borderRadius: 8,
    flex: 1,
    flexDirection: 'row',
    gap: Spacing.one,
    justifyContent: 'center',
    minHeight: 44,
    paddingHorizontal: Spacing.three,
  },
  actionDisabled: {
    opacity: 0.5,
  },
  actionRow: {
    flexDirection: 'row',
    gap: Spacing.two,
  },
  actionText: {
    color: '#ffffff',
  },
  card: {
    borderRadius: 8,
    borderWidth: 1,
    gap: Spacing.three,
    marginBottom: Spacing.two,
    padding: Spacing.three,
  },
  cardHeader: {
    alignItems: 'flex-start',
    flexDirection: 'row',
    gap: Spacing.two,
    justifyContent: 'space-between',
  },
  cardName: {
    fontSize: 16,
  },
  cardTitleText: {
    flex: 1,
  },
  cardTitleWrap: {
    alignItems: 'center',
    flex: 1,
    flexDirection: 'row',
    gap: Spacing.two,
    minWidth: 0,
  },
  connectionLine: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: Spacing.one,
    marginTop: Spacing.one,
  },
  console: {
    backgroundColor: '#07111f',
    borderRadius: 8,
    flex: 1,
    marginTop: Spacing.two,
  },
  consoleContent: {
    padding: Spacing.three,
  },
  consoleText: {
    color: '#d1fae5',
    lineHeight: 18,
  },
  detailHero: {
    alignItems: 'center',
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginBottom: Spacing.three,
  },
  detailMetricGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: Spacing.two,
    marginBottom: Spacing.three,
  },
  detailMetricHeader: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: Spacing.one,
  },
  detailMetricTile: {
    borderRadius: 8,
    flexBasis: '48%',
    flexGrow: 1,
    gap: Spacing.one,
    padding: Spacing.three,
  },
  detailMetricValue: {
    fontSize: 15,
  },
  execInput: {
    flex: 1,
  },
  execRow: {
    flexDirection: 'row',
    gap: Spacing.two,
  },
  filterChip: {
    alignItems: 'center',
    borderRadius: 8,
    borderWidth: 1,
    flex: 1,
    flexDirection: 'row',
    gap: Spacing.one,
    justifyContent: 'center',
    minHeight: 40,
    paddingHorizontal: Spacing.two,
  },
  filterCount: {
    borderRadius: 5,
    paddingHorizontal: Spacing.one,
    paddingVertical: 1,
  },
  filters: {
    flexDirection: 'row',
    gap: Spacing.two,
  },
  headerContent: {
    gap: Spacing.three,
    marginBottom: Spacing.three,
  },
  iconButton: {
    alignItems: 'center',
    borderRadius: 8,
    height: 42,
    justifyContent: 'center',
    width: 42,
  },
  infoRow: {
    alignItems: 'center',
    borderBottomColor: 'rgba(128,128,128,0.18)',
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: 'row',
    gap: Spacing.three,
    justifyContent: 'space-between',
    paddingVertical: Spacing.two,
  },
  infoValue: {
    flex: 1,
    textAlign: 'right',
  },
  inlineError: {
    alignItems: 'center',
    borderRadius: 8,
    borderWidth: 1,
    flexDirection: 'row',
    gap: Spacing.two,
    padding: Spacing.three,
  },
  inlineErrorText: {
    color: '#ef4444',
    flex: 1,
  },
  input: {
    borderRadius: 8,
    fontSize: 14,
    minHeight: 42,
    paddingHorizontal: Spacing.three,
  },
  listContent: {
    paddingHorizontal: Spacing.three,
  },
  liveDot: {
    borderRadius: 4,
    height: 8,
    width: 8,
  },
  logTools: {
    gap: Spacing.two,
  },
  metricBlock: {
    flex: 1,
    gap: Spacing.one,
  },
  metricGrid: {
    flexDirection: 'row',
    gap: Spacing.three,
  },
  metricHeader: {
    alignItems: 'center',
    flexDirection: 'row',
    justifyContent: 'space-between',
  },
  modalOverlay: {
    backgroundColor: 'rgba(0,0,0,0.48)',
    flex: 1,
    justifyContent: 'flex-end',
  },
  optionPill: {
    alignItems: 'center',
    backgroundColor: 'rgba(128,128,128,0.12)',
    borderRadius: 7,
    minHeight: 32,
    minWidth: 52,
    paddingHorizontal: Spacing.two,
    paddingVertical: Spacing.one,
  },
  optionPillActive: {
    backgroundColor: '#3c87f7',
  },
  optionRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: Spacing.two,
  },
  portPill: {
    borderRadius: 5,
    paddingHorizontal: Spacing.two,
    paddingVertical: Spacing.one,
  },
  portRow: {
    borderRadius: 6,
    marginTop: Spacing.one,
    padding: Spacing.two,
  },
  portText: {
    fontSize: 10,
  },
  portsRow: {
    alignItems: 'center',
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: Spacing.one,
  },
  progressFill: {
    borderRadius: 3,
    height: '100%',
  },
  progressTrack: {
    backgroundColor: 'rgba(128,128,128,0.18)',
    borderRadius: 3,
    height: 6,
    overflow: 'hidden',
  },
  refreshLogs: {
    backgroundColor: '#3c87f7',
    minWidth: 40,
  },
  restartButton: {
    backgroundColor: '#f59e0b',
  },
  runButton: {
    alignItems: 'center',
    backgroundColor: '#3c87f7',
    borderRadius: 8,
    justifyContent: 'center',
    width: 46,
  },
  screen: {
    flex: 1,
  },
  searchBox: {
    alignItems: 'center',
    borderRadius: 8,
    borderWidth: 1,
    flexDirection: 'row',
    gap: Spacing.two,
    minHeight: 44,
    paddingHorizontal: Spacing.three,
  },
  searchInput: {
    flex: 1,
    fontSize: 14,
    minHeight: 42,
  },
  sectionLabel: {
    marginTop: Spacing.three,
  },
  sheet: {
    borderTopLeftRadius: 16,
    borderTopRightRadius: 16,
    gap: Spacing.three,
    height: '86%',
    padding: Spacing.three,
  },
  sheetBody: {
    flex: 1,
  },
  sheetBodyContent: {
    paddingBottom: Spacing.four,
  },
  sheetHandle: {
    alignSelf: 'center',
    backgroundColor: 'rgba(128,128,128,0.35)',
    borderRadius: 2,
    height: 4,
    width: 40,
  },
  sheetHeader: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: Spacing.two,
  },
  sheetTab: {
    alignItems: 'center',
    borderRadius: 7,
    flex: 1,
    minHeight: 36,
    justifyContent: 'center',
  },
  sheetTabs: {
    backgroundColor: 'rgba(128,128,128,0.1)',
    borderRadius: 8,
    flexDirection: 'row',
    padding: 3,
  },
  sheetTitle: {
    fontSize: 18,
  },
  sheetTitleWrap: {
    flex: 1,
    minWidth: 0,
  },
  shortcut: {
    borderRadius: 7,
    paddingHorizontal: Spacing.two,
    paddingVertical: Spacing.one,
  },
  shortcutRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: Spacing.two,
    marginTop: Spacing.two,
  },
  skeletonWrap: {
    paddingTop: Spacing.two,
  },
  startButton: {
    backgroundColor: '#22c55e',
  },
  statusDot: {
    borderRadius: 5,
    height: 10,
    width: 10,
  },
  stopButton: {
    backgroundColor: '#ef4444',
  },
  summaryGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: Spacing.two,
  },
  summaryTile: {
    borderRadius: 8,
    borderWidth: 1,
    flexBasis: '48%',
    flexGrow: 1,
    padding: Spacing.three,
  },
  summaryValue: {
    fontSize: 24,
    lineHeight: 30,
  },
  title: {
    fontSize: 30,
    lineHeight: 36,
  },
  titleWrap: {
    flex: 1,
    minWidth: 0,
  },
  topBar: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: Spacing.three,
    justifyContent: 'space-between',
  },
});
