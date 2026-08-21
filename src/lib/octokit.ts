import { Octokit } from "@octokit/rest";
import { prisma } from "@/lib/prisma";
import { decryptSecret } from "@/lib/crypto";

/** Decrypted GitHub token for a user, or null if the user doesn't exist. */
export async function getGitHubTokenForUser(userId: string): Promise<string | null> {
    const user = await prisma.user.findUnique({
        where: { id: userId },
        select: { encryptedAccessToken: true },
    });
    if (!user) return null;
    return decryptSecret(user.encryptedAccessToken);
}

/** Octokit client authenticated as the given user, for repo listing/creation
 * and the GitHub-API-backed session filesystem. */
export async function getOctokitForUser(userId: string): Promise<Octokit> {
    const token = await getGitHubTokenForUser(userId);
    if (!token) {
        throw new Error(`No stored GitHub token for user ${userId}`);
    }
    return new Octokit({ auth: token });
}
