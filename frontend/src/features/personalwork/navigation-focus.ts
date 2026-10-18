export function createNavigationFocus() {
  let pending: { path: string; hash: string } | null = null;
  return {
    request(href: string) {
      const [path, hash = ""] = href.split("#");
      pending = { path, hash };
    },
    peek: () => pending,
    consume(pathname: string, root: Pick<Document, "getElementById">) {
      if (!pending || pathname !== pending.path) return false;
      const element =
        root.getElementById(pending.hash || "product-main") ??
        (pending.hash ? root.getElementById("product-main") : null);
      if (!element) return false;
      if (!element.hasAttribute("tabindex"))
        element.setAttribute("tabindex", "-1");
      element.focus();
      pending = null;
      return true;
    },
    clear() {
      pending = null;
    },
  };
}
export const navigationFocus = createNavigationFocus();
