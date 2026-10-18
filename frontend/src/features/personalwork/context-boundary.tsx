"use client";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { usePathname } from "next/navigation";
import { useAuth } from "@/providers/auth-provider";
import { productScope } from "./scope";
import { favoriteOperations, portfolioCaptures } from "./personal-store";
import { personalContext } from "./context-state";
export function PersonalContextBoundary() {
  const cache = useQueryClient();
  const { user, isAuthenticated, bootstrapped } = useAuth(),
    pathname = usePathname(),
    workspaceID = productScope(pathname).workspaceID;
  useEffect(() => {
    const actorID = bootstrapped && isAuthenticated ? user?.uuid : undefined;
    personalContext.activate(actorID ?? null, workspaceID);
    favoriteOperations.activate(actorID, workspaceID ?? undefined);
    portfolioCaptures.activate(actorID, workspaceID ?? undefined);
    return () => {
      personalContext.activate(null, null);
      cache.removeQueries({ queryKey: ["personalwork"] });
      favoriteOperations.activate();
      portfolioCaptures.activate();
    };
  }, [user?.uuid, isAuthenticated, bootstrapped, workspaceID, cache]);
  return null;
}
