# Single-container image: Next.js (custom server) + WebSocket server in one
# always-on Node.js process. The Copilot SDK spawns a native runtime
# (koffi FFI), so this must stay a long-lived container, not a serverless
# function.

FROM node:22-slim AS base
RUN apt-get update && apt-get install -y --no-install-recommends openssl \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app

FROM base AS deps
COPY package.json package-lock.json* ./
COPY prisma ./prisma
RUN npm ci

FROM deps AS build
COPY . .
RUN npm run prisma:generate && npm run build

FROM base AS runner
ENV NODE_ENV=production
COPY --from=deps /app/node_modules ./node_modules
COPY --from=build /app/.next ./.next
COPY --from=build /app/prisma ./prisma
COPY --from=build /app/public ./public
COPY package.json server.ts tsconfig.json ./
COPY --from=build /app/src ./src

EXPOSE 3000
CMD ["npm", "run", "start"]
