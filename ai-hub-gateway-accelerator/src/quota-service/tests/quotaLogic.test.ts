import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  approverAuthorizedForScope,
  computeExpiresAt,
  hasOpenRequest,
  identityMatchesToken,
  requiresEscalation,
  resolveAllowance,
  resolveVerifiedIdentity,
  survivesMonthlyReset,
} from '../src/lib/quotaLogic';
import { QuotaOverride } from '../src/lib/types';

function makeOverride(overrides: Partial<QuotaOverride> = {}): QuotaOverride {
  return {
    id: 'user-abc',
    docType: 'quotaOverride',
    scopeType: 'user',
    scopeId: 'abc',
    subscriptionId: 'LLM-HR-ChatAgent-DEV-SUB-01',
    baselineQuota: 100000,
    effectiveQuota: 250000,
    tpmTier: 'elevated',
    grantedBy: 'approver-oid',
    requestId: 'req-1',
    expiresAt: null,
    updatedAt: new Date().toISOString(),
    ...overrides,
  };
}

test('resolveAllowance: no override falls back to baseline, tier standard', () => {
  const result = resolveAllowance(undefined, 100000);
  assert.deepEqual(result, { scopeType: 'none', effectiveQuota: 100000, tpmTier: 'standard' });
});

test('resolveAllowance: active override wins', () => {
  const result = resolveAllowance(makeOverride(), 100000);
  assert.equal(result.scopeType, 'user');
  assert.equal(result.effectiveQuota, 100000); // clamped to baseline, see next test for the un-clamped case
  assert.equal(result.tpmTier, 'elevated');
});

test('resolveAllowance: override strictly under baseline is not clamped', () => {
  const result = resolveAllowance(makeOverride({ effectiveQuota: 60000 }), 100000);
  assert.equal(result.effectiveQuota, 60000);
});

test('resolveAllowance: hard backstop — override can never exceed contract baseline', () => {
  // The override (250000) was granted when the contract's own baseline
  // was higher; the contract's token-quota has since been lowered via
  // the normal GitOps flow to 100000. The override must not win.
  const result = resolveAllowance(makeOverride({ effectiveQuota: 250000 }), 100000);
  assert.equal(result.effectiveQuota, 100000);
});

test('resolveAllowance: expired override is treated as no override', () => {
  const yesterday = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString();
  const result = resolveAllowance(makeOverride({ expiresAt: yesterday }), 100000);
  assert.deepEqual(result, { scopeType: 'none', effectiveQuota: 100000, tpmTier: 'standard' });
});

test('resolveAllowance: not-yet-expired override is honored', () => {
  const tomorrow = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString();
  const result = resolveAllowance(makeOverride({ effectiveQuota: 90000, expiresAt: tomorrow }), 100000);
  assert.equal(result.scopeType, 'user');
  assert.equal(result.effectiveQuota, 90000);
});

test('requiresEscalation: under the multiplier does not escalate', () => {
  assert.equal(requiresEscalation(100000, 200000, 3), false); // 2x < 3x
});

test('requiresEscalation: at or over the multiplier escalates', () => {
  assert.equal(requiresEscalation(100000, 300001, 3), true);
  assert.equal(requiresEscalation(100000, 300000, 3), false); // exactly 3x does not trip ">" — documents the boundary
});

test('requiresEscalation: zero/negative current quota always escalates', () => {
  assert.equal(requiresEscalation(0, 1000, 3), true);
  assert.equal(requiresEscalation(-5, 1000, 3), true);
});

test('hasOpenRequest', () => {
  assert.equal(hasOpenRequest(0), false);
  assert.equal(hasOpenRequest(1), true);
  assert.equal(hasOpenRequest(2), true);
});

test('computeExpiresAt: null durationDays means permanent (null expiresAt)', () => {
  assert.equal(computeExpiresAt(null), null);
});

test('computeExpiresAt: N days from a fixed reference date', () => {
  const now = new Date('2026-08-31T00:00:00.000Z');
  const result = computeExpiresAt(30, now);
  assert.equal(result, '2026-09-30T00:00:00.000Z');
});

test('computeExpiresAt: 0 days means expires immediately (same instant)', () => {
  const now = new Date('2026-08-31T00:00:00.000Z');
  const result = computeExpiresAt(0, now);
  assert.equal(result, now.toISOString());
});

test('resolveVerifiedIdentity: a real header value passes through, trimmed', () => {
  assert.equal(resolveVerifiedIdentity('  a1b2c3d4-1234-5678-9abc-def012345678  '), 'a1b2c3d4-1234-5678-9abc-def012345678');
  assert.equal(resolveVerifiedIdentity('oid-value'), 'oid-value');
});

test('resolveVerifiedIdentity: missing/empty/whitespace-only header is rejected (null), never falls back', () => {
  assert.equal(resolveVerifiedIdentity(undefined), null);
  assert.equal(resolveVerifiedIdentity(null), null);
  assert.equal(resolveVerifiedIdentity(''), null);
  assert.equal(resolveVerifiedIdentity('   '), null);
});

test('survivesMonthlyReset: permanent override survives by default (includePermanent=false)', () => {
  assert.equal(survivesMonthlyReset(null, false), true);
});

test('survivesMonthlyReset: permanent override is cleared when includePermanent=true', () => {
  assert.equal(survivesMonthlyReset(null, true), false);
});

test('survivesMonthlyReset: every temporary override is cleared, regardless of includePermanent', () => {
  const future = new Date(Date.now() + 10 * 24 * 60 * 60 * 1000).toISOString();
  const past = new Date(Date.now() - 10 * 24 * 60 * 60 * 1000).toISOString();
  assert.equal(survivesMonthlyReset(future, false), false);
  assert.equal(survivesMonthlyReset(future, true), false);
  assert.equal(survivesMonthlyReset(past, false), false);
  assert.equal(survivesMonthlyReset(past, true), false);
});

test('identityMatchesToken: matching oids pass', () => {
  assert.equal(identityMatchesToken('a1b2c3d4', 'a1b2c3d4'), true);
});

test('identityMatchesToken: a spoofed header (mismatched real token oid) fails — this is the actual anti-impersonation check', () => {
  assert.equal(identityMatchesToken('attacker-claimed-oid', 'real-token-oid'), false);
});

test('identityMatchesToken: an empty header oid never matches, even against an empty token oid', () => {
  assert.equal(identityMatchesToken('', ''), false);
  assert.equal(identityMatchesToken('', 'some-oid'), false);
});

test('approverAuthorizedForScope: user-scoped requests are always authorized — the deliberate residual gap, no org-hierarchy data source exists', () => {
  assert.equal(approverAuthorizedForScope(undefined, 'user', 'oid-abc'), true);
  assert.equal(approverAuthorizedForScope('Finance', 'user', 'oid-abc'), true);
});

test('approverAuthorizedForScope: team-scoped request — approver department matches scopeId', () => {
  assert.equal(approverAuthorizedForScope('Finance', 'team', 'Finance'), true);
});

test('approverAuthorizedForScope: team-scoped request — approver department does not match scopeId', () => {
  assert.equal(approverAuthorizedForScope('Engineering', 'team', 'Finance'), false);
});

test('approverAuthorizedForScope: team-scoped request — approver has no resolvable department, fails closed', () => {
  assert.equal(approverAuthorizedForScope(undefined, 'team', 'Finance'), false);
});
