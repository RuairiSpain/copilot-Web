// Minimal fakes for @azure/functions's HttpRequest/InvocationContext —
// scoped exactly to what this service's handlers actually call:
// request.json() and context.log/warn/error. Mirrors
// src/quota-service/tests/helpers/fakeHttpRequest.ts's identical helper
// (this is a separate Function App with its own package.json/tsconfig,
// not sharing a test tree with quota-service, so it gets its own copy
// rather than a cross-package import).

import { HttpRequest, InvocationContext } from '@azure/functions';

export function makeFakeRequest(options: { body?: unknown; bodyThrows?: boolean }): HttpRequest {
  const fake = {
    json: async () => {
      if (options.bodyThrows) {
        throw new SyntaxError('Fake: invalid JSON');
      }
      return options.body;
    },
  };
  return fake as unknown as HttpRequest;
}

export interface FakeContextLog {
  logs: unknown[][];
  warns: unknown[][];
  errors: unknown[][];
}

export function makeFakeContext(): InvocationContext & FakeContextLog {
  const logs: unknown[][] = [];
  const warns: unknown[][] = [];
  const errors: unknown[][] = [];
  const fake = {
    logs,
    warns,
    errors,
    log: (...args: unknown[]) => {
      logs.push(args);
    },
    warn: (...args: unknown[]) => {
      warns.push(args);
    },
    error: (...args: unknown[]) => {
      errors.push(args);
    },
  };
  return fake as unknown as InvocationContext & FakeContextLog;
}

/** Minimal fake for @azure/functions's Timer parameter (refreshPricingCache
 *  ignores its contents entirely, per its own `_timer` naming — this
 *  exists just to satisfy the parameter's type at call sites). */
export function makeFakeTimer(): never {
  return {} as never;
}
