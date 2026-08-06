# License boundary review: Lightning CSS 1.33.0

Status: Engineering-approved build-tool boundary; include in specialist legal review before the first public release  
Reviewed scope: `lightningcss` and its platform packages, exactly version `1.33.0`  
License: `MPL-2.0`

## Use and distribution boundary

Lightning CSS is pulled by Vite and runs only while developing or compiling the FeastCloud PWA. FeastCloud does not modify its source, statically link it into a FeastCloud binary, copy its source into this repository, or distribute its native package in the generated PWA. Its output is FeastCloud-authored CSS; the tool itself is absent from `apps/web/dist`.

The resolved package retains the upstream MPL-2.0 license metadata and source location in `package-lock.json`, the npm CycloneDX SBOM, and the installed package license. No proprietary dependency or hosted service is introduced.

## Decision and guardrail

The exact unmodified build-tool boundary is approved under the open-source-only policy. The license checker permits only the `lightningcss` package family at version `1.33.0`; a different MPL package or version fails closed and requires a new review. If Lightning CSS is later modified, embedded, shipped in an OCI image, or used at application runtime, this decision no longer applies.

This engineering decision is not a substitute for the repository-wide specialist legal review required before the first public release.
