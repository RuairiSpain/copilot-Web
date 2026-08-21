"use client";

import { useEffect } from "react";

export function PwaRegister() {
    useEffect(() => {
        if ("serviceWorker" in navigator) {
            navigator.serviceWorker.register("/sw.js").catch(() => {
                // Non-fatal — the app works the same without it, just not
                // "installable" as a home-screen app.
            });
        }
    }, []);
    return null;
}
