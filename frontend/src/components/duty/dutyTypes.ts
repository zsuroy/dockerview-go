export interface ToolTrace {
  tool: string;
  input: string;
  output_excerpt: string;
  evidence?: string;
}

export interface PreviewResult {
  id: string;
  name: string;
  status: string;
  health_score: number;
  op: string;
  impact: string;
}

export interface AskResult {
  answer: string;
  tool_traces: ToolTrace[];
  ticket_id: number;
  proposed_write?: PreviewResult;
}

export interface Ticket {
  id: number;
  time: string;
  actor: string;
  actor_kind: string;
  source: string;
  question: string;
  tool_summary: ToolTrace[];
  conclusion: string;
  write_confirmed: boolean;
  write_action: string;
  related_container: string;
  request_id: string;
}

export interface DutyConfig {
  enabled: boolean;
  mode?: 'live' | 'fake';
  model?: string;
  base_url?: string;
  has_key?: boolean;
}

export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant';
  text: string;
  traces?: ToolTrace[];
  proposedWrite?: PreviewResult;
  ticketId?: number;
  timestamp: number;
}
