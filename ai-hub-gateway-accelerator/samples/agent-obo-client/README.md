# EAG OBO Client — copy-paste starting point for agent developers

Implements `guides/agent-hierarchy-attribution.md` sections 1 and 4 for
developers building agents/sub-agents/MCP tools **inside** the trust
boundary — the OBO token exchange plus the hierarchy headers, so calls
your agent makes to other agents or MCP tools through the EAG gateway get
metered against the original user's live token budget instead of your
own service identity.

**Not for third-party/external agents outside your control** — see
`guides/agent-hierarchy-attribution.md` section 7 for that case, which is
explicitly unmetered by design, not something this code applies to.

## Files

| File | What it verifies |
| --- | --- |
| `python/eag_obo_client.py` | **Tested against the real, installed `msal` 1.38.0 library** — `extract_oid()` and `build_hierarchy_headers()` are unit-tested (fake JWT construction/parsing, header assembly); `EagOboClient`'s constructor call was confirmed correct by reaching real MSAL internals and a live network round trip to Entra ID's OIDC discovery endpoint (failed only because the test used a non-existent tenant, which is expected). |
| `csharp/EagOboClient.cs` | Written against MSAL.NET's documented, confirmed API surface (`AcquireTokenOnBehalfOf(IEnumerable<string>, UserAssertion)`). **Not compiled** — no .NET toolchain was available in the environment this was written in. Build and run it against a real (even sandbox) app registration before trusting it in production, same as you would any copied snippet. |

## Before you can use either

1. An Entra ID app registration for your agent, with a delegated
   permission to call the gateway's app registration on the user's
   behalf — granted and admin-consented. This is a prerequisite the code
   can't set up for you; ask whoever owns your tenant's Entra ID
   configuration.
2. The gateway's own exposed scope string (`api://<gateway-app-id>/.default`
   or similar) — specific to your tenant, get it from whoever owns the
   gateway's app registration.
3. In production, prefer a certificate or Federated Identity Credential
   over the plain client-secret constructor shown here — both files note
   where to swap that in.

## What these files deliberately don't do

- **No signature verification** of the token they decode `oid` from —
  that decode exists only to label your own logs/requests correctly, not
  to make a trust decision. The gateway independently validates every
  token on every call; these snippets never substitute for that.
- **No retry/backoff logic** around the OBO exchange or the gateway call
  — add whatever your own app's resilience conventions call for.
- **No token caching beyond what MSAL/MSAL.NET already do internally** —
  both libraries cache acquired tokens on their own; these files don't
  add a second cache on top.
