# FeastCloud Web

Offline-first React/TypeScript vertical slice for FeastCloud Kitchen OS. It includes order entry, a station-filtered kitchen display, manager overview, English/Hindi/Bengali packs, runtime-installable additional languages, role personalization, IndexedDB persistence, an idempotent mutation outbox, and an installable service worker.

## Run locally

```sh
npm install
npm run dev
```

The app is self-contained by default. Mutations are durably written to a session-pinned IndexedDB store, with one-record localStorage journaling only when IndexedDB cannot be opened. The demo adapter acknowledges them locally when online. To connect an outlet edge service, copy `.env.example` to `.env` and set `VITE_EDGE_URL`. The client posts canonical mutation envelopes to:

```text
${VITE_EDGE_URL}/api/v1/sync/mutations
```

If `VITE_EDGE_URL` already ends in `/api/v1`, that segment is not duplicated.

When connected, the PWA first discovers `tenantId`, `outletId`, and `edgeId` from `GET /api/v1`; it will not create commands using the demo scope before that binding succeeds. It then hydrates and polls the edge order and kitchen-ticket projections. This removes local demo fixtures, makes orders from another paired outlet device visible, reconciles edge-assigned numbers and versions, and preserves newer unsynchronized local intent until the edge catches up. Guest/table/note, target time, line name, preparation note, station routing, and per-station progress round-trip through that projection. Browser mutations and projection commits share one serialized durability queue so concurrent station actions cannot overwrite each other.

When the configured edge requires authentication, the PWA presents an 8-digit pairing screen, exchanges the one-time code for a role-scoped 72-hour outlet-local session, attaches it to projection and mutation requests, and locks the display role to the server-issued role. The session permits operation across a WAN outage and is removed locally on expiry. Never embed the long-lived edge bootstrap token in a `VITE_*` value.

Managers receive a Food cost view when `VITE_CORE_URL` is configured. It reads the ledger-derived on-hand quantity, stock value, theoretical recipe consumption, and waste value for the paired outlet. Demo mode uses tenant headers; an OIDC deployment reads the short-lived access token from the authenticated browser session and never embeds it in a `VITE_*` build variable.

Station filters operate on individual kitchen tickets from `/kitchen-tickets`; advancing one station does not falsely advance another. The order state is derived from the combined ticket evidence. **All stations** remains a deliberately separate whole-order transition for supervisors and demo workflows; production authorization policy is still a release gate.

New browser orders allocate stable ticket IDs per routed station before network transmission. The edge preserves those IDs from `stationTicketIds`, so ticket actions queued behind an offline order remain valid when connectivity returns. A configured outlet edge is always attempted even when `navigator.onLine` reports no public internet, because LAN edge reachability and WAN availability are separate conditions.

The outbox drains on application start and after connectivity returns. Transient failures stay pending in device order; permanent command failures are retained as visible “needs review” reconciliation items while independent commands continue. A successful order-entry message means only that the operation is durable on this device and queued—it does not claim edge or kitchen acceptance.

## Install a language without changing application code

Set `VITE_LANGUAGE_PACK_INDEX_URL` to a trusted HTTPS index, or deploy an index at `/language-packs/index.json`. Each entry contains a BCP 47 `locale`, same-origin pack `url`, and lowercase SHA-256. Packs follow the public `language-pack.json` contract and must contain every English UI key with the same interpolation placeholders. The app validates the locale, direction, semantic version, message completeness, size, origin, and checksum before installation; invalid packs cannot block the built-in English fallback. Successfully fetched same-origin packs are cached by the service worker for later offline starts.

The bundled index is empty because English, Hindi, and Bengali are built in. Updating the hosted index and pack files installs another language independently of a FeastCloud code release. Human review/certification state remains pack metadata and is not inferred from successful installation.

## Verification

```sh
npm run typecheck
npm test
npm run build
```

Preview the production build to validate PWA installation and offline navigation; service workers intentionally do not register in the development server.
