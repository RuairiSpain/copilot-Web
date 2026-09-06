// EAG OBO Client — for agent/sub-agent developers inside the trust boundary.
//
// Copy this file into your agent's codebase, fill in your Entra ID app
// registration values, and use it before your agent calls out to a
// sub-agent or MCP tool through the EAG gateway — so that call gets
// metered against the ORIGINAL user's token budget, not your own service
// identity, and is cryptographically verifiable rather than a
// self-asserted claim.
//
// Implements the design in guides/agent-hierarchy-attribution.md:
//   - Section 4: Entra ID On-Behalf-Of (OBO) token exchange.
//   - Section 1: the x-agent-root-id / x-agent-caller-type / x-agent-depth
//     hierarchy headers, carried alongside the OBO token.
//
// NuGet: Microsoft.Identity.Client (MSAL.NET), System.IdentityModel.Tokens.Jwt
//
// HONESTY NOTE: I verified the equivalent Python file's core logic and
// its MSAL constructor call against the real, installed msal library
// (including a live network round trip to Entra ID's OIDC discovery
// endpoint). No .NET compiler was available in the environment this was
// written in, so this C# file is NOT compiled or run — it's written
// against MSAL.NET's confirmed, documented API surface
// (AcquireTokenOnBehalfOf(IEnumerable<string>, UserAssertion) on
// IConfidentialClientApplication, per Microsoft's own reference docs),
// but build it and run it against a real (even sandbox) app registration
// before trusting it in production, the same way you would any copied
// snippet.

using System;
using System.Collections.Generic;
using System.IdentityModel.Tokens.Jwt;
using System.Linq;
using System.Net.Http;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Text.Json;
using System.Threading.Tasks;
using Microsoft.Identity.Client;

namespace Eag.AgentObo
{
    /// <summary>
    /// Raised when the OBO token exchange fails. Check <see cref="ErrorDetail"/>
    /// before assuming this is a bug in this class — the overwhelmingly
    /// common cause is an Entra ID app-registration/consent problem:
    ///   - Your app's registration hasn't been granted (and admin-
    ///     consented) the delegated permission to call the gateway on
    ///     the user's behalf.
    ///   - The incoming token you passed as the user assertion is an ID
    ///     token, not an access token — OBO requires an access token.
    ///   - The incoming token's audience doesn't match what your app
    ///     registration expects.
    /// </summary>
    public class EagOboException : Exception
    {
        public MsalServiceException? ErrorDetail { get; }

        public EagOboException(string message, MsalServiceException? errorDetail = null)
            : base(message)
        {
            ErrorDetail = errorDetail;
        }
    }

    /// <summary>
    /// Wraps an Entra ID confidential client configured for On-Behalf-Of
    /// token exchange. Construct one instance per process/app and reuse
    /// it across requests — MSAL.NET caches tokens internally and skips
    /// the network round trip when it already holds a still-valid token.
    /// </summary>
    public class EagOboClient
    {
        private readonly IConfidentialClientApplication _app;

        /// <param name="clientId">Your agent's Entra ID app registration client ID.</param>
        /// <param name="clientSecret">
        /// The simplest way to get started. Prefer a certificate or
        /// Federated Identity Credential over a bare secret in
        /// production — this project's own pricing-service uses managed
        /// identity for exactly this reason (see src/pricing-service/),
        /// and Microsoft's purpose-built "Microsoft Entra Agent ID"
        /// product (guides/agent-hierarchy-attribution.md, section 4) is
        /// built around Federated Identity Credentials, not a stored
        /// secret. See:
        /// https://learn.microsoft.com/en-us/entra/msal/dotnet/acquiring-tokens/web-apps-apis/confidential-client-assertions
        /// for swapping this constructor for a certificate/FIC-based one.
        /// </param>
        /// <param name="tenantId">Your Entra ID tenant ID or domain.</param>
        public EagOboClient(string clientId, string clientSecret, string tenantId)
        {
            _app = ConfidentialClientApplicationBuilder
                .Create(clientId)
                .WithClientSecret(clientSecret)
                .WithAuthority(new Uri($"https://login.microsoftonline.com/{tenantId}"))
                .Build();
        }

        /// <summary>
        /// Exchanges the incoming user's ACCESS token for one scoped to
        /// call the EAG gateway, still carrying the original user's
        /// identity (via the <c>oid</c> claim — see
        /// <see cref="EagTokenHelpers.ExtractOid"/>).
        /// </summary>
        /// <param name="userAssertionToken">
        /// The access token your agent received on its own inbound
        /// request. Must be an access token, not an ID token.
        /// </param>
        /// <param name="gatewayScopes">
        /// The scope(s) exposed by the gateway's own Entra ID app
        /// registration, e.g. ["api://&lt;gateway-app-id&gt;/.default"].
        /// This is specific to your tenant's setup — ask whoever owns
        /// the gateway's app registration for the exact scope string
        /// rather than guessing.
        /// </param>
        /// <exception cref="EagOboException">On any exchange failure.</exception>
        public async Task<string> GetGatewayTokenAsync(string userAssertionToken, string[] gatewayScopes)
        {
            try
            {
                var userAssertion = new UserAssertion(userAssertionToken);
                AuthenticationResult result = await _app
                    .AcquireTokenOnBehalfOf(gatewayScopes, userAssertion)
                    .ExecuteAsync();
                return result.AccessToken;
            }
            catch (MsalServiceException ex)
            {
                throw new EagOboException($"OBO token exchange failed: {ex.ErrorCode} — {ex.Message}", ex);
            }
        }
    }

