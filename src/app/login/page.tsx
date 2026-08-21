import { redirect } from "next/navigation";
import { auth, signIn } from "@/auth";

export default async function LoginPage() {
    const session = await auth();
    if (session?.user) redirect("/sessions");

    return (
        <main className="flex min-h-dvh flex-col items-center justify-center gap-8 px-6 text-center">
            <div className="space-y-2">
                <h1 className="text-2xl font-semibold">Copilot Web</h1>
                <p className="max-w-xs text-sm text-muted">
                    Chat with GitHub Copilot across your repos — plan, collaborate, or let it
                    run on auto, right from your phone.
                </p>
            </div>
            <form
                action={async () => {
                    "use server";
                    await signIn("github", { redirectTo: "/sessions" });
                }}
            >
                <button
                    type="submit"
                    className="flex items-center gap-2 rounded-card bg-accent px-5 py-3 text-sm font-medium text-accent-foreground shadow-sm"
                >
                    <GitHubMark />
                    Continue with GitHub
                </button>
            </form>
        </main>
    );
}

function GitHubMark() {
    return (
        <svg viewBox="0 0 16 16" width="18" height="18" fill="currentColor" aria-hidden>
            <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
        </svg>
    );
}
