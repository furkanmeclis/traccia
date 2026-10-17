import { TeamDetail } from "@/features/teams/team-detail";
export default async function Page({
  params,
}: {
  params: Promise<{ workspaceId: string; teamId: string }>;
}) {
  const { workspaceId, teamId } = await params;
  return <TeamDetail workspaceId={workspaceId} teamId={teamId} />;
}
