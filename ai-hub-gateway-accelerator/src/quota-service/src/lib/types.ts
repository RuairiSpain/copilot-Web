// Mirrors the schemas in guides/quota-override-approval.md §3.

export type QuotaScopeType = 'user' | 'team';

/** Current effective state — one document per scope. */
export interface QuotaOverride {
  id: string; // "{scopeType}-{scopeId}"
  docType: 'quotaOverride';
  scopeType: QuotaScopeType;
  scopeId: string;
  subscriptionId: string;
  baselineQuota: number;
  effectiveQuota: number;
  tpmTier: 'standard' | 'elevated';
  grantedBy: string;
  requestId: string;
  expiresAt: string | null; // null = permanent
  updatedAt: string;
}

export type QuotaRequestStatus = 'Pending' | 'Approved' | 'Denied' | 'Expired';

export interface QuotaRequestStatusEvent {
  status: QuotaRequestStatus;
  at: string;
  by: string;
  note?: string;
}

/** Append-only audit trail — one document per request, statusHistory grows, never replaced. */
export interface QuotaOverrideRequest {
  id: string; // "req-{guid}"
  docType: 'quotaOverrideRequest';
  scopeType: QuotaScopeType;
  scopeId: string;
  subscriptionId: string;
  requestedBy: string;
  /** Captured from x-verified-email (quota-api-policy.xml, from the JWT's preferred_username/upn/email claim) at submission time — this is who gets emailed once the request is decided. Optional: absent if the token carried none of those claims, in which case the decided-request notification is skipped for this request rather than guessed at. */
  requestedByEmail?: string;
  currentQuota: number;
  requestedQuota: number;
  reason: string;
  durationDays: number | null; // null = permanent
  status: QuotaRequestStatus;
  /** Set when requestedQuota / currentQuota exceeds the escalation multiplier — see guides/quota-override-approval.md §7. */
  requiresEscalation: boolean;
  statusHistory: QuotaRequestStatusEvent[];
  createdAt: string;
  /** Set once quota-approval-notification's webhook/email call succeeds for the APPROVER notification of this new request — see markQuotaRequestNotified.ts. Absent, not false, so the "not yet notified" query is a plain IS_DEFINED check. */
  notifiedAt?: string;
  /** Set once the REQUESTER has been emailed that this request was decided — see markRequesterNotified.ts. Deliberately a separate field from notifiedAt: two different people, two different notifications, two different points in the lifecycle. */
  requesterNotifiedAt?: string;
}

/** What the APIM policy fragment actually needs back — kept minimal on purpose. */
export interface QuotaAllowanceResponse {
  scopeType: QuotaScopeType | 'none';
  effectiveQuota: number;
  tpmTier: 'standard' | 'elevated';
}
