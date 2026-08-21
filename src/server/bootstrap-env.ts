/**
 * Side-effect-only module, imported FIRST in server.ts (before anything
 * that transitively instantiates PrismaClient) so it can set
 * `process.env.DATABASE_URL` before Prisma ever reads it.
 *
 * Why this exists: Azure Container Apps secrets can be injected as a whole
 * env var value (via `secretRef`), but you can't string-interpolate a
 * secretRef into a larger composed value — there's no way to define
 * `DATABASE_URL=postgresql://user:${secret}@host/db` directly in the
 * Container App definition. infra/bicep/modules/containerApp.bicep
 * instead sets discrete `PGHOST`/`PGPORT`/`PGUSER`/`PGPASSWORD`/
 * `PGDATABASE` env vars (the password via secretRef, resolved to a real
 * value at runtime), and this module assembles them into the
 * `DATABASE_URL` Prisma's schema.prisma actually reads.
 *
 * A no-op when DATABASE_URL is already set (local dev, Docker Compose,
 * CI) — those all set it directly, so this never runs there.
 */
if (!process.env.DATABASE_URL && process.env.PGHOST && process.env.PGUSER && process.env.PGPASSWORD) {
    const host = process.env.PGHOST;
    const port = process.env.PGPORT ?? "5432";
    const user = encodeURIComponent(process.env.PGUSER);
    const password = encodeURIComponent(process.env.PGPASSWORD);
    const database = process.env.PGDATABASE ?? "postgres";
    // Azure Database for PostgreSQL Flexible Server requires TLS.
    const sslmode = process.env.PGSSLMODE ?? "require";
    process.env.DATABASE_URL = `postgresql://${user}:${password}@${host}:${port}/${database}?sslmode=${sslmode}`;
}

// Forces this file to be treated as an ES module (rather than an ambient
// script) so `import "./bootstrap-env"` / dynamic `import()` type-check
// correctly — it has no other exports, this is side-effect-only.
export {};
