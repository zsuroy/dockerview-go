// Types for the WeCom smart-robot console. They mirror the JSON shapes in
// internal/wecom/transcript.go and internal/server/wecom_handlers.go.

/** Connection state. The brief names disconnected / mock / connected;
 *  connecting and error are included so "not connected" and "trying" and
 *  "failed" are distinguishable. */
export type WeComState = 'disconnected' | 'connecting' | 'connected' | 'mock' | 'error';

/** Transport mode resolved from configuration. */
export type WeComMode = 'off' | 'mock' | 'live';

export type WeComEntryKind = 'inbound' | 'outbound' | 'event';

export interface WeComEntry {
  time: string;
  kind: WeComEntryKind;
  source: string;
  channel?: string;
  user?: string;
  text: string;
}

export interface WeComCounts {
  inbound: number;
  outbound: number;
  event: number;
}

export interface WeComSnapshot {
  enabled: boolean;
  mode: WeComMode;
  state: WeComState;
  state_reason?: string;
  ws_url: string;
  bot_id_masked?: string;
  has_secret: boolean;
  reply_mode: string;
  inject_allowed: boolean;
  last_activity?: string;
  counts: WeComCounts;
  recent: WeComEntry[];
  group_webhook_enabled: boolean;
}

export interface WeComInjectResult {
  status: string;
  inbound?: WeComEntry;
}
