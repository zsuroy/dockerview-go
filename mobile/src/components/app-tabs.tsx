import { Ionicons } from '@expo/vector-icons';
import React, { useState } from 'react';
import { Platform, StyleSheet, TouchableOpacity, View } from 'react-native';

import { ThemedText } from './themed-text';
import { useTheme } from '@/hooks/use-theme';
import { useTranslation } from '@/utils/i18n';

// Import the actual screens
import DashboardScreen from '@/app/index';
import SettingsScreen from '@/app/settings';
import AboutScreen from '@/app/about';

export default function AppTabs() {
  const [activeTab, setActiveTab] = useState<'dashboard' | 'settings' | 'about'>('dashboard');
  const { t } = useTranslation();
  const theme = useTheme();

  const renderActiveScreen = () => {
    switch (activeTab) {
      case 'dashboard':
        return <DashboardScreen />;
      case 'settings':
        return <SettingsScreen />;
      case 'about':
        return <AboutScreen />;
    }
  };

  return (
    <View style={{ flex: 1, backgroundColor: theme.background }}>
      <View style={{ flex: 1 }}>
        {renderActiveScreen()}
      </View>

      <View style={styles.tabBarContainer}>
        <View
          style={[
            styles.tabBar,
            {
              backgroundColor: theme.backgroundElement,
              borderColor: theme.backgroundSelected,
            },
          ]}>
          <TabItem
            iconName={activeTab === 'dashboard' ? 'grid' : 'grid-outline'}
            label={t('dashboard')}
            isActive={activeTab === 'dashboard'}
            onPress={() => setActiveTab('dashboard')}
          />
          <TabItem
            iconName={activeTab === 'settings' ? 'settings' : 'settings-outline'}
            label={t('settings')}
            isActive={activeTab === 'settings'}
            onPress={() => setActiveTab('settings')}
          />
          <TabItem
            iconName={activeTab === 'about' ? 'information-circle' : 'information-circle-outline'}
            label={t('about')}
            isActive={activeTab === 'about'}
            onPress={() => setActiveTab('about')}
          />
        </View>
      </View>
    </View>
  );
}

interface TabItemProps {
  iconName: keyof typeof Ionicons.glyphMap;
  label: string;
  isActive: boolean;
  onPress: () => void;
}

function TabItem({ iconName, label, isActive, onPress }: TabItemProps) {
  const theme = useTheme();
  return (
    <TouchableOpacity style={[styles.tabItem, isActive && { backgroundColor: theme.backgroundSelected }]} onPress={onPress} activeOpacity={0.75}>
      <Ionicons name={iconName} size={20} color={isActive ? theme.primary : theme.textSecondary} />
      <ThemedText
        type="smallBold"
        style={[
          styles.tabLabel,
          { color: isActive ? theme.primary : theme.textSecondary },
        ]}>
        {label}
      </ThemedText>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  tabBarContainer: {
    position: 'absolute',
    bottom: Platform.OS === 'ios' ? 18 : 14,
    left: 16,
    right: 16,
    alignItems: 'center',
    justifyContent: 'center',
    zIndex: 999,
  },
  tabBar: {
    flexDirection: 'row',
    width: '100%',
    maxWidth: 500,
    height: 58,
    borderRadius: 12,
    borderWidth: 1,
    padding: 5,
    gap: 5,
    alignItems: 'center',
    ...Platform.select({
      ios: {
        shadowColor: '#000',
        shadowOffset: { width: 0, height: 8 },
        shadowOpacity: 0.14,
        shadowRadius: 14,
      },
      android: {
        elevation: 6,
      },
      web: {
        shadowColor: '#000',
        shadowOffset: { width: 0, height: 8 },
        shadowOpacity: 0.08,
        shadowRadius: 16,
      },
    }),
  },
  tabItem: {
    alignItems: 'center',
    justifyContent: 'center',
    height: '100%',
    borderRadius: 8,
    flex: 1,
  },
  tabLabel: {
    fontSize: 10,
    marginTop: 1,
  },
});
