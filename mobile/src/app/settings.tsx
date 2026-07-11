import { Ionicons } from '@expo/vector-icons';
import React, { useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  Platform,
  ScrollView,
  StyleSheet,
  TextInput,
  TouchableOpacity,
  View,
} from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { ThemedText } from '@/components/themed-text';
import { BottomTabInset, MaxContentWidth, Spacing } from '@/constants/theme';
import { useTheme } from '@/hooks/use-theme';
import { fetchContainers } from '@/utils/api';
import { Language, useTranslation } from '@/utils/i18n';
import { getAuthToken, getServerUrl, saveAuthToken, saveServerUrl } from '@/utils/storage';

function FieldLabel({ icon, label }: { icon: keyof typeof Ionicons.glyphMap; label: string }) {
  const theme = useTheme();
  return (
    <View style={styles.fieldLabel}>
      <Ionicons name={icon} size={15} color={theme.primary} style={styles.fieldLabelIcon} />
      <ThemedText type="smallBold" style={styles.fieldLabelText}>
        {label}
      </ThemedText>
    </View>
  );
}

function Section({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle?: string;
  children: React.ReactNode;
}) {
  const theme = useTheme();
  return (
    <View style={styles.section}>
      <View style={styles.sectionHeader}>
        <ThemedText type="smallBold" style={styles.sectionTitle}>
          {title}
        </ThemedText>
        {subtitle ? (
          <ThemedText type="small" themeColor="textSecondary" style={styles.sectionSubtitle}>
            {subtitle}
          </ThemedText>
        ) : null}
      </View>
      <View style={[styles.card, { backgroundColor: theme.backgroundElement, borderColor: theme.border }]}>
        {children}
      </View>
    </View>
  );
}

