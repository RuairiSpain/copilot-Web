import { describe, expect, it } from "vitest";
import { formatEnvProblems, validateEnv } from "@/server/env";

const validKey = Buffer.alloc(32, 7).toString("base64");

function baseEnv(overrides: Record<string, string | undefined> = {}): Record<string, string | undefined> {
    return {
        DATABASE_URL: "postgresql://user:pass@localhost:5432/db",
        AUTH_GITHUB_ID: "id",
        AUTH_GITHUB_SECRET: "secret",
        AUTH_SECRET: "auth-secret",
        TOKEN_ENCRYPTION_KEY: validKey,
        ...overrides,
    };
}

describe("validateEnv", () => {
    it("passes with a complete, valid env", () => {
        expect(validateEnv(baseEnv())).toEqual([]);
    });

    it("flags every required variable that's missing", () => {
        const problems = validateEnv({});
        const flagged = problems.map((p) => p.variable);
        expect(flagged).toEqual(
            expect.arrayContaining(["DATABASE_URL", "AUTH_GITHUB_ID", "AUTH_GITHUB_SECRET", "AUTH_SECRET", "TOKEN_ENCRYPTION_KEY"]),
        );
    });

    it("flags an empty-string variable the same as a missing one", () => {
        const problems = validateEnv(baseEnv({ AUTH_SECRET: "   " }));
        expect(problems.some((p) => p.variable === "AUTH_SECRET")).toBe(true);
    });

    it("rejects a TOKEN_ENCRYPTION_KEY that isn't base64 for exactly 32 bytes", () => {
        const problems = validateEnv(baseEnv({ TOKEN_ENCRYPTION_KEY: "too-short" }));
        expect(problems.find((p) => p.variable === "TOKEN_ENCRYPTION_KEY")?.message).toMatch(/32 bytes/);
    });

    it("requires AUTH_URL (or AUTH_TRUST_HOST) in production only", () => {
        expect(validateEnv(baseEnv({ NODE_ENV: "production" })).some((p) => p.variable === "AUTH_URL")).toBe(true);
        expect(validateEnv(baseEnv({ NODE_ENV: "production", AUTH_URL: "https://example.com" }))).toEqual([]);
        expect(validateEnv(baseEnv({ NODE_ENV: "production", AUTH_TRUST_HOST: "true" }))).toEqual([]);
        expect(validateEnv(baseEnv({ NODE_ENV: "development" })).some((p) => p.variable === "AUTH_URL")).toBe(false);
    });
});

describe("formatEnvProblems", () => {
    it("renders each problem on its own line with the variable name", () => {
        const output = formatEnvProblems([{ variable: "AUTH_SECRET", message: "is required but not set" }]);
        expect(output).toContain("AUTH_SECRET: is required but not set");
        expect(output).toContain(".env.example");
    });
});
