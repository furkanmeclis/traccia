import { ApiError } from "@/lib/api";
import { portfolioCaptures } from "./personal-store";
import { personalService, type PortfolioValues } from "./personal.service";
import { portfolioScope, type PortfolioScope } from "./portfolio-state";
export function portfolioFailure(error: unknown) {
  if (error instanceof ApiError) {
    if (
      error.status === 403 &&
      error.details.some(
        (detail) =>
          detail.field === "session_id" && detail.code === "scope_changed",
      )
    )
      return "scope_changed";
    if (
      error.code === "CONFLICT" &&
      error.details.some(
        (detail) => detail.field === "session_id" && detail.code === "expired",
      )
    )
      return "expired";
    if (error.status === 401 || error.status === 403 || error.status === 404)
      return "forbidden";
    if (error.status < 500 && error.status !== 408) return "rejected";
  }
  return "uncertain";
}
export async function sendPortfolio(scope: PortfolioScope) {
  const id = portfolioScope(scope),
    entry = portfolioCaptures.get(id);
  if (!entry) return;
  const ticket = portfolioCaptures.ticket(id);
  try {
    const page = await personalService.capture(
      {
        workspaceID: scope.workspaceID,
        path: entry.command.path,
        body: structuredClone(entry.command.body),
      },
      ticket.signal,
    );
    portfolioCaptures.success(id, ticket, page);
  } catch (error) {
    portfolioCaptures.failed(
      id,
      ticket,
      error instanceof Error ? error.message : String(error),
      portfolioFailure(error),
    );
  }
}
export function beginPortfolio(
  scope: PortfolioScope,
  body: Omit<PortfolioValues, "request_id">,
) {
  portfolioCaptures.begin(scope, (request_id) => ({
    path: `/v1/workspaces/${scope.workspaceID}/personal-work/sessions`,
    body: { ...structuredClone(body), request_id },
  }));
  return sendPortfolio(scope);
}
export function retryPortfolio(scope: PortfolioScope) {
  portfolioCaptures.retry(portfolioScope(scope));
  return sendPortfolio(scope);
}
