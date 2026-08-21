import type { NextConfig } from "next";

const nextConfig: NextConfig = {
    reactStrictMode: true,
    // The Copilot SDK spawns/talks to a native runtime (koffi FFI) and is
    // only meant to run server-side. Keep it out of the client bundle and
    // out of Next's server component bundling so its native bindings are
    // required at runtime, not bundled.
    serverExternalPackages: ["@github/copilot-sdk", "koffi", "ws"],
};

export default nextConfig;
