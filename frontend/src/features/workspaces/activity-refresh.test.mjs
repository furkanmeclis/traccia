import assert from "node:assert/strict";
import { test } from "node:test";
import { canAutomaticallyRefreshActivity } from "./activity-refresh.ts";

test("automatic activity refresh remains bounded when browsing cached history", () => {
  assert.equal(canAutomaticallyRefreshActivity(0), true);
  assert.equal(canAutomaticallyRefreshActivity(1), true);
  assert.equal(canAutomaticallyRefreshActivity(2), false);
  assert.equal(canAutomaticallyRefreshActivity(500), false);
});

test("access failures require explicit refresh, while temporary failures can recover", () => {
  for (const status of [401, 403, 404]) {
    assert.equal(canAutomaticallyRefreshActivity(1, status), false);
  }
  assert.equal(canAutomaticallyRefreshActivity(1, 500), true);
  assert.equal(canAutomaticallyRefreshActivity(2, 500), false);
});
