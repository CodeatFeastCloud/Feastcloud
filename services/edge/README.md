<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# FeastCloud outlet edge

The outlet edge is the live-shift authority for local orders and kitchen display tickets. It acknowledges a mutation only after the order/ticket projection, immutable transition record, operation log, and cloud outbox entry commit in one SQLite transaction. Cloud availability is never required for local reads or writes.

## Run locally

Go 1.24 or newer is required.

```sh
cd services/edge
FEASTCLOUD_EDGE_ID=edge-development-1 \
FEASTCLOUD_TENANT_ID=11111111-1111-4111-8111-111111111111 \
FEASTCLOUD_OUTLET_ID=33333333-3333-4333-8333-333333333333 \
go run ./cmd/feastcloud-edge
```

The service listens on `127.0.0.1:8081`, stores its database at `data/edge.db`, permits browser requests only from `http://localhost:5173`, and keeps cloud synchronization disabled until a cloud URL is configured.

Verify it with:

```sh
curl http://127.0.0.1:8081/healthz
curl http://127.0.0.1:8081/readyz
curl http://127.0.0.1:8081/api/v1/sync/status
```

Run the restart, duplicate-delivery, transition, CORS, UUIDv7, and cloud-acknowledgement tests with:

```sh
GOCACHE=/tmp/feastcloud-edge-gocache go test ./...
```

## Configuration

| Environment variable | Default | Purpose |
|---|---|---|
| `FEASTCLOUD_EDGE_ID` | required | Stable enrolled edge identity. |
| `FEASTCLOUD_TENANT_ID` | required | Tenant scope enforced for every mutation and sync batch. |
| `FEASTCLOUD_OUTLET_ID` | required | Outlet scope enforced for every mutation and sync batch. |
| `FEASTCLOUD_EDGE_LISTEN` | `127.0.0.1:8081` | Local HTTP(S) listen address. |
| `FEASTCLOUD_EDGE_DATABASE` | `data/edge.db` | Durable SQLite path. |
| `FEASTCLOUD_EDGE_TOKEN` | empty | Bootstrap enrollment/admin token. Required for non-loopback listeners; do not give it to routine browser users. |
| `FEASTCLOUD_EDGE_ALLOWED_ORIGIN` | `http://localhost:5173` | One exact PWA origin; wildcard origins are rejected. Set empty to deny cross-origin browser access. |
| `FEASTCLOUD_EDGE_TLS_CERT`, `FEASTCLOUD_EDGE_TLS_KEY` | empty | Local server certificate and key. Both are required for a non-loopback listener. |
| `FEASTCLOUD_EDGE_MAX_BODY_BYTES` | `1048576` | Maximum mutation request size; range 1 KiB–5 MiB. |
| `FEASTCLOUD_CLOUD_URL` | empty | Cloud base URL; adds `/api/v1/sync/operations`. |
| `FEASTCLOUD_SYNC_ENDPOINT` | empty | Exact cloud push URL; overrides `FEASTCLOUD_CLOUD_URL`. |
| `FEASTCLOUD_CLOUD_TOKEN` | empty | Optional cloud bearer token. |
| `FEASTCLOUD_CLOUD_TLS_CERT`, `FEASTCLOUD_CLOUD_TLS_KEY` | empty | Cloud mTLS client identity. |
| `FEASTCLOUD_CLOUD_CA` | system roots | Additional trusted cloud CA PEM file. |
| `FEASTCLOUD_SYNC_INTERVAL` | `5s` | Due-outbox polling interval. |
| `FEASTCLOUD_SYNC_BATCH_SIZE` | `100` | Operations per push, capped by the protocol at 500 and 5 MiB. |

Remote cloud endpoints must use HTTPS. Cloud requests include `X-Edge-ID`, `X-FeastCloud-Tenant-ID`, and `X-FeastCloud-Actor-ID: edge:<edgeId>`; mTLS can be enabled without changing the sync adapter. Non-loopback local serving requires TLS and a bearer token.

### Browser pairing and offline sessions

A manager uses the bootstrap token to create a one-time code, then enters that code in the PWA. The code expires after 10 minutes and is atomically consumed once. The resulting manager, cashier, or chef session is random, stored only as a SHA-256 hash in SQLite, valid for 72 hours, usable across a WAN outage, and immediately revocable. Role checks are enforced by the edge; the PWA cannot elevate a paired role with its display-role control.

