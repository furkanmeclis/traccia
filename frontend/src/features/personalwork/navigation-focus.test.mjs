import test from "node:test";
import assert from "node:assert/strict";
import { createNavigationFocus } from "./navigation-focus.ts";
test("same-route and hash selection consume focus after close, cross-workspace request survives menu remount until destination DOM exists", () => {
  const focus = createNavigationFocus(),
    calls = [];
  const nodes = new Map();
  function node(id) {
    const attrs = new Set();
    return {
      hasAttribute: (key) => attrs.has(key),
      setAttribute: (key, value) => {
        assert.equal(value, "-1");
        attrs.add(key);
      },
      focus: () => calls.push(id),
    };
  }
  const root = { getElementById: (id) => nodes.get(id) ?? null };
  nodes.set("product-main", node("product-main"));
  nodes.set("favorites", node("favorites"));
  focus.request("/w/a/home");
  assert(focus.consume("/w/a/home", root));
  assert.deepEqual(calls, ["product-main"]);
  assert.equal(focus.peek(), null);
  focus.request("/w/a/home#favorites");
  assert(focus.consume("/w/a/home", root));
  assert.deepEqual(calls, ["product-main", "favorites"]);
  focus.request("/w/b/home");
  assert(!focus.consume("/w/a/home", root));
  assert(focus.peek());
  // The old command instance is gone; the stable shell's destination effect uses the same controller.
  assert(!focus.consume("/w/b/home", { getElementById: () => null }));
  assert(focus.peek());
  assert(focus.consume("/w/b/home", root));
  assert.deepEqual(calls, ["product-main", "favorites", "product-main"]);
  assert(!focus.consume("/w/b/home", root));
  focus.request("/w/b/home#not-mounted-yet");
  assert(focus.consume("/w/b/home", root));
  assert.equal(calls.at(-1), "product-main");
  assert.equal(focus.peek(), null);
  focus.request("/home");
  focus.clear();
  assert.equal(focus.peek(), null);
});
