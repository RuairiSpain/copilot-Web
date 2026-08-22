/**
 * Side-effect-only module, imported FIRST in server.ts — even before
 * bootstrap-env.ts — so `.env` (local dev, Docker Compose) is loaded into
 * process.env deterministically before anything else runs.
 *
 * Without this, Next.js's own `.env` loading (triggered later, inside
 * `next({ dev })`) hadn't happened yet by the time bootstrap-env.ts or
 * validateEnv() ran, so a local `.env` setting PGHOST/PGUSER/PGPASSWORD
 * (rather than DATABASE_URL directly) silently failed — confirmed by
 * actually booting the server that way, not just by reasoning about it.
 *
 * A no-op in production deployments (Container Apps, Docker) which set
 * real process env vars directly and don't ship a .env file at all.
 */
try {
    process.loadEnvFile();
} catch {
    // No .env file present — expected outside local dev.
}

export {};
