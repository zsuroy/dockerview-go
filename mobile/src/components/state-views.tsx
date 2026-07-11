import { Ionicons } from '@expo/vector-icons';
import { ActivityIndicator, StyleSheet, TouchableOpacity, View } from 'react-native';

import { ThemedText } from '@/components/themed-text';
import { Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';

export function LoadingState({ message }: { message: string }) {
  const theme = useTheme();
  return (
    <View style={styles.center}>
      <ActivityIndicator color={theme.primary} size="large" />
      <ThemedText type="small" themeColor="textSecondary" style={styles.message}>
        {message}
      </ThemedText>
    </View>
  );
}

export function EmptyState({
  title,
  message,
  icon = 'cube-outline',
}: {
  title: string;
  message?: string;
  icon?: keyof typeof Ionicons.glyphMap;
}) {
  const theme = useTheme();
  return (
    <View style={styles.center}>
      <Ionicons name={icon} size={48} color={theme.textSecondary} />
      <ThemedText type="smallBold" style={styles.title}>
        {title}
      </ThemedText>
      {message ? (
        <ThemedText type="small" themeColor="textSecondary" style={styles.message}>
          {message}
        </ThemedText>
      ) : null}
    </View>
  );
}

export function ErrorState({
  message,
  actionLabel,
  onAction,
}: {
  message: string;
  actionLabel: string;
  onAction: () => void;
}) {
  const theme = useTheme();
  return (
    <View style={styles.center}>
      <Ionicons name="alert-circle-outline" size={48} color={theme.danger} />
      <ThemedText type="smallBold" style={styles.title}>
        Connection problem
      </ThemedText>
      <ThemedText type="small" themeColor="textSecondary" style={styles.message}>
        {message}
      </ThemedText>
      <TouchableOpacity style={[styles.action, { backgroundColor: theme.primary }]} onPress={onAction} activeOpacity={0.75}>
        <ThemedText type="smallBold" style={styles.actionText}>
          {actionLabel}
        </ThemedText>
      </TouchableOpacity>
    </View>
  );
}

export function SkeletonCard() {
  const theme = useTheme();
  return (
    <View style={[styles.skeleton, { backgroundColor: theme.backgroundElement }]}>
      <View style={styles.skeletonTop}>
        <View style={[styles.skeletonName, { backgroundColor: theme.backgroundSelected }]} />
        <View style={[styles.skeletonBadge, { backgroundColor: theme.backgroundSelected }]} />
      </View>
      <View style={[styles.skeletonLine, { backgroundColor: theme.backgroundSelected }]} />
      <View style={[styles.skeletonLine, { backgroundColor: theme.backgroundSelected, width: '62%' }]} />
    </View>
  );
}

const styles = StyleSheet.create({
  action: {
    borderRadius: 8,
    marginTop: Spacing.three,
    paddingHorizontal: Spacing.four,
    paddingVertical: Spacing.two,
  },
  actionText: {
    color: '#ffffff',
  },
  center: {
    alignItems: 'center',
    flex: 1,
    justifyContent: 'center',
    paddingHorizontal: Spacing.four,
    paddingVertical: Spacing.six,
  },
  message: {
    marginTop: Spacing.two,
    maxWidth: 320,
    textAlign: 'center',
  },
  skeleton: {
    borderRadius: 8,
    gap: Spacing.three,
    marginBottom: Spacing.two,
    padding: Spacing.three,
  },
  skeletonBadge: {
    borderRadius: 6,
    height: 24,
    width: 78,
  },
  skeletonLine: {
    borderRadius: 4,
    height: 8,
    width: '100%',
  },
  skeletonName: {
    borderRadius: 4,
    height: 16,
    width: 150,
  },
  skeletonTop: {
    alignItems: 'center',
    flexDirection: 'row',
    justifyContent: 'space-between',
  },
  title: {
    marginTop: Spacing.three,
  },
});
