# Agent entry

Read this first, then the matching skill. Human docs live under `docs/`. Traccia explicitly requires workspace → team → project tenancy. The copied platform is the starting point, not the completed product. Product decisions in `docs/PRODUCT.md` override inherited boilerplate assumptions.

Canonical env is the **repo-root** `.env`. Compose files are `compose.local.yml` (infra) and `compose.prod.yml` (full stack).

## Skills (`.agents/skills/`)

| Skill | When |
|-------|------|
| [boilerplate-first-setup](./.agents/skills/boilerplate-first-setup/SKILL.md) | Clone, `.env`, Docker names, Dokploy, `make local-dev` |
| [monorepo-playbooks](./.agents/skills/monorepo-playbooks/SKILL.md) | Cross-app work; repo map |
| [add-platform-resource](./.agents/skills/add-platform-resource/SKILL.md) | New list resource (module → RBAC → OpenAPI → UI → nav) |
| [nav-engine](./.agents/skills/nav-engine/SKILL.md) | Sidebar catalog, badges, info |
| [io-engine](./.agents/skills/io-engine/SKILL.md) | Export / import adapters |
| [bulk-engine](./.agents/skills/bulk-engine/SKILL.md) | Bulk actions, jobs, rollback |
| [search-engine](./.agents/skills/search-engine/SKILL.md) | Cmd+K palette, Meilisearch adapters |
| [storage-media](./.agents/skills/storage-media/SKILL.md) | Object storage, media URLs |
| [github-integration](./.agents/skills/github-integration/SKILL.md) | GitHub App OAuth, encrypted credentials |
| [step-up-engine](./.agents/skills/step-up-engine/SKILL.md) | Re-auth gates (password/passkey) on sensitive actions |
| [admin-date-picker](./.agents/skills/admin-date-picker/SKILL.md) | Any date field in `frontend/` |
| [db-migration](./.agents/skills/db-migration/SKILL.md) | New table/column/seed/query: migration → sqlc → OpenAPI |
| [verify-changes](./.agents/skills/verify-changes/SKILL.md) | Checks to run before commit / PR (mirrors CI) |

## Human docs

| Doc | When |
|-----|------|
| [docs/ARCHITECTURE.md](./docs/ARCHITECTURE.md) | Shells, request flow, stores, RBAC |
| [docs/DEVELOPMENT.md](./docs/DEVELOPMENT.md) | Local / prod commands |
| [docs/EXTENDING.md](./docs/EXTENDING.md) | How to add a feature (narrative) |
| [backend/API_CONVENTIONS.md](./backend/API_CONVENTIONS.md) | Envelope, lists, meta |
| [backend/DATABASE_RULES.md](./backend/DATABASE_RULES.md) | Migrations + sqlc |
| [backend/docs/auth.md](./backend/docs/auth.md) | JWT, roles, permissions |

## Hard rules

- Browser talks to same-origin `/api/v1/*`, never the Go host.
- Media URLs are API-owned. Never assemble MinIO/S3 keys in the browser.
- Never expose Meilisearch to the browser; search goes through `/v1/search`.
- New `/v1` routes update `backend/docs/openapi.yaml` in the same change.
- Permission slugs are added only via migration + `internal/platform/rbac` + `frontend/src/config/permissions.ts`.
- After OpenAPI changes: `cd frontend && pnpm api:generate`.

## Traccia product rules

- Use **Plate.js** for new issue, comment, widget and document editors. The inherited Lexical component is legacy platform code; its presence is not the final editor decision.
- Panel, API and MCP must call shared application services with the same validation, authorization, pagination and content fidelity. Do not require a panel-only approval step for ordinary MCP operations.
- Preserve workspace boundaries in queries, search, object storage, background jobs and realtime subscriptions. Teams and projects belong to a workspace; they are not separate tenants.
- Turkish is the default product language; keep all user-facing copy in the existing i18n system.
- Do not publish secrets, private repository source, local analysis or machine paths. Public repository visibility does not select an open-source license.
- Keep implementation status honest. Never claim an approved roadmap item is complete because its scaffolding exists.
