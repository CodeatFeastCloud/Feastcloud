// SPDX-License-Identifier: AGPL-3.0-only

import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const databaseUrl = process.env.FEASTCLOUD_TEST_DATABASE_URL;
if (!databaseUrl) {
  console.log('PostgreSQL smoke skipped: FEASTCLOUD_TEST_DATABASE_URL is not set.');
  process.exit(0);
}

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const tenantId = '11111111-1111-4111-8111-111111111111';
const outletId = '33333333-3333-4333-8333-333333333333';
const pause = (milliseconds) => new Promise((resolvePause) => setTimeout(resolvePause, milliseconds));
const uuidV7 = () => {
  const value = randomUUID();
  return `${value.slice(0, 14)}7${value.slice(15)}`;
};

async function freePort() {
  const server = createServer();
  await new Promise((resolveListen, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolveListen);
  });
  const address = server.address();
  assert(address && typeof address === 'object');
  await new Promise((resolveClose, reject) => server.close((error) => (error ? reject(error) : resolveClose())));
  return address.port;
}

async function build(command, args, options) {
  const child = spawn(command, args, { ...options, stdio: ['ignore', 'pipe', 'pipe'] });
  let output = '';
  child.stdout.on('data', (chunk) => { output += chunk; });
  child.stderr.on('data', (chunk) => { output += chunk; });
  const code = await new Promise((resolveExit, reject) => {
    child.once('error', reject);
    child.once('exit', resolveExit);
  });
  if (code !== 0) throw new Error(`${command} ${args.join(' ')} failed (${code})\n${output}`);
}

function start(command, args, options, name) {
  const child = spawn(command, args, { ...options, stdio: ['ignore', 'pipe', 'pipe'] });
  const service = { child, name, output: '' };
  child.stdout.on('data', (chunk) => { service.output = `${service.output}${chunk}`.slice(-12000); });
  child.stderr.on('data', (chunk) => { service.output = `${service.output}${chunk}`.slice(-12000); });
  return service;
}

async function stop(service) {
  if (!service || service.child.exitCode !== null) return;
  service.child.kill('SIGTERM');
  await Promise.race([
    new Promise((resolveExit) => service.child.once('exit', resolveExit)),
    pause(5000).then(() => {
      if (service.child.exitCode === null) service.child.kill('SIGKILL');
    }),
  ]);
}

async function waitFor(name, service, check, timeoutMilliseconds = 20000) {
  const deadline = Date.now() + timeoutMilliseconds;
  let lastError;
  while (Date.now() < deadline) {
    if (service?.child.exitCode !== null) {
      throw new Error(`${name} exited early (${service.child.exitCode})\n${service.output}`);
    }
    try {
      const result = await check();
      if (result) return result;
    } catch (error) {
      lastError = error;
    }
    await pause(100);
  }
  throw new Error(`${name} timed out: ${lastError?.message ?? 'condition not met'}\n${service?.output ?? ''}`);
}

async function json(response, expectedStatus = 200) {
  const text = await response.text();
  assert.equal(response.status, expectedStatus, `unexpected HTTP ${response.status}: ${text}`);
  return JSON.parse(text);
}

function envelope(id, idempotencyKey, payload) {
  return {
    id,
    tenantId,
    outletId,
    deviceId: 'pwa_postgres_smoke',
    actorId: 'cashier_postgres_smoke',
    occurredAt: new Date().toISOString(),
    source: 'feastcloud-pwa',
    sourceId: idempotencyKey,
    schemaVersion: '1.0',
    idempotencyKey,
    payload,
  };
}

async function postMutation(edgeUrl, idempotencyKey, body, expectedStatus) {
  return fetch(`${edgeUrl}/api/v1/sync/mutations`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify(body),
  }).then((response) => json(response, expectedStatus));
}

