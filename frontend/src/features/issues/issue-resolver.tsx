"use client";
import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useAuth } from "@/providers/auth-provider";
import { useWorkspace } from "@/features/workspaces/queries";
import { issueKeys } from "./queries";
import { issueService } from "./issue.service";
import { LoadNotice } from "./issue-ui";
export function IssueResolver({
  workspaceId,
  identifier,
}: {
  workspaceId: string;
  identifier: string;
}) {
  const { user } = useAuth();
  const router = useRouter();
  const workspace = useWorkspace(workspaceId);
  const allowed = workspace.isSuccess && !workspace.isError;
  const query = useQuery({
    queryKey: [
      ...issueKeys.all(workspaceId),
      user?.uuid,
      "resolve",
      identifier,
    ],
    enabled: allowed && !!user?.uuid,
    staleTime: 0,
    queryFn: () => issueService.resolve(workspaceId, identifier),
  });
  useEffect(() => {
    if (allowed && query.isSuccess && !query.isError)
      router.replace(`/w/${workspaceId}/issues/${query.data.issue.id}`);
  }, [
    allowed,
    query.isSuccess,
    query.isError,
    query.data,
    workspaceId,
    router,
  ]);
  return (
    <>
      <LoadNotice
        pending={workspace.isPending}
        error={workspace.isError}
        retry={() => workspace.refetch()}
      />
      {allowed && (
        <LoadNotice
          pending={query.isPending}
          error={query.isError}
          retry={() => query.refetch()}
        />
      )}
    </>
  );
}
