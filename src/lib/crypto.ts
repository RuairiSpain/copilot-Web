import { createCipheriv, createDecipheriv, randomBytes } from "node:crypto";

/**
 * AES-256-GCM helpers used to encrypt secrets at rest: GitHub OAuth access
 * tokens and MCP server bearer tokens/env. Never log or return plaintext
 * outside the server.
 *
 * Ciphertext layout (all base64 of the concatenated buffer):
 *   [12-byte IV][16-byte auth tag][ciphertext]
 */

function getKey(): Buffer {
    const raw = process.env.TOKEN_ENCRYPTION_KEY;
    if (!raw) {
        throw new Error(
            "TOKEN_ENCRYPTION_KEY is not set. Generate one with `openssl rand -base64 32`.",
        );
    }
    const key = Buffer.from(raw, "base64");
    if (key.length !== 32) {
        throw new Error("TOKEN_ENCRYPTION_KEY must decode to exactly 32 bytes.");
    }
    return key;
}

export function encryptSecret(plaintext: string): string {
    const iv = randomBytes(12);
    const cipher = createCipheriv("aes-256-gcm", getKey(), iv);
    const ciphertext = Buffer.concat([cipher.update(plaintext, "utf8"), cipher.final()]);
    const authTag = cipher.getAuthTag();
    return Buffer.concat([iv, authTag, ciphertext]).toString("base64");
}

export function decryptSecret(encoded: string): string {
    const buf = Buffer.from(encoded, "base64");
    const iv = buf.subarray(0, 12);
    const authTag = buf.subarray(12, 28);
    const ciphertext = buf.subarray(28);
    const decipher = createDecipheriv("aes-256-gcm", getKey(), iv);
    decipher.setAuthTag(authTag);
    return Buffer.concat([decipher.update(ciphertext), decipher.final()]).toString("utf8");
}
