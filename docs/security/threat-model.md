# FeastCloud threat model

Status: Phase 0 baseline; update for every new trust boundary  
Method: STRIDE-informed, asset and abuse-case driven

## Security objectives

1. Prevent one tenant, outlet, user, device, plugin, or connector from accessing another's data or capabilities.
2. Preserve the integrity and traceability of orders, kitchen transitions, inventory, cash, approvals, and audit events online and offline.
3. Keep live kitchen work available during WAN and optional-service failures.
4. Minimize personal, payment, voice, and employee data and make permitted use auditable.
5. Ensure recommendations cannot bypass deterministic safety rules or authorized approval.

## Assets and classification

| Class | Examples | Minimum handling |
|---|---|---|
| Restricted | Secrets, signing keys, device private keys, identity credentials | Dedicated secret store/keystore, never logged, rotation and access audit |
| Sensitive | Guest/employee PII, voice/transcripts, supplier banking references, security audit payloads | Encryption in transit/at rest, purpose-bound access, retention/deletion policy |
| Financial/operational truth | Orders, tenders, tax records, ledgers, recipes, costs, approvals | Immutable history where specified, integrity controls, backup/restore tests |
| Internal | Forecasts, schedules, aggregate reports, equipment status | Tenant authorization, encryption, controlled exports |
| Public | Published menu, allergen disclosure, public docs | Integrity and publication approval |

FeastCloud MUST NOT store card PAN, CVV, magnetic-stripe data, or payment PINs. Payment adapters store provider tokens and display-safe references only.

## Actors and trust boundaries

Actors include guests, employees, managers, tenant administrators, FeastCloud operators, devices, edge agents, connectors, plugins, model services, attackers on the outlet LAN/WAN, compromised dependencies, and malicious insiders.

Trust boundaries exist between:

- Browser/Capacitor applications and the outlet edge or cloud.
- Outlet devices and the edge host.
- Each edge installation and the cloud ingress.
- Cloud ingress and tenant-scoped application transactions.
- Domain modules and asynchronous consumers.
- Core services and identity, object, analytics, workflow, connector, plugin, and AI systems.
- Managed infrastructure and enterprise self-hosted deployments.
- One tenant/outlet authorization scope and another, even inside a shared database.

## Principal threats and controls

| Threat / abuse case | Required controls | Verification |
|---|---|---|
| Cross-tenant query or object-reference attack | Server-derived tenant context; PostgreSQL RLS; scoped repository APIs; deny-by-default policy; unguessable IDs do not replace authorization | Automated attempts across every CRUD/query/export path and direct SQL test role |
| Stale pooled connection leaks prior tenant context | Transaction-only `SET LOCAL`; mandatory transaction wrapper; pool reset; no tenant context in global/session state | Concurrent tenant stress test and forced transaction-failure test |
| Credential or session theft | OIDC, short-lived access tokens, secure/httpOnly cookies where used, refresh rotation, MFA for privileged roles, logout/revocation | Authentication integration tests and token replay tests |
| Rogue or stolen outlet device | Enrollment approval, per-device certificate and key, mTLS to cloud, least-privilege outlet scope, rotation/revocation, encrypted local data | Lost-device exercise; rejected and quarantined post-revocation operations |
| LAN interception or fake edge | HTTPS, signed edge identity, explicit pairing, short-lived scoped client token, no trust based on network location | MITM and unauthorized pairing tests |
| Replayed/duplicated offline operation | UUID operation IDs, idempotency journal, inbox uniqueness, aggregate transitions, permanent source uniqueness for ledgers/orders | Reorder, duplicate, reconnect, and restore/replay chaos tests |
| Device clock manipulation | Server `recorded_at`, monotonic cursors and aggregate versions; policy windows evaluated at authority | Skew clocks by days and verify order, tax, and authorization behavior |
| Ledger tampering or privileged repair | Runtime deny update/delete; compensating events; checksums; append-only audit; separate time-bound repair identity | Database privilege tests and daily projection/rebuild comparison |
| Malicious employee discount/refund/waste action | Capability and value limits, step-up approval, reason/evidence, immutable audit, anomaly reporting | Role/action matrix tests and collusion-oriented analytics scenarios |
| XSS, CSRF, injection, or unsafe file upload | Contextual escaping, CSP, same-site/CSRF protection, parameterized queries, schema validation, MIME/content scanning, isolated object serving | SAST/DAST, dependency scan, malicious payload suite |
| SSRF or connector exfiltration | Per-connector egress allowlist, secret isolation, fixed callback validation, size/time limits, no cloud metadata access | Network policy test and malicious endpoint fixtures |
| Webhook forgery or replay | HMAC signature over raw body and timestamp, key IDs/rotation, short replay window, event deduplication | Signature mutation, stale timestamp, and duplicate delivery tests |
| Plugin escapes scope | Signed package, explicit capability manifest, Wasm sandbox, CPU/memory/time limits, no direct database/filesystem/network access | Host-call allowlist tests, fuzzing, malicious plugin suite |
| Supply-chain compromise | Lockfiles/checksums, open-source license gate, SBOM, signed provenance/images, isolated CI, secret scan, vulnerability policy | Release verification from clean environment |
| Backup/object exposure or ransomware | Encryption, separate credentials, immutable/offline copies, least privilege, restore drills | Scheduled restore and destructive-account simulation |
| Log/telemetry leakage | Structured field allowlist, redaction, tenant-safe identifiers, restricted log access, retention limits | Automated canary-secret/PII scanning of logs and traces |
| Denial of service or runaway sync | Per-principal limits, bounded payload/batch, backpressure, circuit breakers, quotas, edge autonomy | Load, queue saturation, retry-storm, and 72-hour WAN-loss tests |
| AI prompt injection, data leakage, or unsafe recommendation | Minimized structured inputs, tenant-local retrieval, output schema validation, deterministic policy checks, evidence/confidence, approval, no secrets/tools by default | Adversarial evaluation and attempted cross-tenant retrieval |
| Incorrect allergen, food-safety, tax, or employment decision | Versioned deterministic rules and authorized override; AI cannot authoritatively decide | Golden policy tests and AI-unavailable drills |

