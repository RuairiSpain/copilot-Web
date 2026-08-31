import { test } from 'node:test';
import assert from 'node:assert/strict';

// index.ts's only job is registering every function file as a side effect
// of importing it (Azure Functions v4 programming model — see its own
// header comment). Nothing to assert beyond "importing it doesn't throw",
// which is enough to count its otherwise-uncovered import lines toward
// real coverage rather than leaving them permanently unexercised.
test('index: importing the entry point registers every function without throwing', async () => {
  await assert.doesNotReject(() => import('../src/index'));
});
