"use client";
import { useEffect } from "react";
import { usePathname } from "next/navigation";
import { useAuth } from "@/providers/auth-provider";
import { commentOperations } from "./comment-store";
/** Leaving the workspace or changing actor expires every reviewed write grant. */
export function CommentContextBoundary() {
  const { user } = useAuth(),
    pathname = usePathname();
  const workspaceID = pathname.match(/^\/w\/([0-9a-f-]{36})(?:\/|$)/i)?.[1];
  useEffect(
    () => commentOperations.activate(user?.uuid, workspaceID),
    [user?.uuid, workspaceID],
  );
  return null;
}
