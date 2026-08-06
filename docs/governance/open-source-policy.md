# Open-source software, model, and release policy

Status: Required Phase 0 governance  
Owner: Architecture and security  
Legal status: Engineering baseline; counsel must ratify before the first public release

## Licensing model

| Repository content | Default license |
|---|---|
| Server, PWA clients, outlet edge, standard modules | `AGPL-3.0-only` |
| Connector, country-pack, language-pack, and hardware SDKs | `Apache-2.0` |
| Public schemas and wire protocols | `Apache-2.0` |
| Documentation | `CC-BY-4.0` unless it contains code under the adjacent code license |

Every source file that supports comments SHOULD carry an SPDX identifier. Generated files identify their source and inherit its declared license. Repository-level `LICENSES/` texts, scoped license notices, and SPDX declarations are the canonical notices. Full REUSE metadata is required before the first public release.

Contributions use Developer Certificate of Origin 1.1 sign-off. No contributor license agreement is assumed. FeastCloud trademarks are not granted by the software licenses and require a separate policy before public launch.

## Non-negotiable rules

- Every build-time and runtime dependency distributed with FeastCloud MUST use an OSI-approved license compatible with its target artifact.
- The core MUST function without a proprietary service, binary, SDK, database extension, font, map, telemetry endpoint, or hosted model API.
- An optional commercial partner API is allowed only through a replaceable open-source connector. The core cannot require it, and proprietary SDKs cannot be bundled.
- Source-available is not treated as open source. A dependency with field-of-use, revenue, hosted-service, non-commercial, or research-only restrictions is rejected.
- Dependency names are not enough: CI verifies the resolved version, source URL, checksum, license files, and transitive dependency graph.
- Container base images, operating-system packages, browser assets, fonts, firmware, sample data, model weights, and training/evaluation datasets are governed artifacts, not exceptions.

## License allowlist

The following are approved by automation when unmodified license text and provenance are present and the package is used as intended:

- `Apache-2.0`
- `MIT`
- `BSD-2-Clause`, `BSD-3-Clause`, and `0BSD`
- `ISC`
- `Zlib`
- `PostgreSQL`
- `Python-2.0`
- `Unicode-3.0`
- `CC0-1.0` for data/assets
- `OFL-1.1` for fonts, with reserved-font-name requirements preserved
- `CC-BY-4.0` for attribution-compatible documentation or data, never silently mixed into source code
- `MPL-2.0` only for an exact, unmodified, build-only artifact covered by a versioned boundary review; it is not generally allowlisted for runtime or distributed code

This list grants policy approval, not a compatibility guarantee for an unusual use. Modified dependencies, static linking, generated code, or copied source may change obligations and trigger review.

## Review-required licenses

The following are blocked by CI until architecture and legal reviewers document artifact boundaries, linking, source-offer, notice, and distribution obligations:

- `MPL-2.0` outside the narrow build-only exception above
- `LGPL-2.1-only`, `LGPL-2.1-or-later`, `LGPL-3.0-only`, and `LGPL-3.0-or-later`
- `GPL-2.0-*`, `GPL-3.0-*`, and `AGPL-3.0-*` dependencies
- `EPL-2.0`, `CDDL-1.0`, and `CDDL-1.1`
- Multiple/dual licenses where the selected option is not pinned in dependency metadata
- Licenses with exceptions, custom additions, missing SPDX matches, or conflicting upstream files

A review may require process isolation, dynamic linking, a different package, or rejection. It may not be approved by a package author alone.

## Denied licenses and terms

The following are not permitted in build, runtime, distributed images, model weights, fonts, fixtures, or production datasets:

- SSPL, Business Source License/BUSL, Elastic License 2.0, Commons Clause, PolyForm, Prosperity, Fair Source, and similar source-available terms.
- Any non-commercial, no-derivatives, research-only, evaluation-only, field-of-use, ethical-use, or revenue/user-count restriction.
- Unlicensed code or assets, copied snippets without provenance, ambiguous custom terms, and dependencies whose source cannot be reproduced.
- Network-fetched executable code, models, or assets not represented in the locked manifest and release evidence.

An exception cannot convert a denied artifact into a release dependency. Replace it or isolate it outside FeastCloud as a customer-managed external system accessed through an open protocol.

## Models and datasets

Model code and weights are reviewed independently. A model is eligible only when its exact version permits commercial use, modification, redistribution, and self-hosted inference without a field-of-use restriction.

Every model release record MUST include:

- Upstream repository and immutable revision/checksum.
- Code license, weight license, model card, supported languages, and known limitations.
- Training-data provenance statement to the extent provided by the publisher.
- FeastCloud evaluation version, safety results, quantization/derivative lineage, and approval owner.
- Download size, minimum hardware, retention behavior, and a deterministic fallback when unavailable.

Datasets require source, license, consent/purpose, geographic restrictions, retention policy, and deletion path. Customer data is not used for cross-tenant training without explicit contractual opt-in and de-identification review.

## Automated release evidence

Every pull request runs dependency and secret scanning. Every release candidate produces and signs:

- SPDX and CycloneDX SBOMs for each binary, container, PWA bundle, and model image.
- License inventory including transitive packages, embedded assets, and notices.
- Vulnerability report with OSV/CVE identifiers and exploitability disposition.
- OCI provenance/attestation, source revision, reproducible build inputs, and artifact digest.
- Model/data bill of materials when AI assets are included.

Suggested open-source tooling is REUSE for source declarations, Syft for SBOMs, ORT or ScanCode for license analysis, Trivy/Grype/OSV-Scanner for vulnerabilities, and Cosign for signing. Tool selection may change; evidence requirements may not.

Release policy:

- Unknown or denied license: fail.
- Critical known vulnerability: fail unless the artifact is removed; production exceptions are not allowed.
- High vulnerability: fail unless the security owner records non-exploitability or a time-bounded mitigation and remediation date no later than 30 days.
- Missing provenance, checksum, source archive, notices, or model record: fail.

## Dependency change workflow

1. Prefer the standard library or an existing approved package.
2. Record why the dependency is necessary, its maintained alternatives, resolved transitive graph, and runtime privilege/network needs.
3. Pin an immutable version and checksum; prohibit floating container tags and unpinned model downloads.
4. Run license, vulnerability, provenance, and architecture checks.
5. For review-required licenses, merge only after the documented review is linked.
6. Re-evaluate on every version change and at each release; upstream license changes never inherit prior approval.

## Exception register

Exceptions are stored as versioned records containing artifact/version, exact use, owner, rationale, legal/security decisions, compensating control, review date, and expiry. Exceptions are narrow and expire automatically. Denied terms and the open-source-only core requirement have no engineering exception path.
