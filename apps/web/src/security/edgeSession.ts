import type { Role } from '../domain/types';

const storageKey = 'feastcloud.edge-session.v1';

export interface EdgeSession {
  accessToken: string;
  role: Role;
  expiresAt: string;
}

export function loadEdgeSession(): EdgeSession | undefined {
  try {
    const raw = localStorage.getItem(storageKey);
    if (!raw) return undefined;
    const value = JSON.parse(raw) as Partial<EdgeSession>;
    if (
      typeof value.accessToken !== 'string'
      || (value.role !== 'manager' && value.role !== 'cashier' && value.role !== 'chef')
      || typeof value.expiresAt !== 'string'
      || Date.parse(value.expiresAt) <= Date.now()
    ) {
      localStorage.removeItem(storageKey);
      return undefined;
    }
    return value as EdgeSession;
  } catch {
    return undefined;
  }
}

export function edgeAuthorizationHeaders(): Record<string, string> {
  const session = loadEdgeSession();
  return session ? { Authorization: `Bearer ${session.accessToken}` } : {};
}

export async function pairWithEdge(apiBase: string, code: string): Promise<EdgeSession> {
  const response = await fetch(`${apiBase}/pairing/sessions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code: code.replace(/\s/g, '') }),
  });
  if (!response.ok) throw new Error('The pairing code is invalid or expired.');
  const body = await response.json() as { data?: Partial<EdgeSession> };
  const value = body.data;
  if (!value?.accessToken || !value.expiresAt || !value.role) throw new Error('The edge returned an invalid session.');
  const session = value as EdgeSession;
  localStorage.setItem(storageKey, JSON.stringify(session));
  return session;
}

export function clearEdgeSession(): void {
  localStorage.removeItem(storageKey);
}