    public static class EagTokenHelpers
    {
        /// <summary>
        /// Pulls the <c>oid</c> claim out of a JWT WITHOUT verifying its
        /// signature.
        ///
        /// This is intentional and fine for its actual purpose here —
        /// labeling YOUR OWN outbound request/log lines — but it is NOT
        /// a substitute for real validation, and its output must never
        /// be used to make an authorization decision in your own code.
        /// The EAG gateway independently validates every token's
        /// signature, issuer, and audience on every call
        /// (guides/agent-hierarchy-attribution.md, section 4); this
        /// method exists only so your logging/telemetry can show the
        /// right value, not so your code can decide whether to trust
        /// the caller.
        ///
        /// Deliberately reads <c>oid</c>, not <c>sub</c> — <c>sub</c> is
        /// pairwise-pseudonymous per resource in Entra ID and will NOT
        /// match the same user across different audiences/hops. Using
        /// it here would silently break the chargeback roll-up this
        /// whole mechanism exists for.
        /// </summary>
        public static string? ExtractOid(string jwtToken)
        {
            try
            {
                var handler = new JwtSecurityTokenHandler();
                var token = handler.ReadJwtToken(jwtToken);
                return token.Claims.FirstOrDefault(c => c.Type == "oid")?.Value;
            }
            catch (Exception)
            {
                return null;
            }
        }

        /// <summary>
        /// Builds the Authorization + x-agent-* headers for a call into
        /// the EAG gateway (design doc section 1) — send these alongside
        /// the OBO token from <see cref="EagOboClient.GetGatewayTokenAsync"/>.
        /// </summary>
        /// <param name="gatewayAccessToken">The token from GetGatewayTokenAsync.</param>
        /// <param name="rootId">
        /// Pass through the value YOUR OWN caller gave you, if you
        /// received one (you're a sub-agent being called by another
        /// agent). Pass null if you're the first hop — a fresh ID is
        /// generated for you. Whatever value ends up here, everything
        /// YOU call downstream must propagate the identical value
        /// onward, unchanged — that's what makes it a roll-up key.
        /// </param>
        /// <param name="callerType">
        /// "root" for the first hop, "sub-agent" or "mcp-tool" otherwise.
        /// </param>
        /// <param name="depth">
        /// Your own incoming depth plus one. Pass 0 explicitly if this
        /// call IS the root.
        /// </param>
        public static Dictionary<string, string> BuildHierarchyHeaders(
            string gatewayAccessToken,
            string? rootId = null,
            string callerType = "sub-agent",
            int depth = 1)
        {
            return new Dictionary<string, string>
            {
                ["Authorization"] = $"Bearer {gatewayAccessToken}",
                ["x-agent-root-id"] = rootId ?? Guid.NewGuid().ToString(),
                ["x-agent-caller-type"] = callerType,
                ["x-agent-depth"] = depth.ToString(),
            };
        }
    }

    // ---------------------------------------------------------------------
    // Example usage — a sub-agent receiving a request and calling the
    // gateway on behalf of whoever called it. Adapt the ASP.NET Core
    // parts to your own framework/hosting model.
    // ---------------------------------------------------------------------
    public class ExampleSubAgentHandler
    {
        private readonly EagOboClient _oboClient;
        private readonly HttpClient _httpClient;
        private readonly string _gatewayUrl;
        private readonly string[] _gatewayScopes;

        public ExampleSubAgentHandler(EagOboClient oboClient, HttpClient httpClient, string gatewayUrl, string[] gatewayScopes)
        {
            _oboClient = oboClient;
            _httpClient = httpClient;
            _gatewayUrl = gatewayUrl;
            _gatewayScopes = gatewayScopes;
        }

        public async Task<JsonDocument> HandleIncomingRequestAsync(
            string inboundAuthorizationHeader,
            string? inboundRootId,
            int inboundDepth,
            object inboundBody)
        {
            // 1. Pull the caller's token off the inbound request this
            //    sub-agent just received.
            var incomingUserToken = inboundAuthorizationHeader.Replace("Bearer ", "").Trim();

            // 2. Exchange it for a token scoped to the gateway.
            var gatewayToken = await _oboClient.GetGatewayTokenAsync(incomingUserToken, _gatewayScopes);

            // 3. (Optional, for your own logging) confirm which user
            //    this resolved to — never use this to authorize anything.
            var oid = EagTokenHelpers.ExtractOid(incomingUserToken);
            Console.WriteLine($"Calling gateway on behalf of oid={oid}");

            // 4. Build the outbound headers and call the gateway.
            var headers = EagTokenHelpers.BuildHierarchyHeaders(
                gatewayAccessToken: gatewayToken,
                rootId: inboundRootId,           // propagate unchanged
                callerType: "sub-agent",
                depth: inboundDepth + 1);

            using var request = new HttpRequestMessage(HttpMethod.Post, _gatewayUrl)
            {
                Content = JsonContent.Create(inboundBody),
            };
            foreach (var (key, value) in headers)
            {
                if (key == "Authorization")
                {
                    request.Headers.Authorization = AuthenticationHeaderValue.Parse(value);
                }
                else
                {
                    request.Headers.Add(key, value);
                }
            }

            var response = await _httpClient.SendAsync(request);
            response.EnsureSuccessStatusCode();
            return await response.Content.ReadFromJsonAsync<JsonDocument>()
                   ?? throw new InvalidOperationException("Empty response body from gateway.");
        }
    }
}
