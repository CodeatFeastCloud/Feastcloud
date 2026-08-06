import type { OutboxEvent } from '../domain/types';
import {
  acknowledgeOutboxEvent,
  incrementOutboxAttempt,
  listOutbox,
  quarantineOutboxEvent,
} from '../persistence/offlineStore';
import { edgeAuthorizationHeaders } from '../security/edgeSession';

interface ProblemDocument {
  code?: string;
  detail?: string;
  retryable?: boolean;
}

export class OutboxTransmissionError extends Error {
  readonly permanent: boolean;
  readonly status?: number;

  constructor(message: string, options: { permanent: boolean; status?: number; cause?: unknown }) {
    super(message, options.cause === undefined ? undefined : { cause: options.cause });
    this.name = 'OutboxTransmissionError';
    this.permanent = options.permanent;
    this.status = options.status;
  }
}

function isPermanentClientFailure(status: number, problem: ProblemDocument): boolean {
  if (problem.retryable) return false;
  // Authentication, authorization and throttling can recover after pairing or time.
  if ([401, 403, 408, 425, 429].includes(status)) return false;
  return status >= 400 && status < 500;
}

async function readProblem(response: Response): Promise<ProblemDocument> {
  try {
    const body = (await response.json()) as unknown;
    return body && typeof body === 'object' ? (body as ProblemDocument) : {};
  } catch {
    return {};
  }
}

export async function transmitOutboxEvent(
  event: OutboxEvent,
  apiBase?: string,
): Promise<void> {
  if (!apiBase) {
    await new Promise((resolve) => globalThis.setTimeout(resolve, 90));
    return;
  }

  const {
    attempts: _localAttempts,
    localSequence: _localSequence,
    disposition: _disposition,
    lastError: _lastError,
    ...mutationEnvelope
  } = event;
  let response: Response;
  try {
    response = await fetch(`${apiBase}/sync/mutations`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Idempotency-Key': event.idempotencyKey,
		...edgeAuthorizationHeaders(),
      },
      body: JSON.stringify(mutationEnvelope),
    });
  } catch (cause) {
    throw new OutboxTransmissionError('The outlet edge could not be reached', {
      permanent: false,
      cause,
    });
  }

  if (response.ok) return;
  const problem = await readProblem(response);
  const detail = problem.detail || problem.code || `Sync service returned ${response.status}`;
  throw new OutboxTransmissionError(detail, {
    permanent: isPermanentClientFailure(response.status, problem),
    status: response.status,
  });
}

export interface DrainResult {
  acknowledged: number;
  quarantined: number;
  transientError?: Error;
}

/**
 * Drains until empty so events appended during an active transmission are not
 * stranded. A permanent command failure is retained for reconciliation while
 * independent later commands continue. Transient failures preserve ordering.
 */
export async function drainPendingOutbox(
  transmit: (event: OutboxEvent) => Promise<void>,
): Promise<DrainResult> {
  const result: DrainResult = { acknowledged: 0, quarantined: 0 };

  while (true) {
    const events = await listOutbox();
    if (events.length === 0) return result;

    for (const event of events) {
      try {
        await transmit(event);
        await acknowledgeOutboxEvent(event.id);
        result.acknowledged += 1;
      } catch (error) {
        if (error instanceof OutboxTransmissionError && error.permanent) {
          await quarantineOutboxEvent(event, error.message);
          result.quarantined += 1;
          continue;
        }

        const transientError = error instanceof Error ? error : new Error('Sync failed');
        await incrementOutboxAttempt(event, transientError.message);
        return { ...result, transientError };
      }
    }
  }
}
