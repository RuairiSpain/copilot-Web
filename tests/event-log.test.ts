import { randomUUID } from "node:crypto";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { prisma } from "@/lib/prisma";
import { appendEvent, getEventsSince } from "@/server/event-log";

/**
 * Integration test against a real Postgres database (DATABASE_URL from
 * `.env`, loaded by tests/setup.ts) rather than a mock — `appendEvent`'s
 * whole job is to assign gap-free sequence numbers under a DB transaction,
 * which a mock can't meaningfully exercise. Requires a reachable Postgres
 * with the schema migrated (`npm run prisma:migrate`); skipped automatically
 * if `DATABASE_URL` isn't set.
 */
const hasDb = Boolean(process.env.DATABASE_URL);
const describeIfDb = hasDb ? describe : describe.skip;

describeIfDb("event-log (real Postgres)", () => {
    let sessionId: string;

    beforeAll(async () => {
        const user = await prisma.user.create({
            data: { githubId: `test-${randomUUID()}`, login: "test-user", encryptedAccessToken: "unused-in-this-test" },
        });
        const session = await prisma.session.create({
            data: { userId: user.id, title: "test session", repoFullName: "octocat/hello-world", repoDefaultBranch: "main" },
        });
        sessionId = session.id;
    });

    afterAll(async () => {
        await prisma.session.deleteMany({ where: { id: sessionId } });
        await prisma.user.deleteMany({ where: { sessions: { some: { id: sessionId } } } });
        await prisma.$disconnect();
    });

    it("assigns gap-free, strictly increasing sequence numbers", async () => {
        const first = await appendEvent(sessionId, "user.message", { content: "hi" });
        const second = await appendEvent(sessionId, "assistant.message", { content: "hello" });
        expect(first.seq).toBe(1);
        expect(second.seq).toBe(2);
    });

    it("assigns sequence numbers correctly under concurrent appends", async () => {
        const before = await prisma.session.findUniqueOrThrow({ where: { id: sessionId }, select: { lastEventSeq: true } });
        const results = await Promise.all(
            Array.from({ length: 10 }, (_, i) => appendEvent(sessionId, "tool.execution_start", { i })),
        );
        const seqs = results.map((r) => r.seq).sort((a, b) => a - b);
        expect(new Set(seqs).size).toBe(10); // no duplicates, even under concurrency
        expect(seqs[0]).toBe(before.lastEventSeq + 1);
    });

    it("getEventsSince returns only events after the given seq, in order", async () => {
        const all = await getEventsSince(sessionId, 0);
        const midpoint = all[Math.floor(all.length / 2)]!.seq;

        const after = await getEventsSince(sessionId, midpoint);
        expect(after.every((e) => e.seq > midpoint)).toBe(true);
        expect(after).toEqual([...after].sort((a, b) => a.seq - b.seq));
    });
});