```sh
curl -X POST http://127.0.0.1:8081/api/v1/pairing/codes \
  -H 'Authorization: Bearer <bootstrap-token>' \
  -H 'Content-Type: application/json' \
  --data '{"role":"chef"}'
```

Pair with `POST /api/v1/pairing/sessions`, revoke the current browser session with `POST /api/v1/pairing/sessions/revoke`, and rotate `FEASTCLOUD_EDGE_TOKEN` if the bootstrap credential is exposed.

## Local API

All application routes use `/api/v1`. Mutation requests use `Content-Type: application/json`, include an `Idempotency-Key` header, and carry the exact canonical camelCase envelope from `packages/contracts/schemas/mutation-envelope.json`. The header value must equal `idempotencyKey`. Operation, order, and line identifiers must be UUIDv7 so the same operation can be accepted by the cloud protocol.

The committed OpenAPI 3.1 document is in `api/openapi.json`.

Primary routes:

- `POST /api/v1/orders` — create an order and one queued KDS ticket per distinct line `stationId`.
- `GET /api/v1/orders[?status=&limit=]` and `GET /api/v1/orders/{id}`.
- `POST /api/v1/orders/{id}/transitions` — transition the order and its active tickets.
- `GET /api/v1/kitchen-tickets[?stationId=&status=&limit=]` and `GET /api/v1/kitchen-tickets/{id}`.
- `GET /api/v1/stations/{stationId}/tickets` — station-specific KDS queue.
- `POST /api/v1/kitchen-tickets/{id}/transitions` — transition one KDS ticket and derive its order state.
- `POST /api/v1/sync/mutations` — browser ingress for PWA order events.
- `GET /api/v1/sync/status` — pending, synchronized, and reconciliation counts plus last cloud result.
- `POST /api/v1/pairing/codes`, `/pairing/sessions`, and `/pairing/sessions/revoke` — one-time enrollment and revocable offline browser sessions.

Order projections retain the service context another local KDS or POS needs: guest name, table label, order note, target time, localized line display name, preparation note, and station ID. They never contain payment-card data.

Example direct order command:

```sh
curl -i http://127.0.0.1:8081/api/v1/orders \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: create-order-0198a2b3' \
  --data '{
    "id":"0198a2b3-c4d5-7e6f-8a9b-0c1d2e3f4001",
    "tenantId":"11111111-1111-4111-8111-111111111111",
    "outletId":"33333333-3333-4333-8333-333333333333",
    "deviceId":"tablet-1",
    "actorId":"cashier-1",
    "occurredAt":"2026-08-03T08:00:00Z",
    "source":"feastcloud-pwa",
    "schemaVersion":"1.0",
    "idempotencyKey":"create-order-0198a2b3",
    "payload":{"order":{
      "id":"0198a2b3-c4d5-7e6f-8a9b-0c1d2e3f4002",
      "type":"takeaway",
      "placedAt":"2026-08-03T08:00:00Z",
      "lines":[
        {"id":"0198a2b3-c4d5-7e6f-8a9b-0c1d2e3f4003","menuItemId":"dal-bowl","name":"Dal bowl","quantity":1,"stationId":"hot"},
        {"id":"0198a2b3-c4d5-7e6f-8a9b-0c1d2e3f4004","menuItemId":"lassi","name":"Lassi","quantity":1,"stationId":"beverage"}
      ]
    }}
  }'
```

A retry with the same idempotency key and canonically equivalent body returns the original status and body with `Idempotency-Replayed: true`. Reusing the key with different content returns `409 idempotency_key_reused`. Idempotency survives process and machine restarts with the database.

### PWA browser ingress

`POST /api/v1/sync/mutations` accepts three event payloads inside the same mutation envelope:

```json
{
  "eventType": "com.feastcloud.order.created.v1",
  "aggregateType": "order",
  "aggregateId": "<same UUIDv7 as order.id>",
  "order": {
    "id": "<UUIDv7>",
    "type": "delivery",
    "placedAt": "<RFC3339>",
    "stationTicketIds": { "hot": "<UUIDv7>", "beverage": "<UUIDv7>" },
    "lines": [
      { "id": "<UUIDv7>", "name": "Biryani", "quantity": 1, "stationId": "hot" },
      { "id": "<UUIDv7>", "name": "Lassi", "quantity": 1, "stationId": "beverage" }
    ]
  }
}
```

