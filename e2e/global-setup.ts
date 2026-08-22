import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { encode } from "next-auth/jwt";
import { PrismaClient } from "@prisma/client";
import { encryptSecret } from "../src/lib/crypto";
import { TEST_USER } from "./test-user";

/**
 * Runs once before the whole Playwright suite. Seeds a fixed test `User`
 * row directly in Postgres and mints a valid Auth.js session cookie for
 * it — this is the "login (mocked)" step the original build plan called
 * for.
 *
 * Why not drive the real GitHub OAuth screen: this app's login is a real
 * GitHub OAuth App code exchange (src/auth.ts), which needs a real GitHub
 * account plus a stored client secret and would make CI depend on GitHub's
 * availability. Auth.js's JWT session strategy means "being logged in" *is*
 * holding a valid encrypted session cookie, so minting one directly with
 * the same `encode()` Auth.js itself calls — same secret, and the same
 * salt convention confirmed by reading @auth/core's session/callback
 * source (the salt is always the session cookie's own name,
 * `authjs.session-token`) — is a faithful stand-in for a completed login,
 * not a bypass of any code path the app runs for a real user. auth.ts's
 * `jwt`/`session` callbacks run unchanged on every request after this: the
 * `jwt` callback only touches `account`/`profile` (absent here, since
 * there's no OAuth `account` on a request that isn't a fresh sign-in), and
 * `session` populates `session.user` straight from the token fields we set
 * below.
 */
export default async function globalSetup() {
    try {
        process.loadEnvFile();
    } catch {
        // No .env file present — expected in CI, which sets these via the
        // workflow's own `env:` block instead (see .github/workflows/ci.yml).
    }

    const authSecret = process.env.AUTH_SECRET;
    if (!authSecret) {
        throw new Error("AUTH_SECRET must be set to run the e2e suite (see .env.example).");
    }

    const prisma = new PrismaClient();
    try {
        const user = await prisma.user.upsert({
            where: { githubId: TEST_USER.githubId },
            update: {
                login: TEST_USER.login,
                encryptedAccessToken: encryptSecret("e2e-fake-github-token"),
            },
            create: {
                id: TEST_USER.id,
                githubId: TEST_USER.githubId,
                login: TEST_USER.login,
                encryptedAccessToken: encryptSecret("e2e-fake-github-token"),
            },
        });
        // Clean slate of sessions each run so specs can assert on an exact
        // session list rather than accumulating rows across test runs.
        await prisma.session.deleteMany({ where: { userId: user.id } });

        const sessionToken = await encode({
            secret: authSecret,
            salt: "authjs.session-token",
            token: { userId: user.id, login: user.login, avatarUrl: user.avatarUrl ?? undefined, sub: user.id },
        });

        const storageState = {
            cookies: [
                {
                    name: "authjs.session-token",
                    value: sessionToken,
                    domain: "localhost",
                    path: "/",
                    expires: Math.floor(Date.now() / 1000) + 60 * 60 * 24,
                    httpOnly: true,
                    secure: false,
                    sameSite: "Lax" as const,
                },
            ],
            origins: [],
        };

        const authDir = path.join(process.cwd(), "e2e", ".auth");
        await mkdir(authDir, { recursive: true });
        await writeFile(path.join(authDir, "storageState.json"), JSON.stringify(storageState, null, 2));
    } finally {
        await prisma.$disconnect();
    }
}
