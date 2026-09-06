/**
 * Minimal Azure AI Foundry chat-completions client for the delegation
 * scripts. No SDK dependency on purpose: these scripts are invoked by the
 * agent as plain `node` processes and shouldn't need an install step in
 * whatever worktree the session happens to be running in.
 *
 * Auth precedence: an explicit bearer token (`FOUNDRY_TOKEN`, e.g. from
 * `az account get-access-token --resource https://cognitiveservices.azure.com`)
 * beats an API key, so a managed-identity setup doesn't need a key at all.
 *
 * The endpoint defaults to the Azure OpenAI **v1** API surface, which is what
 * the Copilot app's BYOK provider expects. Setting `FOUNDRY_API_VERSION`
 * switches to the older dated `deployments/{name}` route instead.
 */

export class FoundryError extends Error {}

function endpointUrl(env) {
    const base = (env.FOUNDRY_ENDPOINT ?? "").replace(/\/+$/, "");
    if (!base) throw new FoundryError("FOUNDRY_ENDPOINT is not set");
    const deployment = env.FOUNDRY_DEPLOYMENT;
    if (!deployment) throw new FoundryError("FOUNDRY_DEPLOYMENT is not set");
    if (env.FOUNDRY_API_VERSION) {
        return `${base}/openai/deployments/${encodeURIComponent(deployment)}/chat/completions?api-version=${encodeURIComponent(env.FOUNDRY_API_VERSION)}`;
    }
    return `${base}/openai/v1/chat/completions`;
}

function authHeaders(env) {
    if (env.FOUNDRY_TOKEN) return { Authorization: `Bearer ${env.FOUNDRY_TOKEN}` };
    if (env.FOUNDRY_API_KEY) return { "api-key": env.FOUNDRY_API_KEY };
    throw new FoundryError("Set FOUNDRY_API_KEY or FOUNDRY_TOKEN");
}

/**
 * One-shot invocation. Nothing is stored anywhere: the corpus goes to the
 * worker and never enters the agent's context, so re-sending the same files
 * on a follow-up question costs worker tokens only.
 *
 * Usage is reported on stderr rather than stdout so the caller can pipe the
 * result without having to strip it — and so the saving is observable.
 */
export async function invoke({ system, user, temperature = 0.2, cfg, env = process.env }) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), cfg.timeoutMs);
    let res;
    try {
        res = await fetch(endpointUrl(env), {
            method: "POST",
            headers: { "Content-Type": "application/json", ...authHeaders(env) },
            body: JSON.stringify({
                model: env.FOUNDRY_DEPLOYMENT,
                temperature,
                messages: [
                    { role: "system", content: system },
                    { role: "user", content: user },
                ],
            }),
            signal: controller.signal,
        });
    } catch (err) {
        if (err?.name === "AbortError") throw new FoundryError(`worker timed out after ${cfg.timeoutMs}ms`);
        throw new FoundryError(`worker request failed: ${err?.message ?? err}`);
    } finally {
        clearTimeout(timeout);
    }

    const text = await res.text();
    if (!res.ok) throw new FoundryError(`worker returned HTTP ${res.status}: ${text.slice(0, 800)}`);

    let body;
    try {
        body = JSON.parse(text);
    } catch {
        throw new FoundryError(`worker returned non-JSON: ${text.slice(0, 400)}`);
    }

    const content = body?.choices?.[0]?.message?.content;
    if (typeof content !== "string") throw new FoundryError("worker response had no message content");

    const u = body.usage;
    if (u) {
        process.stderr.write(
            `shunt: delegated to ${env.FOUNDRY_DEPLOYMENT} — ` +
                `${u.prompt_tokens ?? "?"} in / ${u.completion_tokens ?? "?"} out (worker tokens, not agent context)\n`,
        );
    }
    return content;
}

/** The worker is told not to use fences, but instruction-following isn't
 * guaranteed, and a stray fence becomes a syntax error once written to disk. */
export function stripFences(text) {
    const trimmed = text.trim();
    const fenced = /^```[^\n]*\n([\s\S]*?)\n?```$/.exec(trimmed);
    return (fenced ? fenced[1] : trimmed).replace(/\s*$/, "") + "\n";
}