`stationTicketIds` is optional for older clients. When supplied, each key must match a routed station and each value must be a unique UUIDv7; the edge preserves these browser-allocated IDs so offline station transitions can safely follow the create operation.

```json
{
  "eventType": "com.feastcloud.order.status-changed.v1",
  "aggregateType": "order",
  "aggregateId": "<order UUIDv7>",
  "orderId": "<same order UUIDv7>",
  "toStatus": "fired",
  "expectedVersion": 1
}
```

The status event advances every order ticket by exactly one valid step. `expectedVersion` is the current order version and is mandatory; idempotency is also mandatory.

```json
{
  "eventType": "com.feastcloud.kitchen-ticket.status-changed.v1",
  "aggregateType": "kitchenTicket",
  "aggregateId": "<ticket UUIDv7>",
  "ticketId": "<same ticket UUIDv7>",
  "orderId": "<parent order UUIDv7>",
  "toStatus": "fired",
  "expectedVersion": 1
}
```

The station event advances only the addressed ticket. `expectedVersion` is mandatory for station-ticket mutations; the edge derives the parent order state from all of its tickets in the same transaction.

## Deterministic state machines

Tickets follow `queued → fired → preparing → ready → completed`. Any non-terminal ticket may be cancelled. Skipping or reversing a state returns `409 invalid_transition` and commits no state or outbox entry.

Order commands follow `received → accepted → preparing → ready → completed`, with cancellation allowed before completion. Ticket events derive the order projection deterministically:

| Ticket evidence | Derived order state |
|---|---|
| all queued | `received` |
| at least one fired, none preparing or later | `accepted` |
| at least one preparing/completed and not all ready | `preparing` |
| all active tickets ready or later | `ready` |
| all non-cancelled tickets completed | `completed` |
| all tickets cancelled | `cancelled` |

Derived order state never regresses. Every accepted transition records its prior state, next state, version, operation ID, and server record time.

## Cloud synchronization contract

The HTTP adapter sends `POST /api/v1/sync/operations` with:

```json
{
  "batchId": "<UUIDv7>",
  "edgeId": "edge-development-1",
  "outletId": "33333333-3333-4333-8333-333333333333",
  "operations": [{
    "operationId": "<UUIDv7>",
    "aggregateType": "order",
    "aggregateId": "<UUIDv7>",
    "aggregateVersion": 1,
    "commandType": "order.create",
    "mutation": { "id": "<same operation UUIDv7>", "payload": {} },
    "recordedAt": "<RFC3339>"
  }]
}
```

The response is `{ "batchId": "…", "results": [{ "operationId": "…", "status": "ACCEPTED" }] }`. `ACCEPTED` and `DUPLICATE` mark an item synchronized. `REJECTED` and `CONFLICT` preserve it in the operator-visible reconciliation count. `RETRY`, missing results, invalid responses, and transport failures retain it with bounded retry scheduling. The adapter is isolated in `internal/syncer`; local domain and store packages do not depend on HTTP or any cloud implementation.

The default infrastructure-free core returns `RETRY / sync_inbox_unavailable`, so the edge shows a degraded state and keeps the operation pending. When core is configured with PostgreSQL and both migrations are applied, it returns `ACCEPTED` only after inbox evidence and the append-only domain event commit atomically; an exact replay returns `DUPLICATE`.

## Durability and security notes

- SQLite uses WAL mode, foreign keys, a five-second busy timeout, one serialized writer, and file mode `0600`.
- Projection changes, transition history, operation, outbox, and stored idempotent response share one database transaction.
- Payment card PAN/CVV must never enter any edge command or log.
- The current pure-Go SQLite driver does not provide database-level encryption. Production deployments must place the database on an encrypted volume with keys outside the database; application-managed encrypted storage remains a production hardening gate.
- Reconciliation entries are retained rather than silently discarded or rewritten.

## License

This service is licensed `AGPL-3.0-only` and inherits the repository root license. It uses `modernc.org/sqlite`, a pure-Go SQLite driver distributed under a BSD-3-Clause license; its transitive dependencies are OSI-compatible. The service has no required proprietary runtime or SaaS dependency.
