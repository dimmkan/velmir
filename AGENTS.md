# Velmir

## Product

Velmir is a universal metadata-driven PostgreSQL admin panel.

Backend: Go.
Frontend: Vue 3 + TypeScript + Vite.
Target database: existing PostgreSQL database.
Admin database: separate PostgreSQL database owned by Velmir.

## MVP scope

1. PostgreSQL schema introspection.
2. Automatic resource lists and forms.
3. Authentication, users, roles and permissions.
4. Relations between resources.
5. Change history.

Files, visual schema editing, flows, webhooks and GraphQL are out of scope.

## Architecture

- The target database must never be modified by Velmir migrations.
- Do not create Velmir tables, schemas or triggers in the target database.
- Users, sessions, roles, settings and audit events live in the admin database.
- The backend reads target schema through pg_catalog and information_schema.
- All dynamic SQL identifiers must be validated against introspected metadata.
- User input must never be interpolated directly into SQL.
- Authorization checks must run on the backend.
- Audit history covers changes made through Velmir only.

## Code ownership

- backend agent: /backend
- frontend agent: /frontend
- orchestrator: /docs, root configuration and API contracts
- reviewer: read-only review and test execution

Do not edit files owned by another agent without explicit coordination.

## Backend stack

- Go
- Chi
- pgx/v5
- Goose migrations for the admin database
- Argon2id password hashing
- Server-side sessions with HttpOnly cookies

## Frontend stack

- Vue 3
- TypeScript
- Vite
- Vue Router
- Pinia
- TanStack Query
- TanStack Table
- Naive UI
- VeeValidate + Zod

## Development rules

- Keep the first iteration small and runnable.
- Define API contracts before implementing dependent frontend features.
- Do not rewrite the whole application when a focused change is sufficient.
- Do not add unrequested frameworks or architectural patterns.
- Never commit secrets.
- Never run destructive SQL against the target database.
- Target reads use read-only transactions; supported mutations use a separate pool built from the same TARGET_DATABASE_URL and require database-owner grants.
- Ask before changing public API contracts or database migrations.
- Run tests and builds before reporting completion.

## Definition of done

- Backend tests pass.
- Frontend type-check and production build pass.
- No unrelated files are changed.
- Errors are handled and visible to the user.
- New behavior is documented when necessary.
- The final response lists changed files, executed checks and remaining limitations.