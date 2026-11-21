# Changelog

## 0.1.0-rc.1 — 2026-11-21

### Added

- PostgreSQL schema introspection and metadata-driven resource lists with pagination, sorting, search, and typed filters.
- Automatic create/update forms, confirmed deletion, related labels, many-to-one selectors, read-only one-to-many panels, and many-to-many editing through supported junction tables.
- Server-side session authentication, user/role administration, resource/action RBAC, and independent field-level read/create/update permissions, including PK-only and write-only modes.
- Audit history for successful create/update/delete operations performed through Velmir.
- Russian and English UI, Velmir branding, logo, and favicons.
- Admin database Goose migrations through version 9, including unrestricted read backfill for existing version-8 permissions.

### Security

- Argon2id password hashing, HttpOnly session cookies, CSRF validation, login rate limiting, secure production cookie requirements, and backend authorization.
- Production rejection of missing, weak, and trivial bootstrap credentials; `admin/admin` remains limited to the explicit localhost demo.
- Single `TARGET_DATABASE_URL` for separate read and mutation pools, with the read pool kept read-only and no automatic target DDL/GRANT.
- CSP and other security headers, explicit trusted-proxy CIDRs, non-root containers, read-only app rootfs, dropped capabilities, and secret-file support.
- Go toolchain updated to 1.26.11 and `chi/v5` updated to 5.4.0; runtime `zlib` is upgraded during the image build.

### Deployment

- Multi-stage production Docker image with compiled frontend, backend, migration/bootstrap/users CLIs, and healthchecks.
- Compose modes for demo, production loopback HTTP, Caddy HTTPS, Docker secrets, and combined secrets/bootstrap/target/TLS operation.
- Ordered migration and bootstrap jobs, named volumes, restart policies, resource limits, log rotation, and Caddy certificate persistence.
- Shell and PowerShell admin backup/fresh-restore scripts; restore rejects an existing destination database and does not require a target DSN.

### Documentation

- API references in Russian and English.
- Full standalone setup guides: [Russian](docs/setup-guide.ru.md) and [English](docs/setup-guide.en.md).
- README quickstarts, environment reference, upgrade/rollback guidance, backup/restore instructions, and production security checklist.

### Known limitations

- Nested mutations, files, visual schema editing, flows, webhooks, GraphQL, and automation are not implemented.
- Audit covers changes made through Velmir only; target mutation and admin audit writes are not cross-database atomic.
- Updates use last-write-wins semantics; optimistic locking and bulk deletion are unavailable.
- Login rate limiting is process-local and is not synchronized across replicas.
- Image rollback does not roll back database migrations; disaster recovery requires a separate fresh-database restore.
- Public ACME issuance requires a real public domain and was not exercised by the local RC smoke checks.
- Development-only npm audit findings remain in the test/type-check toolchain; production npm dependencies have no reported audit findings.
- Исправлена совместимость collation admin-базы: PostgreSQL 16 закреплён на Debian Trixie с glibc 2.41.