export default function SettingsScreen() {
  const insets = useSafeAreaInsets();
  const theme = useTheme();
  const { t, lang, setLanguage } = useTranslation();
  const [serverUrl, setServerUrlState] = useState('');
  const [authToken, setAuthTokenState] = useState('');
  const [hideToken, setHideToken] = useState(true);
  const [testing, setTesting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null);

  useEffect(() => {
    let active = true;
    Promise.all([getServerUrl(), getAuthToken()]).then(([url, token]) => {
      if (!active) return;
      setServerUrlState(url);
      setAuthTokenState(token);
    });
    return () => {
      active = false;
    };
  }, []);

  const notify = (title: string, message: string) => {
    if (Platform.OS === 'web') {
      alert(`${title}: ${message}`);
    } else {
      Alert.alert(title, message);
    }
  };

  const handleSave = async () => {
    if (!serverUrl.trim()) {
      notify('Error', 'Server URL is required');
      return;
    }
    setSaving(true);
    try {
      await saveServerUrl(serverUrl.trim());
      await saveAuthToken(authToken.trim());
      notify('Success', t('savedSuccess'));
    } finally {
      setSaving(false);
    }
  };

  const handleTestConnection = async () => {
    if (!serverUrl.trim()) {
      setTestResult({ success: false, message: 'Server URL is required to test connection.' });
      return;
    }
    setTesting(true);
    setTestResult(null);
    try {
      const containers = await fetchContainers(serverUrl.trim(), authToken.trim());
      setTestResult({
        success: true,
        message: `${t('connSuccess')} · ${containers.length} containers visible.`,
      });
    } catch (error: any) {
      setTestResult({
        success: false,
        message: error?.message || 'Connection failed. Please check the URL and token.',
      });
    } finally {
      setTesting(false);
    }
  };

  const changeLanguage = async (nextLang: Language) => {
    await setLanguage(nextLang);
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
          <View style={styles.header}>
            <ThemedText type="subtitle" style={styles.title}>
              {t('settings')}
            </ThemedText>
            <ThemedText type="small" themeColor="textSecondary">
              {t('serverDesc')}
            </ThemedText>
          </View>

          <Section title="Server" subtitle="DockerView-Go endpoint and token">
            <View style={styles.formStack}>
              <View style={styles.field}>
                <FieldLabel icon="server-outline" label={t('hostAddress')} />
                <TextInput
                  style={[styles.input, { backgroundColor: theme.surfaceMuted, color: theme.text, borderColor: theme.border }]}
                  placeholder={t('hostPlaceholder')}
                  placeholderTextColor={theme.textSecondary}
                  value={serverUrl}
                  onChangeText={setServerUrlState}
                  autoCapitalize="none"
                  autoCorrect={false}
                  keyboardType="url"
                />
                <ThemedText type="small" themeColor="textSecondary" style={styles.hint}>
                  {t('hostHint')}
                </ThemedText>
              </View>

              <View style={styles.field}>
                <FieldLabel icon="key-outline" label={t('token')} />
                <View style={styles.inputRow}>
                  <TextInput
                    style={[
                      styles.input,
                      styles.inputWithButton,
                      { backgroundColor: theme.surfaceMuted, color: theme.text, borderColor: theme.border },
                    ]}
                    placeholder={t('tokenPlaceholder')}
                    placeholderTextColor={theme.textSecondary}
                    value={authToken}
                    onChangeText={setAuthTokenState}
                    autoCapitalize="none"
                    autoCorrect={false}
                    secureTextEntry={hideToken}
                  />
                  <TouchableOpacity style={styles.eyeButton} onPress={() => setHideToken((value) => !value)}>
                    <Ionicons name={hideToken ? 'eye-outline' : 'eye-off-outline'} size={20} color={theme.textSecondary} />
                  </TouchableOpacity>
                </View>
                <ThemedText type="small" themeColor="textSecondary" style={styles.hint}>
                  {t('tokenHint')}
                </ThemedText>
              </View>

              <View style={styles.buttonRow}>
                <TouchableOpacity
                  style={[styles.primaryButton, { backgroundColor: theme.primary }, saving && styles.disabledButton]}
                  onPress={handleSave}
                  disabled={saving}>
                  {saving ? <ActivityIndicator color="#ffffff" size="small" /> : <Ionicons name="save-outline" size={17} color="#ffffff" />}
                  <ThemedText type="smallBold" style={styles.primaryButtonText}>
                    {t('save')}
                  </ThemedText>
                </TouchableOpacity>
                <TouchableOpacity
                  style={[styles.secondaryButton, { backgroundColor: theme.surfaceMuted }, testing && styles.disabledButton]}
                  onPress={handleTestConnection}
                  disabled={testing}>
                  {testing ? <ActivityIndicator color={theme.text} size="small" /> : <Ionicons name="pulse-outline" size={17} color={theme.text} />}
                  <ThemedText type="smallBold">{t('testConn')}</ThemedText>
                </TouchableOpacity>
              </View>
            </View>
          </Section>

          {testResult ? (
            <View
              style={[
                styles.resultCard,
                {
                  backgroundColor: testResult.success ? `${theme.success}1f` : `${theme.danger}1f`,
                  borderColor: testResult.success ? theme.success : theme.danger,
                },
              ]}>
              <Ionicons
                name={testResult.success ? 'checkmark-circle-outline' : 'alert-circle-outline'}
                size={20}
                color={testResult.success ? theme.success : theme.danger}
              />
              <ThemedText type="small" style={{ color: testResult.success ? theme.success : theme.danger, flex: 1 }}>
                {testResult.message}
              </ThemedText>
            </View>
          ) : null}

          <Section title="Preferences">
            <View style={styles.preferenceRow}>
              <View style={styles.preferenceCopy}>
                <FieldLabel icon="language-outline" label={t('language')} />
                <ThemedText type="small" themeColor="textSecondary">
                  {lang === 'zh' ? t('langZh') : t('langEn')}
                </ThemedText>
              </View>
              <View style={[styles.segmented, { backgroundColor: theme.surfaceMuted }]}>
                <TouchableOpacity
                  style={[styles.segment, lang === 'zh' && { backgroundColor: theme.primary }]}
                  onPress={() => changeLanguage('zh')}>
                  <ThemedText type="smallBold" style={{ color: lang === 'zh' ? '#ffffff' : theme.textSecondary }}>
                    中文
                  </ThemedText>
                </TouchableOpacity>
                <TouchableOpacity
                  style={[styles.segment, lang === 'en' && { backgroundColor: theme.primary }]}
                  onPress={() => changeLanguage('en')}>
                  <ThemedText type="smallBold" style={{ color: lang === 'en' ? '#ffffff' : theme.textSecondary }}>
                    EN
                  </ThemedText>
                </TouchableOpacity>
              </View>
            </View>
          </Section>
        </View>
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  buttonRow: {
    flexDirection: 'row',
    gap: Spacing.two,
  },
  card: {
    borderRadius: 8,
    borderWidth: 1,
    padding: Spacing.three,
  },
  content: {
    alignItems: 'center',
    paddingHorizontal: Spacing.three,
  },
  disabledButton: {
    opacity: 0.65,
  },
  eyeButton: {
    alignItems: 'center',
    height: 46,
    justifyContent: 'center',
    position: 'absolute',
    right: Spacing.two,
    width: 44,
  },
  field: {
    gap: Spacing.two,
  },
  fieldLabel: {
    alignItems: 'flex-start',
    flexDirection: 'row',
    gap: Spacing.one,
  },
  fieldLabelIcon: {
    marginTop: 1,
  },
  fieldLabelText: {
    flex: 1,
    flexWrap: 'wrap',
  },
  formStack: {
    gap: Spacing.three,
  },
  header: {
    gap: Spacing.one,
  },
  hint: {
    fontSize: 12,
    lineHeight: 17,
  },
  inner: {
    gap: Spacing.four,
    maxWidth: MaxContentWidth,
    width: '100%',
  },
  input: {
    borderRadius: 8,
    borderWidth: 1,
    fontSize: 14,
    minHeight: 46,
    paddingHorizontal: Spacing.three,
  },
  inputRow: {
    position: 'relative',
  },
  inputWithButton: {
    paddingRight: 54,
  },
  preferenceCopy: {
    flex: 1,
    gap: Spacing.one,
    minWidth: 0,
  },
  preferenceRow: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: Spacing.three,
  },
  primaryButton: {
    alignItems: 'center',
    borderRadius: 8,
    flex: 1,
    flexDirection: 'row',
    gap: Spacing.one,
    justifyContent: 'center',
    minHeight: 46,
  },
  primaryButtonText: {
    color: '#ffffff',
  },
  resultCard: {
    alignItems: 'center',
    borderRadius: 8,
    borderWidth: 1,
    flexDirection: 'row',
    gap: Spacing.two,
    padding: Spacing.three,
  },
  screen: {
    flex: 1,
  },
  scrollView: {
    flex: 1,
  },
  secondaryButton: {
    alignItems: 'center',
    borderRadius: 8,
    flex: 1,
    flexDirection: 'row',
    gap: Spacing.one,
    justifyContent: 'center',
    minHeight: 46,
  },
  section: {
    gap: Spacing.two,
  },
  sectionHeader: {
    gap: 2,
  },
  sectionSubtitle: {
    fontSize: 12,
  },
  sectionTitle: {
    fontSize: 15,
  },
  segment: {
    alignItems: 'center',
    borderRadius: 7,
    justifyContent: 'center',
    minHeight: 34,
    minWidth: 48,
    paddingHorizontal: Spacing.two,
  },
  segmented: {
    borderRadius: 8,
    flexDirection: 'row',
    padding: 3,
  },
  title: {
    fontSize: 30,
    lineHeight: 36,
  },
});
