// SPDX-License-Identifier: AGPL-3.0-only

import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const tenantId = '11111111-1111-4111-8111-111111111111';
const outletId = '33333333-3333-4333-8333-333333333333';
const edgeId = 'edge_connected_smoke_01';

const ids = {
  createOperation: '0198a2b3-c4d5-7e6f-8a9b-0c1d2e3f4001',
  order: '0198a2b3-c4d5-7e6f-8a9b-0c1d2e3f4002',
  hotLine: '0198a2b3-c4d5-7e6f-8a9b-0c1d2e3f4003',
  beverageLine: '0198a2b3-c4d5-7e6f-8a9b-0c1d2e3f4004',
  ticketStatusOperation: '0198a2b3-c4d5-7e6f-8a9b-0c1d2e3f4005',
  statusOperation: '0198a2b3-c4d5-7e6f-8a9b-0c1d2e3f4006',
  hotTicket: '0198a2b3-c4d5-7e6f-8a9b-0c1d2e3f4007',
  beverageTicket: '0198a2b3-c4d5-7e6f-8a9b-0c1d2e3f4008',
};

const delay = (milliseconds) => new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds));

async function freePort() {
  const server = createServer();
  await new Promise((resolveListen, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolveListen);
  });
  const address = server.address();
  assert(address && typeof address === 'object');
  const { port } = address;
  await new Promise((resolveClose, reject) => server.close((error) => (error ? reject(error) : resolveClose())));
  return port;
}

async function run(command, args, options) {
  const child = spawn(command, args, { ...options, stdio: ['ignore', 'pipe', 'pipe'] });
  let output = '';
  child.stdout.on('data', (chunk) => { output += chunk; });
  child.stderr.on('data', (chunk) => { output += chunk; });
  const code = await new Promise((resolveExit, reject) => {
    child.once('error', reject);
    child.once('exit', resolveExit);
  });
  if (code !== 0) {
    throw new Error(`${command} ${args.join(' ')} failed with exit code ${code}\n${output}`);
  }
}

function startService(command, args, options, name) {
  const child = spawn(command, args, { ...options, stdio: ['ignore', 'pipe', 'pipe'] });
  const service = { child, name, output: '' };
  child.stdout.on('data', (chunk) => { service.output = `${service.output}${chunk}`.slice(-12000); });
  child.stderr.on('data', (chunk) => { service.output = `${service.output}${chunk}`.slice(-12000); });
  return service;
}

async function stopService(service) {
  if (!service || service.child.exitCode !== null) return;
  service.child.kill('SIGTERM');
  await Promise.race([
    new Promise((resolveExit) => service.child.once('exit', resolveExit)),
    delay(5000).then(() => {
      if (service.child.exitCode === null) service.child.kill('SIGKILL');
    }),
  ]);
}

async function waitFor(name, service, check, timeoutMilliseconds = 15000) {
  const deadline = Date.now() + timeoutMilliseconds;
  let lastError;
  while (Date.now() < deadline) {
    if (service?.child.exitCode !== null) {
      throw new Error(`${name} exited early with code ${service.child.exitCode}\n${service.output}`);
    }
    try {
      const result = await check();
      if (result) return result;
    } catch (error) {
      lastError = error;
    }
    await delay(100);
  }
  throw new Error(`${name} did not become ready: ${lastError?.message ?? 'condition not met'}\n${service?.output ?? ''}`);
}

async function responseJson(response, expectedStatus) {
  const text = await response.text();
  assert.equal(response.status, expectedStatus, `unexpected HTTP ${response.status}: ${text}`);
  return JSON.parse(text);
}

function mutationEnvelope(id, idempotencyKey, payload) {
  return {
    id,
    tenantId,
    outletId,
    deviceId: 'pwa_connected_smoke',
    actorId: 'cashier_connected_smoke',
    occurredAt: '2026-08-03T08:00:00Z',
    source: 'feastcloud-pwa',
    sourceId: idempotencyKey,
    schemaVersion: '1.0',
    idempotencyKey,
    payload,
  };
}

