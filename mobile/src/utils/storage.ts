import AsyncStorage from '@react-native-async-storage/async-storage';

const SERVER_URL_KEY = '@dockerview/server_url';
const AUTH_TOKEN_KEY = '@dockerview/auth_token';

// Default to a typical host for localhost or empty
const DEFAULT_SERVER_URL = 'http://127.0.0.1:8080';

export async function getServerUrl(): Promise<string> {
  try {
    const value = await AsyncStorage.getItem(SERVER_URL_KEY);
    return value || DEFAULT_SERVER_URL;
  } catch (e) {
    console.error('Failed to load server URL', e);
    return DEFAULT_SERVER_URL;
  }
}

export async function saveServerUrl(url: string): Promise<void> {
  try {
    // Normalize url (remove trailing slash)
    const normalized = url.trim().replace(/\/+$/, '');
    await AsyncStorage.setItem(SERVER_URL_KEY, normalized);
  } catch (e) {
    console.error('Failed to save server URL', e);
  }
}

export async function getAuthToken(): Promise<string> {
  try {
    const value = await AsyncStorage.getItem(AUTH_TOKEN_KEY);
    return value || '';
  } catch (e) {
    console.error('Failed to load auth token', e);
    return '';
  }
}

export async function saveAuthToken(token: string): Promise<void> {
  try {
    await AsyncStorage.setItem(AUTH_TOKEN_KEY, token.trim());
  } catch (e) {
    console.error('Failed to save auth token', e);
  }
}
