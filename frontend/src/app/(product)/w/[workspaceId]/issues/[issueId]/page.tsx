import { IssueDetail } from "@/features/issues/issue-detail";
export default async function Page({
  params,
}: {
  params: Promise<{ workspaceId: string; issueId: string }>;
}) {
  const { workspaceId, issueId } = await params;
  return (
    <IssueDetail
      key={`${workspaceId}:${issueId}`}
      workspaceId={workspaceId}
      issueId={issueId}
    />
  );
}
