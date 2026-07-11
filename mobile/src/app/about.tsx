import { Ionicons } from '@expo/vector-icons';
import { Image } from 'expo-image';
import * as WebBrowser from 'expo-web-browser';
import React from 'react';
import { Platform, ScrollView, StyleSheet, TouchableOpacity, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { ThemedText } from '@/components/themed-text';
import { BottomTabInset, MaxContentWidth, Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';
import { useTranslation } from '@/utils/i18n';

function TechBadge({ label }: { label: string }) {
  const theme = useTheme();
  return (
    <View style={[styles.techBadge, { backgroundColor: theme.surfaceMuted, borderColor: theme.border }]}>
      <ThemedText type="code" style={styles.techText}>
        {label}
      </ThemedText>
    </View>
  );
}

function LinkButton({
  icon,
  label,
  onPress,
}: {
  icon: keyof typeof Ionicons.glyphMap;
  label: string;
  onPress: () => void;
}) {
  const theme = useTheme();
  return (
    <TouchableOpacity
      activeOpacity={0.75}
      onPress={onPress}
      style={[styles.linkButton, { backgroundColor: theme.backgroundElement, borderColor: theme.border }]}>
      <Ionicons name={icon} size={18} color={theme.primary} />
      <ThemedText type="smallBold" style={styles.linkText} numberOfLines={1}>
        {label}
      </ThemedText>
      <Ionicons name="open-outline" size={16} color={theme.textSecondary} />
    </TouchableOpacity>
  );
}

export default function AboutScreen() {
  const insets = useSafeAreaInsets();
  const theme = useTheme();
  const { t } = useTranslation();

  const openLink = async (url: string) => {
    try {
      await WebBrowser.openBrowserAsync(url);
    } catch {
      if (Platform.OS === 'web') {
        window.open(url, '_blank');
      }
    }
  };

  return (
    <View style={[styles.screen, { backgroundColor: theme.background }]}>
      <ScrollView
        style={styles.scrollView}
        contentContainerStyle={[
          styles.content,
          {
            paddingTop: Platform.OS === 'web' ? Spacing.four : Math.max(insets.top, Spacing.three),
            paddingBottom: insets.bottom + BottomTabInset + Spacing.four,
          },
        ]}
        showsVerticalScrollIndicator={false}>
        <View style={styles.inner}>
          <View style={styles.hero}>
            <Image source={require('@/assets/images/logo-glow.png')} style={styles.logo} contentFit="contain" />
            <ThemedText type="subtitle" style={styles.title}>
              DockerView
            </ThemedText>
            <ThemedText type="small" themeColor="textSecondary" style={styles.tagline}>
              Docker containers, readable from your phone.
            </ThemedText>
            <View style={[styles.versionPill, { backgroundColor: theme.surfaceMuted, borderColor: theme.border }]}>
              <ThemedText type="code" themeColor="textSecondary">
                v1.0.0
              </ThemedText>
            </View>
          </View>

          <View style={[styles.summaryCard, { backgroundColor: theme.backgroundElement, borderColor: theme.border }]}>
            <ThemedText type="smallBold" style={[styles.cardTitle, styles.fullWidth]}>
              {t('aboutSoftware')}
            </ThemedText>
            <ThemedText type="small" themeColor="textSecondary" style={[styles.description, styles.fullWidth, styles.justifyText]}>
              {t('aboutDesc')}
            </ThemedText>
          </View>

          <View style={styles.links}>
            <LinkButton icon="person-outline" label="Suroy" onPress={() => openLink('https://suroy.cn')} />
            <LinkButton icon="logo-github" label="GitHub Repository" onPress={() => openLink('https://github.com/zsuroy/dockerview-go')} />
          </View>

          <View style={styles.techSection}>
            <ThemedText type="smallBold" style={styles.cardTitle}>
              {t('techStack')}
            </ThemedText>
            <View style={styles.techGrid}>
              <TechBadge label="React Native" />
              <TechBadge label="Expo" />
              <TechBadge label="Expo Router" />
              <TechBadge label="TypeScript" />
              <TechBadge label="AsyncStorage" />
              <TechBadge label="Go Backend" />
              <TechBadge label="Docker API" />
            </View>
          </View>
        </View>
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  cardTitle: {
    fontSize: 15,
  },
  content: {
    alignItems: 'center',
    paddingHorizontal: Spacing.three,
  },
  description: {
    lineHeight: 22,
  },
  fullWidth: {
    alignSelf: 'stretch',
  },
  justifyText: {
    textAlign: 'justify',
  },
  hero: {
    alignItems: 'center',
    paddingBottom: Spacing.two,
    paddingTop: Spacing.two,
  },
  inner: {
    gap: Spacing.four,
    maxWidth: MaxContentWidth,
    width: '100%',
  },
  linkButton: {
    alignItems: 'center',
    borderRadius: 8,
    borderWidth: 1,
    flexDirection: 'row',
    gap: Spacing.two,
    minHeight: 52,
    paddingHorizontal: Spacing.three,
  },
  linkText: {
    flex: 1,
  },
  links: {
    gap: Spacing.two,
  },
  logo: {
    borderRadius: 8,
    height: 104,
    width: 104,
  },
  screen: {
    flex: 1,
  },
  scrollView: {
    flex: 1,
  },
  summaryCard: {
    alignSelf: 'stretch',
    borderRadius: 8,
    borderWidth: 1,
    gap: Spacing.two,
    padding: Spacing.three,
  },
  tagline: {
    marginTop: Spacing.one,
    textAlign: 'center',
  },
  techBadge: {
    borderRadius: 7,
    borderWidth: 1,
    paddingHorizontal: Spacing.two,
    paddingVertical: Spacing.one,
  },
  techGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: Spacing.two,
  },
  techSection: {
    gap: Spacing.two,
  },
  techText: {
    fontSize: 11,
  },
  title: {
    fontSize: 32,
    lineHeight: 38,
    marginTop: Spacing.two,
  },
  versionPill: {
    borderRadius: 8,
    borderWidth: 1,
    marginTop: Spacing.three,
    paddingHorizontal: Spacing.three,
    paddingVertical: Spacing.one,
  },
});
