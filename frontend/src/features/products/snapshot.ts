import {
  withinCodePointLimit,
  withinByteLimit,
} from "../../lib/text-limits.mjs";
export type CatalogPage<T> = {
  items?: T[] | null;
  has_more: boolean;
  next_cursor?: string | null;
  product_version?: number;
  capabilities: { manage: boolean; restore?: boolean };
};
// A writable selector/snapshot must include every page from one aggregate version.
export async function collectCatalog<
  T extends { id: string },
  P extends CatalogPage<T>,
>(load: (after?: string) => Promise<P>): Promise<P & { items: T[] }> {
  const items: T[] = [];
  const ids = new Set<string>();
  const cursors = new Set<string>();
  let first: P | undefined;
  let after: string | undefined;
  do {
    const page = await load(after);
    if (first && first.product_version !== page.product_version)
      throw new Error("PRODUCT_SNAPSHOT_CHANGED");
    if (!first) first = { ...page, capabilities: { ...page.capabilities } };
    else {
      first.capabilities.manage &&= page.capabilities.manage;
      first.capabilities.restore = Boolean(
        first.capabilities.restore && page.capabilities.restore,
      );
    }
    for (const item of page.items ?? []) {
      if (!ids.has(item.id)) {
        ids.add(item.id);
        items.push(item);
      }
    }
    if (page.has_more && (!page.next_cursor || cursors.has(page.next_cursor)))
      throw new Error("INCOMPLETE_PAGINATION");
    if (page.next_cursor) cursors.add(page.next_cursor);
    after = page.has_more ? page.next_cursor! : undefined;
  } while (after);
  return { ...first!, items };
}
export function safeCatalogUrl(value: string): string | null {
  try {
    if (
      !withinByteLimit(value, 2048) ||
      value.trim() !== value ||
      /[\u0000-\u001f\u007f-\u009f]/.test(value)
    )
      return null;
    const url = new URL(value);
    const secretKeys = new Set([
      "token",
      "access_token",
      "refresh_token",
      "api_key",
      "apikey",
      "secret",
      "password",
      "signature",
      "x-amz-signature",
      "x-amz-credential",
      "authorization",
    ]);
    if (
      [...url.searchParams.keys()].some((key) =>
        secretKeys.has(key.toLowerCase()),
      )
    )
      return null;
    return (url.protocol === "http:" || url.protocol === "https:") &&
      !url.username &&
      !url.password
      ? url.href
      : null;
  } catch {
    return null;
  }
}

export function withinLimit(value: string, limit: number) {
  return withinCodePointLimit(value, limit);
}
