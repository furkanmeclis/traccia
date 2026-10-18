import { IssueResolver } from "@/features/issues/issue-resolver";
export default async function Page({
  params,
}: {
  params: Promise<{ workspaceId: string; identifier: string }>;
}) {
  const { workspaceId, identifier } = await params;
  return (
    <IssueResolver
      key={`${workspaceId}:${identifier}`}
      workspaceId={workspaceId}
      identifier={identifier}
    />
  );
}
