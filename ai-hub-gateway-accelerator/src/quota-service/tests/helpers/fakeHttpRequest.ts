// Minimal fakes for @azure/functions's HttpRequest/InvocationContext —
// scoped exactly to what this service's handlers actually call:
// request.headers.get(name) and request.json(), context.log/warn/error.
// Cast to the real types at the call site (`as unknown as HttpRequest`)
// rather than implementing the full interface, which this service never
// exercises beyond these members.

import { HttpRequest, InvocationContext } from '@azure/functions';

export function makeFakeRequest(options: { headers?: Record<string, string>; body?: unknown; bodyThrows?: boolean }): HttpRequest {
  const headerMap = new Map(Object.entries(options.headers ?? {}));
  const fake = {
    headers: {
      get: (name: string) => headerMap.get(name.toLowerCase()) ?? headerMap.get(name) ?? null,
    },
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
