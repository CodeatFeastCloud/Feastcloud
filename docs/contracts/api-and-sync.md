# API, idempotency, and offline synchronization

Status: Normative Phase 0 contract

## External API rules

- Public HTTP APIs use JSON over HTTPS under `/api/v1/` and are described by a committed OpenAPI 3.1 document.
- Authentication uses standards-based OIDC/OAuth 2.0 tokens. Authorization is evaluated for tenant, organization node, outlet, capability, and resource condition.
- Mutations accept `Idempotency-Key` and `X-Correlation-ID`. `Idempotency-Key` is mandatory for POST commands and all connector-originated writes.
- Mutable configuration resources expose a strong `ETag`; updates require `If-Match`. Domain commands use transition preconditions rather than generic PATCH.
- Collection pagination is cursor-based with a stable sort. Clients must not construct or modify cursors.
- Errors use `application/problem+json` with stable machine code, human message key, correlation ID, retryability, and field violations. Translated presentation occurs at the client.
- Breaking semantic or wire changes require a new major path or media type. Fields are additive within a major version, and enum consumers must tolerate unknown values.

## Idempotent command processing

The idempotency scope is `(tenant_id, authenticated_principal, route_or_command, idempotency_key)`.

1. The receiver canonicalizes and hashes the command body and relevant precondition headers.
2. It inserts an idempotency record and applies the business transaction atomically.
3. A retry with the same key and hash returns the original status, headers, and body without repeating the effect.
4. Reuse with a different hash returns `409 idempotency_key_reused`.
5. An in-progress duplicate returns the completed response when available or `409 idempotency_in_progress` with `Retry-After`.

API response records are retained for at least 90 days, covering the offline guarantee with operational margin. Permanent business uniqueness remains enforced by source identifiers on orders and ledger events after response expiry. Clients generate random 128-bit-or-greater keys and keep one key for all retries of the same intent.

## Transactional inbox and outbox

- A service writes state and its outbox events in one PostgreSQL transaction.
- The publisher may deliver more than once. Each consumer writes `(consumer, event_id)` to an inbox in the same transaction as its effect.
- An event is acknowledged only after the inbox and effect commit.
- Retries use exponential backoff with jitter. Poison events move to a visible quarantine queue; operators may replay but may not edit them.
- Message ordering is guaranteed only within the documented aggregate key. Consumers use aggregate version and valid transition rules, not global arrival order.

## Edge operation protocol

Edge synchronization uses versioned Protobuf messages over a mutually authenticated streaming or batched HTTPS/gRPC transport. The minimum operations are:

- `Hello(edge_id, outlet_id, software_version, protocol_versions, last_cloud_cursor, capabilities)`
- `PushOperations(batch_id, operations[])`
- `PushResult(batch_id, results[])`
- `PullChanges(after_cursor, limit)`
- `ChangeBatch(next_cursor, changes[], has_more)`
- `Acknowledge(cursor)`
- `SnapshotRequest(resource_set, known_snapshot_id)`

Each operation has a UUIDv7 `operation_id`, mutation context, aggregate ID, aggregate version/precondition, command type, schema version, and payload. A batch contains at most 500 operations or 5 MiB, whichever comes first.

### Local commit sequence

1. Validate the command against the locally signed policy/catalog snapshot.
2. In one SQLite transaction, append the operation, apply the local business effect, update projections, and enqueue device output.
3. Return success to the PWA only after that transaction commits.
4. Retry `PushOperations` until every operation receives a terminal result.
5. Mark an operation synchronized only after the cloud commits its inbox and effect.

`PushResult` returns one result per operation:

- `ACCEPTED` — new business effect committed.
- `DUPLICATE` — the same operation was committed previously.
- `REJECTED` — permanently invalid; includes stable problem code.
- `CONFLICT` — requires deterministic or human reconciliation.
- `RETRY` — temporary failure with retry guidance.

The edge retains rejected/conflicting operations and their local effects in an operator-visible reconciliation queue. It never silently discards or rewrites them.

### Pull sequence and cursors

Cloud changes have an opaque, tenant/outlet-scoped monotonic cursor assigned on commit. The edge applies a `ChangeBatch` and advances its cursor in one SQLite transaction, then acknowledges it. Repeating a batch is safe. A cursor encodes no authorization and is invalid outside its scope.

If the cloud can no longer serve a cursor, it returns `snapshot_required`. The edge downloads a signed snapshot, verifies it, preserves unsynchronized local operations, replaces eligible cloud-owned projections, and replays local operations before resuming incremental sync.

## Conflict policy

| Conflict | Required result |
|---|---|
| Duplicate source order or operation | Return prior result; no second order or movement |
| Cloud menu/policy changed during an active order | Existing order keeps captured version; new orders use new version after edge activation |
| Concurrent configuration edit | Reject stale `If-Match`; show a field-aware comparison |
| Invalid order/ticket transition | Reject transition and retain evidence; never last-write-wins |
| Inventory movements from multiple devices | Accept unique movements; recompute projection |
| Count overlaps unsynchronized movements | Accept evidence but open count reconciliation before posting variance |
| Cash/tender disagreement | Preserve every event and open reconciliation; no destructive merge |
| Revoked device returns after offline use | Quarantine post-revocation operations for authorized review; never silently accept privileged effects |

## Security and compatibility

- Edge-to-cloud uses mTLS device certificates plus outlet-scoped authorization. Certificates are short-lived, rotatable, and revocable.
- Browser-to-edge uses HTTPS and a paired, short-lived, outlet-scoped token. Pairing requires an authorized online or locally delegated manager action.
- Payload authorization is checked at receipt; a valid signature or previous offline authorization does not grant current cloud permission.
- Supported edge protocol versions are negotiated. Cloud supports the current and previous released protocol; migrations are expand/contract and older edges receive an actionable upgrade status before incompatibility.
- Synchronization uses cursors and aggregate versions, never device timestamps, for ordering.

## Webhooks and connectors

Outbound webhooks use CloudEvents structured JSON. FeastCloud signs `timestamp + "." + raw_body` with HMAC-SHA256, includes a key ID, and supports overlapping secrets during rotation. Receivers should reject stale timestamps, validate against the raw body, and deduplicate by event ID.

Delivery is at least once with bounded exponential retries and a dead-letter state visible to operators. Connectors report `healthy`, `degraded`, `authentication_required`, or `disabled`; a partner outage queues work and exposes the documented manual workflow. No connector failure may block native POS or KDS operation.

