import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// bootstrap-env.ts runs its logic as an import-time side effect, so each
// test resets the module registry and re-imports fresh to re-trigger it.
const ENV_KEYS = ["DATABASE_URL", "PGHOST", "PGPORT", "PGUSER", "PGPASSWORD", "PGDATABASE", "PGSSLMODE"] as const;
let saved: Record<string, string | undefined>;

beforeEach(() => {
    saved = Object.fromEntries(ENV_KEYS.map((k) => [k, process.env[k]]));
    for (const k of ENV_KEYS) delete process.env[k];
});

afterEach(() => {
    for (const k of ENV_KEYS) {
        if (saved[k] === undefined) delete process.env[k];
        else process.env[k] = saved[k];
    }
    vi.resetModules();
});

describe("bootstrap-env", () => {
    it("synthesizes DATABASE_URL from discrete PG* vars", async () => {
        process.env.PGHOST = "db.example.com";
        process.env.PGUSER = "copilotadmin";
        process.env.PGPASSWORD = "s3cret";
        process.env.PGDATABASE = "copilot_web";

        vi.resetModules();
        await import("@/server/bootstrap-env");

        expect(process.env.DATABASE_URL).toBe("postgresql://copilotadmin:s3cret@db.example.com:5432/copilot_web?sslmode=require");
    });

    it("URL-encodes special characters in user/password", async () => {
        process.env.PGHOST = "db.example.com";
        process.env.PGUSER = "user@name";
        process.env.PGPASSWORD = "p@ss/word:1";
        process.env.PGDATABASE = "copilot_web";

        vi.resetModules();
        await import("@/server/bootstrap-env");

        expect(process.env.DATABASE_URL).toBe(
            "postgresql://user%40name:p%40ss%2Fword%3A1@db.example.com:5432/copilot_web?sslmode=require",
        );
    });

    it("respects PGPORT and PGSSLMODE overrides", async () => {
        process.env.PGHOST = "db.example.com";
        process.env.PGPORT = "6543";
        process.env.PGUSER = "u";
        process.env.PGPASSWORD = "p";
        process.env.PGSSLMODE = "disable";

        vi.resetModules();
        await import("@/server/bootstrap-env");

        expect(process.env.DATABASE_URL).toContain(":6543/");
        expect(process.env.DATABASE_URL).toContain("sslmode=disable");
    });

    it("is a no-op when DATABASE_URL is already set", async () => {
        process.env.DATABASE_URL = "postgresql://already:set@localhost:5432/db";
        process.env.PGHOST = "should-be-ignored.example.com";
        process.env.PGUSER = "ignored";
        process.env.PGPASSWORD = "ignored";

        vi.resetModules();
        await import("@/server/bootstrap-env");

        expect(process.env.DATABASE_URL).toBe("postgresql://already:set@localhost:5432/db");
    });

    it("is a no-op when the PG* vars are incomplete", async () => {
        process.env.PGHOST = "db.example.com";
        // no PGUSER/PGPASSWORD

        vi.resetModules();
        await import("@/server/bootstrap-env");

        expect(process.env.DATABASE_URL).toBeUndefined();
    });
});
