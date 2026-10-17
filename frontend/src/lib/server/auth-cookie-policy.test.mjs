import assert from "node:assert/strict";
import { test } from "node:test";
import { resolveAuthCookiePolicy } from "./auth-cookie-policy.ts";

const cases = [
  [
    "HTTP production supports a configured local origin",
    { AUTH_URL: "http://127.0.0.1:55641", NODE_ENV: "production" },
    false,
  ],
  [
    "HTTPS production keeps the secure cookie prefix",
    { AUTH_URL: "https://traccia.example", NODE_ENV: "production" },
    true,
  ],
  [
    "HTTPS development also requires secure cookies",
    { AUTH_URL: "https://local.example", NODE_ENV: "development" },
    true,
  ],
  [
    "unset production configuration fails closed",
    { NODE_ENV: "production" },
    true,
  ],
  [
    "unset development configuration supports local HTTP",
    { NODE_ENV: "development" },
    false,
  ],
  ["unknown environment fails closed", {}, true],
  [
    "invalid production URL cannot downgrade cookies",
    { AUTH_URL: "invalid", NODE_ENV: "production" },
    true,
  ],
  [
    "unsupported URL scheme cannot downgrade cookies",
    { AUTH_URL: "file:///tmp/traccia", NODE_ENV: "production" },
    true,
  ],
  [
    "legacy configured URL follows the same origin policy",
    { NEXTAUTH_URL: "https://traccia.example", NODE_ENV: "development" },
    true,
  ],
  [
    "AUTH_URL takes priority over the legacy alias",
    {
      AUTH_URL: "https://traccia.example",
      NEXTAUTH_URL: "http://localhost",
      NODE_ENV: "production",
    },
    true,
  ],
];

for (const [name, environment, secure] of cases) {
  test(name, () => {
    assert.deepEqual(resolveAuthCookiePolicy(environment), {
      secure,
      sessionCookieName: secure
        ? "__Secure-authjs.session-token"
        : "authjs.session-token",
    });
  });
}
