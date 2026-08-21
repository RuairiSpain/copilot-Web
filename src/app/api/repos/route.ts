import { NextResponse } from "next/server";
import { auth } from "@/auth";
import { getOctokitForUser } from "@/lib/octokit";

/** GET /api/repos — repos the signed-in user can push to, for the session
 * creation dropdown. Newest-pushed first, capped to a page since this feeds
 * a mobile picker (not a full browser). */
export async function GET() {
    const session = await auth();
    if (!session?.user) {
        return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const octokit = await getOctokitForUser(session.user.id);
    const { data } = await octokit.repos.listForAuthenticatedUser({
        sort: "pushed",
        direction: "desc",
        per_page: 50,
        affiliation: "owner,collaborator,organization_member",
    });

    return NextResponse.json({
        repos: data
            .filter((repo) => repo.permissions?.push)
            .map((repo) => ({
                fullName: repo.full_name,
                private: repo.private,
                defaultBranch: repo.default_branch ?? "main",
                description: repo.description,
                updatedAt: repo.pushed_at,
            })),
    });
}
