import NextAuth from "next-auth";
import GitHub from "next-auth/providers/github";
import { prisma } from "@/lib/prisma";
import { encryptSecret } from "@/lib/crypto";

/**
 * GitHub OAuth App login (Auth.js v5, JWT session strategy).
 *
 * We deliberately don't use the Prisma adapter's Account/Session tables:
 * this app only ever needs one secret per user (the current GitHub access
 * token), so on every sign-in we upsert our own `User` row with the token
 * encrypted at rest (see src/lib/crypto.ts) and carry our internal user id
 * in the JWT. That id — not the GitHub id — is what the rest of the app
 * (sessions, repos, SDK calls) keys off of.
 *
 * Scope is `repo` (+ `read:user`) because both the repo picker/creator and
 * the GitHub-API-backed session filesystem (src/server/github-fs.ts) act on
 * the user's behalf against the GitHub REST API using this same token,
 * passed to the Copilot SDK as `gitHubToken` with `useLoggedInUser: false`.
 */
export const { handlers, auth, signIn, signOut } = NextAuth({
    providers: [
        GitHub({
            authorization: { params: { scope: "read:user repo" } },
        }),
    ],
    session: { strategy: "jwt" },
    callbacks: {
        async jwt({ token, account, profile }) {
            if (account?.access_token && profile) {
                // `profile` is typed generically as Auth.js's `Profile` (OIDC
                // shape); GitHub's OAuth profile is a superset with its own
                // fields not covered by that type, hence the `unknown` hop.
                const githubProfile = profile as unknown as {
                    id: number;
                    login: string;
                    name?: string | null;
                    avatar_url?: string | null;
                };
                const user = await prisma.user.upsert({
                    where: { githubId: String(githubProfile.id) },
                    update: {
                        login: githubProfile.login,
                        name: githubProfile.name ?? undefined,
                        avatarUrl: githubProfile.avatar_url ?? undefined,
                        encryptedAccessToken: encryptSecret(account.access_token),
                    },
                    create: {
                        githubId: String(githubProfile.id),
                        login: githubProfile.login,
                        name: githubProfile.name ?? undefined,
                        avatarUrl: githubProfile.avatar_url ?? undefined,
                        encryptedAccessToken: encryptSecret(account.access_token),
                    },
                });
                token.userId = user.id;
                token.login = user.login;
                token.avatarUrl = user.avatarUrl ?? undefined;
            }
            return token;
        },
        async session({ session, token }) {
            if (session.user && token.userId) {
                session.user.id = token.userId as string;
                session.user.login = token.login as string | undefined;
                session.user.image = (token.avatarUrl as string | undefined) ?? session.user.image;
            }
            return session;
        },
    },
    pages: {
        signIn: "/login",
    },
});
