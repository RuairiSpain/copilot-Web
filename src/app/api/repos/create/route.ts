import { NextResponse } from "next/server";
import { z } from "zod";
import { auth } from "@/auth";
import { getOctokitForUser } from "@/lib/octokit";

const createRepoSchema = z.object({
    name: z
        .string()
        .min(1)
        .max(100)
        .regex(/^[a-zA-Z0-9._-]+$/, "Use letters, numbers, dots, dashes, or underscores"),
    description: z.string().max(350).optional(),
});

/** POST /api/repos/create — "create a new public repo" option in the repo
 * picker. Always public: this app has no path for provisioning a private
 * repo's visibility settings, and public is the safer default for a repo
 * created from a mobile picker. */
export async function POST(req: Request) {
    const session = await auth();
    if (!session?.user) {
        return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const parsed = createRepoSchema.safeParse(await req.json());
    if (!parsed.success) {
        return NextResponse.json({ error: parsed.error.flatten() }, { status: 400 });
    }

    const octokit = await getOctokitForUser(session.user.id);
    const { data } = await octokit.repos.createForAuthenticatedUser({
        name: parsed.data.name,
        description: parsed.data.description,
        private: false,
        auto_init: true,
    });

    return NextResponse.json({
        repo: {
            fullName: data.full_name,
            private: data.private,
            defaultBranch: data.default_branch ?? "main",
            description: data.description,
            updatedAt: data.pushed_at,
        },
    });
}
