import { NextResponse } from "next/server";

// Served at /manifest.webmanifest. Kept as a route handler (rather than a
// static file) so we can vary theme colors without a rebuild later.
export function GET() {
    return NextResponse.json({
        name: "Copilot Web",
        short_name: "Copilot",
        description: "Chat with GitHub Copilot across your repos, from your phone.",
        start_url: "/sessions",
        display: "standalone",
        background_color: "#0d0e11",
        theme_color: "#0d0e11",
        icons: [
            { src: "/icons/icon-192.png", sizes: "192x192", type: "image/png" },
            { src: "/icons/icon-512.png", sizes: "512x512", type: "image/png" },
        ],
    });
}
