import AsyncStorage from '@react-native-async-storage/async-storage';
import { useState, useEffect } from 'react';
import { NativeModules, Platform } from 'react-native';

import { en, TranslationKey } from './en';
import { zh } from './zh';

const LANG_STORAGE_KEY = '@dockerview/language';

export type Language = 'zh' | 'en';

export const translations = { zh, en };

const listeners = new Set<(lang: Language) => void>();

export function getSystemLanguage(): Language {
  try {
    const locale =
      Platform.OS === 'ios'
        ? NativeModules.SettingsManager?.settings?.AppleLocale ||
          NativeModules.SettingsManager?.settings?.AppleLanguages?.[0]
        : NativeModules.I18nManager?.localeIdentifier;

    if (locale && (locale.startsWith('zh') || locale.includes('zh-'))) {
      return 'zh';
    }
  } catch {
    // Ignore
  }
  return 'en';
}

let currentLanguage: Language = 'zh';

AsyncStorage.getItem(LANG_STORAGE_KEY).then((lang) => {
  if (lang === 'zh' || lang === 'en') {
    currentLanguage = lang;
  } else {
    currentLanguage = getSystemLanguage();
  }
  listeners.forEach((l) => l(currentLanguage));
});

export function getCurrentLanguage(): Language {
  return currentLanguage;
}

export async function setLanguage(lang: Language): Promise<void> {
  currentLanguage = lang;
  await AsyncStorage.setItem(LANG_STORAGE_KEY, lang);
  listeners.forEach((l) => l(lang));
}

export function useTranslation() {
  const [lang, setLang] = useState<Language>(currentLanguage);

  useEffect(() => {
    const listener = (newLang: Language) => setLang(newLang);
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  }, []);

  const t = (key: TranslationKey): string => {
    return translations[lang][key] || translations.en[key] || String(key);
  };

  return { t, lang, setLanguage };
}
