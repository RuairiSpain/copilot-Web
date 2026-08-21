import type { Config } from "tailwindcss";

const config: Config = {
    darkMode: "class",
    content: ["./src/**/*.{ts,tsx}"],
    theme: {
        extend: {
            colors: {
                background: "var(--background)",
                foreground: "var(--foreground)",
                surface: "var(--surface)",
                border: "var(--border)",
                accent: "var(--accent)",
                "accent-foreground": "var(--accent-foreground)",
                muted: "var(--muted)",
                danger: "var(--danger)",
            },
            borderRadius: {
                card: "0.875rem",
            },
        },
    },
    plugins: [],
};

export default config;
