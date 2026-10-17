// Infinite queries refresh every cached page, so browsing history requires an
// explicit refresh to keep automatic network work bounded to the latest page.
export function canAutomaticallyRefreshActivity(
  pageCount: number,
  errorStatus?: number,
) {
  return pageCount <= 1 && ![401, 403, 404].includes(errorStatus ?? 0);
}
