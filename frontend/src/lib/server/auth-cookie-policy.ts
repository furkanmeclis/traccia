type AuthCookieEnvironment = {
  AUTH_URL?: string;
  NEXTAUTH_URL?: string;
  NODE_ENV?: string;
};

/** Auth.js and the BFF must use the same configured origin, never request headers. */
export function resolveAuthCookiePolicy(
  environment: AuthCookieEnvironment = process.env,
) {
  const authUrl = environment.AUTH_URL ?? environment.NEXTAUTH_URL;
  // Unconfigured production/unknown environments fail closed to secure cookies.
  let secure = !["development", "test"].includes(environment.NODE_ENV ?? "");
  if (authUrl) {
    try {
      const protocol = new URL(authUrl).protocol;
      if (protocol === "https:") secure = true;
      else if (protocol === "http:") secure = false;
    } catch {
      // Invalid configuration retains the safe environment fallback.
    }
  }
  return {
    secure,
    sessionCookieName: secure
      ? "__Secure-authjs.session-token"
      : "authjs.session-token",
  };
}

export const authCookiePolicy = resolveAuthCookiePolicy();