## Edge and offline security policy

- SQLite data and operation logs are encrypted at rest; the key is sealed in the OS/device keystore and is not synchronized with the database file.
- The PWA may cache signed application assets and non-sensitive display projections. It MUST NOT claim a critical mutation is committed until the edge has durably accepted it.
- Offline sessions have a centrally configured maximum duration and local role snapshot. High-risk functions such as new administrator grants, policy changes, exports, and device enrollment require online verification.
- A revoked device's later operations are quarantined when they reach cloud. Authorized review may accept individual legitimate business events but cannot restore the device's identity implicitly.
- Sync snapshots are signed, scoped, versioned, and verified before activation. Unsynchronized local operations survive snapshot replacement.

## Privacy and data isolation

- Collect only fields required for a declared purpose. Consent records are purpose-specific and withdrawal does not rewrite legally retained transaction facts.
- Analytics and AI receive de-identified projections when identity is unnecessary. Cross-tenant benchmarks require explicit opt-in, aggregation thresholds, and re-identification review.
- Tenant export and deletion jobs use the same authorization and RLS paths as interactive access, are rate limited, and are audited.
- Object keys, cache keys, message subjects, metrics labels, and search indexes include a server-derived tenant scope. Public URLs use short-lived signed access, not predictable paths.
- Country packs define retention and regulatory presentation, but cannot weaken platform security controls. Local professional review is required before certification.

## Phase 0 security gate

- [ ] Data-flow diagrams and owners exist for PWA, edge, core, identity, PostgreSQL, event bus, and object storage.
- [ ] Tenant and outlet authorization matrix is executable as tests, including negative and direct-object-reference cases.
- [ ] Application database roles cannot bypass RLS or mutate ledger/audit rows.
- [ ] Device enrollment, rotation, revocation, lost-device, and post-revocation reconciliation are demonstrated.
- [ ] Offline duplication, clock skew, corrupted batch, snapshot rollback, and retry-storm tests pass.
- [ ] Secrets, dependency, image, SBOM, license, and provenance checks block releases.
- [ ] Backup restore succeeds in an isolated environment and restored edges cannot duplicate cloud effects.
- [ ] Logs, traces, exports, and error responses pass restricted-data leakage tests.
- [ ] Threat owners accept remaining risks; all critical/high threats have implemented controls or a dated remediation before pilot data is used.

## Explicit residual risks

No software control can eliminate employee collusion, incorrect source recipes, compromised external payment/PMS/aggregator systems, or physical theft of unlocked hardware. FeastCloud reduces these risks through separation of duties, evidence, reconciliation, anomaly detection, connector isolation, device hardening, and documented operating procedures. Each integration adds a trust boundary and requires this model to be updated before release.

