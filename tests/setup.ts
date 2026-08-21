// Loads `.env` (DATABASE_URL, TOKEN_ENCRYPTION_KEY, ...) into process.env for
// tests that talk to a real local Postgres — see event-log.test.ts and
// session-manager.test.ts. In CI these vars are set directly by the
// workflow instead, so a missing .env file here is not an error.
import { loadEnvFile } from "node:process";

try {
    loadEnvFile();
} catch {
    // No .env present — assume the environment already provides what's needed.
}
