# Documentation

This directory is organized by audience. The root `README.md` is the public GitHub entry point; this file is the complete documentation map.

## Operator Documents

- [User manual](user-manual.md) - install, configure, connect, and use `httpssh`.
- [Cloudflare setup](cloudflare-setup.md) - one-time Tunnel and Access setup.
- [Cloudflare operations runbook](cloudflare-operations.md) - rotation, troubleshooting, and routine checks.

## Maintainer Documents

- [Release guide](release.md) - GitHub Release workflow, Android signing, tag process, and asset names.
- [Development guide](development.md) - local build, test, and validation commands.
- [Acceptance criteria](acceptance-criteria.md) - v1 shippability checklist.
- [Security policy](../SECURITY.md) - supported versions and vulnerability reporting guidance.

## Design And Contracts

- [Product spec](product-spec.md)
- [Architecture](architecture.md)
- [Data model](data-model.md)
- [API contracts](api-contracts.md)
- [Wire protocol](protocol.md)
- [UI spec](ui-spec.md)
- [Integrations](integrations.md)

## Historical Planning

- [Implementation plan](implementation-plan.md)
- [Task breakdown](task-breakdown.md)

The historical planning docs are still useful for context, but the operator and maintainer docs above should be preferred when installing, releasing, or validating the current code.
