import { ProjectDetail } from "@/features/projects/project-detail";
export default async function Page({
  params,
}: {
  params: Promise<{ workspaceId: string; projectId: string }>;
}) {
  const { workspaceId, projectId } = await params;
  return <ProjectDetail workspaceId={workspaceId} projectId={projectId} />;
}
