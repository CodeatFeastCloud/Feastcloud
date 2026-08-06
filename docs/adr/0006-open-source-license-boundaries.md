# ADR 0006: Open-source license boundaries

- Status: Proposed pending legal ratification
- Date: 2026-08-03

## Context

FeastCloud's moat is operational learning, implementation quality, trust, and ecosystem adoption—not withholding core source. Customers must be able to self-host without a proprietary dependency, while integration authors need permissive contracts and SDKs.

## Decision

License the server, PWA, outlet edge, and standard modules under `AGPL-3.0-only`. License connector, country/language/workflow/hardware SDKs and public schemas/protocols under `Apache-2.0`. Use only approved open-source build/runtime dependencies and commercially usable, redistributable open model weights. Apply the governance policy in `docs/governance/open-source-policy.md`.

Managed hosting, implementation, migration, training, certification, support, backups, hardware bundles, and enterprise SLAs fund the product. Hosted operation does not receive closed core functionality, and full tenant export remains available.

## Consequences

- Network modifications to the AGPL application carry source-sharing obligations; legal notices and source availability must be operationalized.
- Apache-licensed contracts lower connector and pack adoption friction, but implementations copied from AGPL core do not become Apache-licensed automatically.
- License, provenance, SBOM, model/data, and vulnerability checks are release-blocking.
- Trademarks and certification need policies distinct from copyright licenses.

## Alternatives rejected

- Proprietary core/open-core: conflicts with self-hosting and trust goals.
- Permissive license for the entire server: enables closed hosted forks without reciprocity.
- Copyleft SDKs/protocols: discourages independent ecosystem integrations.
- Source-available dependencies: violate the open-source-only constraint.

## Verification

Counsel ratifies exact identifiers and repository boundaries before public release. CI verifies SPDX declarations, artifact SBOMs, transitive licenses, notices, sources, checksums, and signed provenance. A clean self-hosted install runs all core workflows with external network access disabled.

