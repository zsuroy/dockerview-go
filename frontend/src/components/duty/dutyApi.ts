import { basePath } from '../../utils';
import type { AskResult, DutyConfig, Ticket } from './dutyTypes';

export async function fetchDutyConfig(token: string): Promise<DutyConfig> {
  const res = await fetch(`${basePath}api/duty/config?token=${encodeURIComponent(token)}`);
  if (!res.ok) return { enabled: false };
  return res.json();
}

export async function askDuty(question: string, token: string): Promise<AskResult> {
  const res = await fetch(`${basePath}api/duty/ask?token=${encodeURIComponent(token)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ question }),
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `Duty ask failed (${res.status})`);
  }
  return res.json();
}

export async function confirmDutyWrite(
  ticketId: number,
  op: string,
  containerId: string,
  token: string
): Promise<{ status: string; op: string; request_id: string }> {
  const res = await fetch(`${basePath}api/duty/confirm?token=${encodeURIComponent(token)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ticket_id: ticketId, op, id: containerId, confirm: true }),
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `Confirm failed (${res.status})`);
  }
  return res.json();
}

export async function fetchDutyTickets(token: string): Promise<{ tickets: Ticket[]; total: number }> {
  const res = await fetch(`${basePath}api/duty/tickets?token=${encodeURIComponent(token)}&limit=50`);
  if (!res.ok) return { tickets: [], total: 0 };
  return res.json();
}
