import { TeamList } from "@/features/teams/team-list";
export default async function Page({
  params,
}: {
  params: Promise<{ workspaceId: string }>;
}) {
  const { workspaceId } = await params;
  return <TeamList workspaceId={workspaceId} />;
}
