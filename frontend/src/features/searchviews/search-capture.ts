import { ApiError } from "@/lib/api";
import { captures } from "./search-store";
import { searchService } from "./search.service";
import { captureScope, type CaptureScope } from "./capture-state";
import type { Query } from "./query";
export function isExpiredCapture(error: unknown) {
  return (
    error instanceof ApiError &&
    error.code === "CONFLICT" &&
    error.details.some(
      (detail) => detail.field === "session_id" && detail.code === "expired",
    )
  );
}
export async function sendCapture(scope: CaptureScope) {
  const id = captureScope(scope),
    entry = captures.get(id);
  if (!entry) return;
  const ticket = captures.ticket(id);
  try {
    const page = await searchService.capture(
      {
        workspaceID: scope.workspaceID,
        resource: scope.resource,
        path: entry.command.path,
        body: structuredClone(entry.command.body),
      },
      ticket.signal,
    );
    captures.success(id, ticket, page);
  } catch (error) {
    captures.failed(
      id,
      ticket,
      error instanceof Error ? error.message : String(error),
      isExpiredCapture(error)
        ? "expired"
        : error instanceof ApiError
          ? error.status === 401 || error.status === 403
            ? "forbidden"
            : error.status < 500 && error.status !== 408
              ? "rejected"
              : "uncertain"
          : "uncertain",
    );
  }
}
export function beginCapture(
  scope: CaptureScope,
  query: Query,
  ttl: number | null,
) {
  captures.begin(scope, (request_id) => ({
    path: `/v1/workspaces/${scope.workspaceID}/search/${scope.resource === "issue" ? "issues" : scope.resource === "project" ? "projects" : "teams"}/sessions`,
    body: { request_id, query, ttl_seconds: ttl, limit: 100 },
  }));
  return sendCapture(scope);
}
export function retryCapture(scope: CaptureScope) {
  captures.retry(captureScope(scope));
  return sendCapture(scope);
}
