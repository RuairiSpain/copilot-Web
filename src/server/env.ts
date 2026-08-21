/**
 * Fail-fast startup validation. Without this, a missing/malformed env var
 * (e.g. `TOKEN_ENCRYPTION_KEY`) previously surfaced lazily and confusingly
 * on someone's first login attempt instead of at boot — see server.ts,
 * which calls `validateEnv()` before starting to listen.
 */
export interface EnvProblem {
    variable: string;
    message: string;
}

function isValidBase64Key32(value: string): boolean {
    try {
        return Buffer.from(value, "base64").length === 32;
    } catch {
        return false;
    }
}

export function validateEnv(env: Record<string, string | undefined> = process.env): EnvProblem[] {
    const problems: EnvProblem[] = [];

    const requireNonEmpty = (variable: string) => {
        if (!env[variable]?.trim()) {
            problems.push({ variable, message: "is required but not set" });
        }
    };

    requireNonEmpty("DATABASE_URL");
    requireNonEmpty("AUTH_GITHUB_ID");
    requireNonEmpty("AUTH_GITHUB_SECRET");
    requireNonEmpty("AUTH_SECRET");

    if (!env.TOKEN_ENCRYPTION_KEY?.trim()) {
        problems.push({ variable: "TOKEN_ENCRYPTION_KEY", message: "is required but not set" });
    } else if (!isValidBase64Key32(env.TOKEN_ENCRYPTION_KEY)) {
        problems.push({
            variable: "TOKEN_ENCRYPTION_KEY",
            message: "must be base64 decoding to exactly 32 bytes — generate with `openssl rand -base64 32`",
        });
    }

    // Self-hosted (non-Vercel) Auth.js refuses requests with an
    // `UntrustedHost` error unless it trusts the request's Host header,
    // which it only does automatically when AUTH_URL is set (or
    // NODE_ENV !== "production", or AUTH_TRUST_HOST is set — see
    // @auth/core's env.ts). Not fatal outside production, but worth
    // catching before a production deploy silently breaks login.
    if (env.NODE_ENV === "production" && !env.AUTH_URL?.trim() && !env.AUTH_TRUST_HOST?.trim()) {
        problems.push({
            variable: "AUTH_URL",
            message:
                'is required in production (or set AUTH_TRUST_HOST=true) — otherwise Auth.js rejects every request with "UntrustedHost"',
        });
    }

    return problems;
}

export function formatEnvProblems(problems: EnvProblem[]): string {
    return [
        "Invalid environment configuration:",
        ...problems.map((p) => `  - ${p.variable}: ${p.message}`),
        "See .env.example for what's required.",
    ].join("\n");
}