async function main() {
  const temporaryDirectory = await mkdtemp(join(tmpdir(), 'feastcloud-connected-smoke-'));
  const coreBinary = join(temporaryDirectory, 'feastcloud-core');
  const edgeBinary = join(temporaryDirectory, 'feastcloud-edge');
  const edgeDatabase = join(temporaryDirectory, 'edge.db');
  const [corePort, edgePort] = await Promise.all([freePort(), freePort()]);
  const coreUrl = `http://127.0.0.1:${corePort}`;
  const edgeUrl = `http://127.0.0.1:${edgePort}`;
  let core;
  let edge;

  try {
    await Promise.all([
      run('go', ['build', '-o', coreBinary, './cmd/core'], {
        cwd: join(repositoryRoot, 'services/core'),
        env: process.env,
      }),
      run('go', ['build', '-o', edgeBinary, './cmd/feastcloud-edge'], {
        cwd: join(repositoryRoot, 'services/edge'),
        env: process.env,
      }),
    ]);

    const edgeEnvironment = {
      ...process.env,
      FEASTCLOUD_EDGE_ID: edgeId,
      FEASTCLOUD_TENANT_ID: tenantId,
      FEASTCLOUD_OUTLET_ID: outletId,
      FEASTCLOUD_EDGE_LISTEN: `127.0.0.1:${edgePort}`,
      FEASTCLOUD_EDGE_DATABASE: edgeDatabase,
      FEASTCLOUD_EDGE_ALLOWED_ORIGIN: 'http://localhost:5173',
      FEASTCLOUD_CLOUD_URL: coreUrl,
      FEASTCLOUD_SYNC_INTERVAL: '100ms',
    };

    // Start the outlet first so the order is demonstrably accepted without WAN/cloud.
    edge = startService(edgeBinary, [], { cwd: temporaryDirectory, env: edgeEnvironment }, 'edge');
    await waitFor('edge', edge, async () => (await fetch(`${edgeUrl}/readyz`)).ok);

    const createKey = 'connected-smoke-create-order';
    const createBody = mutationEnvelope(ids.createOperation, createKey, {
      eventType: 'com.feastcloud.order.created.v1',
      aggregateType: 'order',
      aggregateId: ids.order,
      order: {
        id: ids.order,
        type: 'takeaway',
        guestName: 'Connected smoke guest',
        tableLabel: 'Counter 1',
        note: 'No cutlery',
        placedAt: '2026-08-03T08:00:00Z',
        stationTicketIds: { hot: ids.hotTicket, beverage: ids.beverageTicket },
        lines: [
          {
            id: ids.hotLine,
            menuItemId: 'dal-bowl',
            name: 'Dal bowl',
            quantity: 1,
            stationId: 'hot',
            preparationNote: 'Medium spice',
          },
          {
            id: ids.beverageLine,
            menuItemId: 'lassi',
            name: 'Lassi',
            quantity: 1,
            stationId: 'beverage',
          },
        ],
      },
    });
    const createPayload = JSON.stringify(createBody);
    const createResponse = await fetch(`${edgeUrl}/api/v1/sync/mutations`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Idempotency-Key': createKey },
      body: createPayload,
    });
    const created = await responseJson(createResponse, 201);
    assert.equal(created.data.order.id, ids.order);
    assert.equal(created.data.order.guestName, 'Connected smoke guest');
    assert.equal(created.data.tickets.length, 2);
    assert.deepEqual(new Set(created.data.tickets.map((ticket) => ticket.stationId)), new Set(['hot', 'beverage']));
    assert.deepEqual(new Set(created.data.tickets.map((ticket) => ticket.id)), new Set([ids.hotTicket, ids.beverageTicket]));

    const offlineStatus = await responseJson(await fetch(`${edgeUrl}/api/v1/sync/status`), 200);
    assert.equal(offlineStatus.data.outbox.pending, 1);
    assert.equal(offlineStatus.data.outbox.synchronized, 0);

    const corsResponse = await fetch(`${edgeUrl}/api/v1/orders`, {
      method: 'OPTIONS',
      headers: { Origin: 'http://localhost:5173', 'Access-Control-Request-Method': 'POST' },
    });
    assert.equal(corsResponse.status, 204);
    assert.equal(corsResponse.headers.get('access-control-allow-origin'), 'http://localhost:5173');

    const replayResponse = await fetch(`${edgeUrl}/api/v1/sync/mutations`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Idempotency-Key': createKey },
      body: createPayload,
    });
    await responseJson(replayResponse, 201);
    assert.equal(replayResponse.headers.get('idempotency-replayed'), 'true');

    core = startService(coreBinary, [`-addr=127.0.0.1:${corePort}`], { cwd: temporaryDirectory, env: process.env }, 'core');
    await waitFor('core', core, async () => (await fetch(`${coreUrl}/readyz`)).ok);
    await waitFor('safe edge-to-core retry retention', edge, async () => {
      const status = await responseJson(await fetch(`${edgeUrl}/api/v1/sync/status`), 200);
      return status.data.state === 'degraded' &&
        status.data.outbox.pending === 1 &&
        status.data.outbox.synchronized === 0 &&
        status.data.outbox.lastError === 'sync_inbox_unavailable';
    });

    const hotTicket = created.data.tickets.find((ticket) => ticket.stationId === 'hot');
    assert(hotTicket, 'hot station ticket was not created');
    const ticketStatusKey = 'connected-smoke-fire-hot-ticket';
    const ticketStatusBody = mutationEnvelope(ids.ticketStatusOperation, ticketStatusKey, {
      eventType: 'com.feastcloud.kitchen-ticket.status-changed.v1',
      aggregateType: 'kitchenTicket',
      aggregateId: hotTicket.id,
      ticketId: hotTicket.id,
      orderId: ids.order,
      toStatus: 'fired',
      expectedVersion: 1,
    });
    const ticketStatusResponse = await fetch(`${edgeUrl}/api/v1/sync/mutations`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Idempotency-Key': ticketStatusKey },
      body: JSON.stringify(ticketStatusBody),
    });
    const ticketTransitioned = await responseJson(ticketStatusResponse, 200);
    assert.equal(ticketTransitioned.data.ticket.id, hotTicket.id);
    assert.equal(ticketTransitioned.data.ticket.status, 'fired');
    assert.equal(ticketTransitioned.data.order.status, 'accepted');
    assert.equal(ticketTransitioned.data.order.version, 2);
    const queuedTickets = await responseJson(
      await fetch(`${edgeUrl}/api/v1/kitchen-tickets?status=queued&limit=100`),
      200,
    );
    assert.equal(queuedTickets.data.length, 1);
    assert.equal(queuedTickets.data[0].stationId, 'beverage');
    await waitFor('station transition safe-retry retention', edge, async () => {
      const status = await responseJson(await fetch(`${edgeUrl}/api/v1/sync/status`), 200);
      return status.data.state === 'degraded' &&
        status.data.outbox.pending === 2 &&
        status.data.outbox.synchronized === 0 &&
        status.data.outbox.lastError === 'sync_inbox_unavailable';
    });

    const statusKey = 'connected-smoke-fire-order';
    const statusBody = mutationEnvelope(ids.statusOperation, statusKey, {
      eventType: 'com.feastcloud.order.status-changed.v1',
      aggregateType: 'order',
      aggregateId: ids.order,
      orderId: ids.order,
      toStatus: 'fired',
      expectedVersion: 2,
    });
    const statusResponse = await fetch(`${edgeUrl}/api/v1/sync/mutations`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Idempotency-Key': statusKey },
      body: JSON.stringify(statusBody),
    });
    const transitioned = await responseJson(statusResponse, 200);
    assert.equal(transitioned.data.order.status, 'accepted');
    assert.equal(transitioned.data.order.version, 2);
    assert(transitioned.data.tickets.every((ticket) => ticket.status === 'fired'));
    await waitFor('transition safe-retry retention', edge, async () => {
      const status = await responseJson(await fetch(`${edgeUrl}/api/v1/sync/status`), 200);
      return status.data.state === 'degraded' &&
        status.data.outbox.pending === 3 &&
        status.data.outbox.synchronized === 0 &&
        status.data.outbox.lastError === 'sync_inbox_unavailable';
    });

    await stopService(edge);
    edge = startService(edgeBinary, [], { cwd: temporaryDirectory, env: edgeEnvironment }, 'restarted edge');
    await waitFor('restarted edge', edge, async () => (await fetch(`${edgeUrl}/readyz`)).ok);
    const persisted = await responseJson(await fetch(`${edgeUrl}/api/v1/orders/${ids.order}`), 200);
    assert.equal(persisted.data.status, 'accepted');
    assert.equal(persisted.data.version, 2);
    assert.equal(persisted.data.guestName, 'Connected smoke guest');
    assert.equal(persisted.data.lines[0].preparationNote, 'Medium spice');

    console.log('Connected smoke passed: offline commit, two-station routing, replay, station-only and bulk KDS transitions, causal cloud retry retention, and restart persistence.');
  } catch (error) {
    if (edge?.output) console.error(`\nEdge log:\n${edge.output}`);
    if (core?.output) console.error(`\nCore log:\n${core.output}`);
    throw error;
  } finally {
    await Promise.all([stopService(edge), stopService(core)]);
    await rm(temporaryDirectory, { recursive: true, force: true });
  }
}

await main();
