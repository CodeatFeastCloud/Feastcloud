# Security policy

FeastCloud is pre-release software and does not yet have a public vulnerability intake address. Until one is established, do not publish exploitable reports in a public issue; contact the repository owner privately.

## Security invariants

- Every request is scoped to an authenticated tenant and authorized outlet.
- Device identity is distinct from employee identity.
- Secrets, card PAN/CVV, raw biometric data, and unrestricted AI prompts do not enter logs.
- Inventory, cash, audit, and fiscal history is append-only.
- Offline clients use bounded credentials and an idempotent operation log.
- Plugins run with declared permissions and cannot query application databases directly.
- AI output is untrusted input and cannot bypass policy or approval services.

Supported-version and disclosure timelines will be published before the first production release.

