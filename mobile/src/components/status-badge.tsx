import { Ionicons } from '@expo/vector-icons';
import { StyleSheet, View } from 'react-native';

import { ThemedText } from '@/components/themed-text';
import { Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export function isContainerRunning(status: string): boolean {
  const value = status?.toLowerCase() ?? '';
  return value.includes('up') || value.includes('running') || value.includes('healthy');
}

export function StatusBadge({
  status,
  healthStatus,
}: {
  status: string;
  healthStatus?: string;
}) {
  const theme = useTheme();
  const running = isContainerRunning(status);
  const unhealthy = running && healthStatus === 'unhealthy';
  const starting = running && healthStatus === 'starting';

  const color = unhealthy ? theme.danger : starting ? theme.warning : running ? theme.success : theme.textSecondary;
  const label = unhealthy ? 'Unhealthy' : starting ? 'Starting' : running ? 'Running' : 'Stopped';
  const icon = unhealthy
    ? 'warning-outline'
    : starting
      ? 'sync-outline'
      : running
        ? 'play-circle-outline'
        : 'stop-circle-outline';

  return (
    <View style={[styles.badge, { backgroundColor: `${color}1f` }]}>
      <View style={[styles.dot, { backgroundColor: color }]} />
      <Ionicons name={icon} size={12} color={color} />
      <ThemedText type="code" style={[styles.text, { color }]}>
        {label}
      </ThemedText>
    </View>
  );
}

const styles = StyleSheet.create({
  badge: {
    alignItems: 'center',
    borderRadius: 6,
    flexDirection: 'row',
    gap: Spacing.one,
    paddingHorizontal: Spacing.two,
    paddingVertical: Spacing.one,
  },
  dot: {
    borderRadius: 3,
    height: 6,
    width: 6,
  },
  text: {
    fontSize: 11,
    fontWeight: '700',
  },
});
