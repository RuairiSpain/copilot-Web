import type { Metadata, Viewport } from "next";
import "./globals.css";
import { PwaRegister } from "@/components/pwa-register";

export const metadata: Metadata = {
    title: "Copilot Web",
    description: "Chat with GitHub Copilot across your repos, from your phone.",
    manifest: "/manifest.webmanifest",
    icons: {
        icon: [
            { url: "/icons/icon.svg", type: "image/svg+xml" },
            { url: "/icons/icon-192.png", sizes: "192x192", type: "image/png" },
        ],
        apple: "/icons/apple-touch-icon.png",
    },
    appleWebApp: {
        capable: true,
        statusBarStyle: "black-translucent",
        title: "Copilot",
    },
};

export const viewport: Viewport = {
    width: "device-width",
    initialScale: 1,
    maximumScale: 1,
    viewportFit: "cover",
    themeColor: [
        { media: "(prefers-color-scheme: light)", color: "#f7f7f8" },
        { media: "(prefers-color-scheme: dark)", color: "#0d0e11" },
    ],
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
    return (
        <html lang="en" suppressHydrationWarning>
            <body className="min-h-dvh antialiased">
                {children}
                <PwaRegister />
            </body>
        </html>
    );
}
