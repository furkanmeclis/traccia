import createClient, { type Middleware } from "openapi-fetch";

import { apiConfig } from "@/config/api";
import type { paths } from "@/generated/api";

import { ApiError, emitApiError, parseApiError } from "./errors";
import { runStepUpEnsure } from "@/features/step-up-engine/lib/step-up-interceptor";

type Envelope<T> = {
  success?: boolean;
  data?: T;
  meta?: { request_id?: string };
};

let refreshPromise: Promise<boolean> | null = null;
let onAuthFailure: (() => void | Promise<void>) | null = null;

export function setAuthFailureHandler(
  handler: (() => void | Promise<void>) | null,
) {
  onAuthFailure = handler;
}

async function refreshSession(): Promise<boolean> {
  const response = await fetch(`${apiConfig.baseUrl}/v1/auth/refresh`, {
    method: "POST",
    headers: { Accept: "application/json", "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify({}),
  });

  if (!response.ok) return false;

  const json = (await response.json()) as Envelope<{ authenticated?: boolean }>;
  return Boolean(json.data?.authenticated ?? json.success);
}

function enqueueRefresh() {
  if (!refreshPromise) {
    refreshPromise = refreshSession().finally(() => {
      refreshPromise = null;
    });
  }
  return refreshPromise;
}

// The transport consumes the original body before onResponse runs. Keep one
// pre-fetch clone per request for either auth refresh or step-up, then release it.
const requestReplays = new WeakMap<Request, Request>();

function discardReplay(replay: Request | undefined) {
  if (replay?.body && !replay.bodyUsed) {
    void replay.body.cancel().catch(() => undefined);
  }
}

const bffMiddleware: Middleware = {
  async onRequest({ request }) {
    const headers = new Headers(request.headers);
    if (!headers.has("Accept")) {
      headers.set("Accept", "application/json");
    }
    const outgoing = new Request(request, {
      headers,
      credentials: "include",
    });
    requestReplays.set(outgoing, outgoing.clone());
    return outgoing;
  },

  onError({ request }) {
    const replay = requestReplays.get(request);
    requestReplays.delete(request);
    discardReplay(replay);
  },

  async onResponse({ request, response }) {
    const replay = requestReplays.get(request);
    requestReplays.delete(request);
    try {
      if (response.status === 403) {
        const payload = (await response
          .clone()
          .json()
          .catch(() => null)) as { error?: { code?: string } } | null;
        if (payload?.error?.code === "STEP_UP_REQUIRED") {
          const ok = await runStepUpEnsure();
          if (ok && replay) {
            return await fetch(replay);
          }
        }
      }

      if (response.status !== 401) {
        return response;
      }

      const url = new URL(request.url);
      if (
        url.pathname.endsWith("/auth/login") ||
        url.pathname.endsWith("/auth/refresh") ||
        url.pathname.endsWith("/auth/logout")
      ) {
        return response;
      }

      const ok = await enqueueRefresh();
      if (!ok || !replay) {
        await onAuthFailure?.();
        return response;
      }

      const retried = await fetch(replay);
      if (retried.status === 401) {
        await onAuthFailure?.();
      }
      return retried;
    } finally {
      discardReplay(replay);
    }
  },
};

export const apiClient = createClient<paths>({
  baseUrl: apiConfig.baseUrl,
  credentials: "include",
});

apiClient.use(bffMiddleware);

type FetchLikeResult = {
  data?: unknown;
  error?: unknown;
  response: Response;
};

export async function unwrap<T>(
  result: FetchLikeResult,
  options?: { silent?: boolean },
): Promise<T> {
  const { data, error, response } = result;

  if (!response.ok || error) {
    const apiError = parseApiError(response.status, error ?? data);
    if (!options?.silent && apiError.code !== "STEP_UP_REQUIRED") {
      emitApiError(apiError);
    }
    throw apiError;
  }

  const envelope = data as Envelope<T> | T | undefined;
  if (
    envelope &&
    typeof envelope === "object" &&
    "data" in envelope &&
    (envelope as Envelope<T>).data !== undefined
  ) {
    return (envelope as Envelope<T>).data as T;
  }

  return data as T;
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

export async function fetchSession(): Promise<{
  authenticated: boolean;
  has_access: boolean;
  has_refresh: boolean;
  user_id: string | null;
}> {
  const response = await fetch("/api/auth/session", {
    method: "GET",
    credentials: "include",
    headers: { Accept: "application/json" },
    cache: "no-store",
  });

  if (!response.ok) {
    return {
      authenticated: false,
      has_access: false,
      has_refresh: false,
      user_id: null,
    };
  }

  const session = (await response.json()) as {
    user?: { id?: string };
    error?: string;
  };

  const authenticated = Boolean(session.user && !session.error);

  return {
    authenticated,
    has_access: authenticated,
    has_refresh: authenticated,
    user_id: session.user?.id ?? null,
  };
}
