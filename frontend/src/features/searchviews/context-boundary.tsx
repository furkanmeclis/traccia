"use client";
import { useEffect } from "react";
import { usePathname } from "next/navigation";
import { useAuth } from "@/providers/auth-provider";
import { captures, viewOperations } from "./search-store";
export function SearchContextBoundary() {
  const { user } = useAuth(),
    pathname = usePathname(),
    workspaceID = pathname.match(/^\/w\/([0-9a-f-]{36})(?:\/|$)/i)?.[1];
  useEffect(() => {
    captures.activate(user?.uuid, workspaceID);
    viewOperations.activate(user?.uuid, workspaceID);
  }, [user?.uuid, workspaceID]);
  return null;
}
