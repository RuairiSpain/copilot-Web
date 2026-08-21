"use client";

import { signOut } from "next-auth/react";

/**
 * Client-side sign-out (`next-auth/react`'s `signOut`, not the server-only
 * one exported from `src/auth.ts`) — it POSTs to the CSRF-protected
 * sign-out endpoint and redirects itself, no SessionProvider needed for
 * this one-shot action.
 */
export function SignOutButton({ className }: { className?: string }) {
    return (
        <button type="button" className={className} onClick={() => signOut({ redirectTo: "/login" })}>
            Sign out
        </button>
    );
}