async function main() {
  const temporaryDirectory = await mkdtemp(join(tmpdir(), 'feastcloud-postgres-smoke-'));
  const coreBinary = join(temporaryDirectory, 'feastcloud-core');
  const edgeBinary = join(temporaryDirectory, 'feastcloud-edge');
  const edgeDatabase = join(temporaryDirectory, 'edge.db');
  const [corePort, edgePort] = await Promise.all([freePort(), freePort()]);
  const coreUrl = `http://127.0.0.1:${corePort}`;
  const edgeUrl = `http://127.0.0.1:${edgePort}`;
  const ids = Object.fromEntries(
    ['createOperation', 'transitionOperation', 'order', 'line', 'ticket'].map((key) => [key, uuidV7()]),
  );
  let core;
  let edge;

  const edgeEnvironment = {
    ...process.env,
    FEASTCLOUD_EDGE_ID: `edge_postgres_smoke_${ids.order}`,
    FEASTCLOUD_TENANT_ID: tenantId,
    FEASTCLOUD_OUTLET_ID: outletId,
    FEASTCLOUD_EDGE_LISTEN: `127.0.0.1:${edgePort}`,
    FEASTCLOUD_EDGE_DATABASE: edgeDatabase,
    FEASTCLOUD_EDGE_ALLOWED_ORIGIN: 'http://localhost:5173',
    FEASTCLOUD_CLOUD_URL: coreUrl,
    FEASTCLOUD_SYNC_INTERVAL: '100ms',
  };

  try {
    await Promise.all([
      build('go', ['build', '-o', coreBinary, './cmd/core'], {
        cwd: join(repositoryRoot, 'services/core'), env: process.env,
      }),
      build('go', ['build', '-o', edgeBinary, './cmd/feastcloud-edge'], {
        cwd: join(repositoryRoot, 'services/edge'), env: process.env,
      }),
    ]);

    core = start(coreBinary, [`-addr=127.0.0.1:${corePort}`], {
      cwd: temporaryDirectory,
      env: { ...process.env, FEASTCLOUD_DATABASE_URL: databaseUrl },
    }, 'core');
    await waitFor('PostgreSQL-backed core readiness', core, async () => (await fetch(`${coreUrl}/readyz`)).ok);

    edge = start(edgeBinary, [], { cwd: temporaryDirectory, env: edgeEnvironment }, 'edge');
    await waitFor('edge readiness', edge, async () => (await fetch(`${edgeUrl}/readyz`)).ok);

    const createKey = `postgres-smoke-create-${ids.createOperation}`;
    const createBody = envelope(ids.createOperation, createKey, {
      eventType: 'com.feastcloud.order.created.v1',
      aggregateType: 'order',
      aggregateId: ids.order,
      order: {
        id: ids.order,
        type: 'takeaway',
        guestName: 'PostgreSQL smoke guest',
        tableLabel: 'Counter 1',
        placedAt: new Date().toISOString(),
        stationTicketIds: { hot: ids.ticket },
        lines: [{
          id: ids.line,
          menuItemId: 'dal-bowl',
          name: 'Dal bowl',
          quantity: 1,
          stationId: 'hot',
          preparationNote: 'Terminal sync proof',
        }],
      },
    });
    const created = await postMutation(edgeUrl, createKey, createBody, 201);
    assert.equal(created.data.order.id, ids.order);
    assert.equal(created.data.tickets[0].id, ids.ticket);

    await waitFor('durable order synchronization', edge, async () => {
      const status = await json(await fetch(`${edgeUrl}/api/v1/sync/status`));
      return status.data.state === 'synchronized' &&
        status.data.outbox.pending === 0 &&
        status.data.outbox.reconciliation === 0 &&
        status.data.outbox.synchronized === 1 && status;
    });

    const replayResponse = await fetch(`${edgeUrl}/api/v1/sync/mutations`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Idempotency-Key': createKey },
      body: JSON.stringify(createBody),
    });
    await json(replayResponse, 201);
    assert.equal(replayResponse.headers.get('idempotency-replayed'), 'true');

    const transitionKey = `postgres-smoke-fire-${ids.transitionOperation}`;
    const transitioned = await postMutation(edgeUrl, transitionKey, envelope(ids.transitionOperation, transitionKey, {
      eventType: 'com.feastcloud.kitchen-ticket.status-changed.v1',
      aggregateType: 'kitchenTicket',
      aggregateId: ids.ticket,
      ticketId: ids.ticket,
      orderId: ids.order,
      toStatus: 'fired',
      expectedVersion: 1,
    }), 200);
    assert.equal(transitioned.data.ticket.status, 'fired');

    await waitFor('durable KDS transition synchronization', edge, async () => {
      const status = await json(await fetch(`${edgeUrl}/api/v1/sync/status`));
      return status.data.state === 'synchronized' &&
        status.data.outbox.pending === 0 &&
        status.data.outbox.synchronized === 2 && status;
    });

    await stop(edge);
    edge = start(edgeBinary, [], { cwd: temporaryDirectory, env: edgeEnvironment }, 'restarted edge');
    await waitFor('restarted edge readiness', edge, async () => (await fetch(`${edgeUrl}/readyz`)).ok);
    const persisted = await json(await fetch(`${edgeUrl}/api/v1/orders/${ids.order}`));
    assert.equal(persisted.data.guestName, 'PostgreSQL smoke guest');
    assert.equal(persisted.data.version, 2);
    const restartStatus = await waitFor('restart synchronization state', edge, async () => {
      const status = await json(await fetch(`${edgeUrl}/api/v1/sync/status`));
      return status.data.state === 'synchronized' && status.data.outbox.synchronized === 2 && status;
    });
    assert.equal(restartStatus.data.outbox.pending, 0);

    console.log(`PostgreSQL smoke passed: order ${ids.order} and KDS transition committed, replayed safely, and survived edge restart.`);
  } catch (error) {
    if (edge?.output) console.error(`\nEdge log:\n${edge.output}`);
    if (core?.output) console.error(`\nCore log:\n${core.output}`);
    throw error;
  } finally {
    await Promise.all([stop(edge), stop(core)]);
    await rm(temporaryDirectory, { recursive: true, force: true });
  }
}

await main();
