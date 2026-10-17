import { SharedLabels } from "@/features/teams/label-settings";
export default async function Page({
  params,
}: {
  params: Promise<{ workspaceId: string }>;
}) {
  const { workspaceId } = await params;
  return <SharedLabels workspaceId={workspaceId} />;
}
