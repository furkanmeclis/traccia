import { IssueList } from "@/features/issues/issue-list";
export default async function Page({
  params,
  searchParams,
}: {
  params: Promise<{ workspaceId: string }>;
  searchParams: Promise<{ team_id?: string; project_id?: string }>;
}) {
  const { workspaceId } = await params;
  const filters = await searchParams;
  return (
    <IssueList
      key={workspaceId}
      workspaceId={workspaceId}
      initialTeam={filters.team_id ?? ""}
      initialProject={filters.project_id ?? ""}
    />
  );
}
