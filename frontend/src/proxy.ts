export { auth as proxy } from "@/auth";

export const config = {
  matcher: [
    "/",
    "/profile/:path*",
    "/platform/:path*",
    "/workspaces/:path*",
    "/w/:path*",
  ],
};
