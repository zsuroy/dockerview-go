/**
 * Below are the colors that are used in the app. The colors are defined in the light and dark mode.
 * There are many other ways to style your app. For example, [Nativewind](https://www.nativewind.dev/), [Tamagui](https://tamagui.dev/), [unistyles](https://reactnativeunistyles.vercel.app), etc.
 */

import '@/global.css';

import { Platform } from 'react-native';

export const Colors = {
  light: {
    text: '#17202A',
    background: '#F7FAFC',
    backgroundElement: '#FFFFFF',
    backgroundSelected: '#E8EEF5',
    textSecondary: '#5F6B7A',
    border: '#DCE4EE',
    surfaceMuted: '#F0F5FA',
    primary: '#2563EB',
    success: '#16A34A',
    warning: '#D97706',
    danger: '#DC2626',
    console: '#07111F',
  },
  dark: {
    text: '#F5F7FA',
    background: '#080B10',
    backgroundElement: '#151A21',
    backgroundSelected: '#252C36',
    textSecondary: '#AAB4C0',
    border: '#2D3642',
    surfaceMuted: '#10151C',
    primary: '#60A5FA',
    success: '#34D399',
    warning: '#FBBF24',
    danger: '#F87171',
    console: '#06101D',
  },
} as const;

export type ThemeColor = keyof typeof Colors.light & keyof typeof Colors.dark;

export const Fonts = Platform.select({
  ios: {
    /** iOS `UIFontDescriptorSystemDesignDefault` */
    sans: 'system-ui',
    /** iOS `UIFontDescriptorSystemDesignSerif` */
    serif: 'ui-serif',
    /** iOS `UIFontDescriptorSystemDesignRounded` */
    rounded: 'ui-rounded',
    /** iOS `UIFontDescriptorSystemDesignMonospaced` */
    mono: 'ui-monospace',
  },
  default: {
    sans: 'normal',
    serif: 'serif',
    rounded: 'normal',
    mono: 'monospace',
  },
  web: {
    sans: 'var(--font-display)',
    serif: 'var(--font-serif)',
    rounded: 'var(--font-rounded)',
    mono: 'var(--font-mono)',
  },
});

export const Spacing = {
  half: 2,
  one: 4,
  two: 8,
  three: 16,
  four: 24,
  five: 32,
  six: 64,
} as const;

export const BottomTabInset = Platform.select({ ios: 76, android: 76, web: 76 }) ?? 76;
export const MaxContentWidth = 800;
