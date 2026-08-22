/**
 * Fixed identity used by the whole Playwright suite — seeded once in
 * global-setup.ts, referenced by every spec that expects to already be
 * signed in. Not a real GitHub account: see global-setup.ts for why login
 * is mocked at the session-cookie level rather than driven through GitHub's
 * real OAuth screen.
 */
export const TEST_USER = {
    id: "e2e-test-user",
    githubId: "e2e-test-github-id",
    login: "e2e-test-user",
} as const;
