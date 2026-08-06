# Contributing to FeastCloud

FeastCloud welcomes product, engineering, translation, restaurant-operations, accessibility, and documentation contributions.

## Ground rules

- Open an issue or design discussion before a large or externally visible change.
- Preserve tenant isolation, offline behavior, ledger immutability, and human approval boundaries.
- Never add a dependency, model, dataset, font, icon set, or copied example without recording its license and provenance.
- Public contracts are backward-compatible within a major version.
- New user-facing text must use localization keys and include an English source message.
- Money is represented in integer minor units; quantities always carry units.
- Financial and inventory corrections are compensating events, never destructive updates.

## Development checks

```sh
npm install
make check
```

Commits should be small enough to review and include tests for changed behavior. By contributing, you certify the Developer Certificate of Origin 1.1; add a `Signed-off-by` line to commits.

## Licensing

Product contributions are accepted under AGPL-3.0-only. Contributions to `packages/contracts` are accepted under Apache-2.0. Do not submit code that cannot legally be distributed under the applicable repository license.

