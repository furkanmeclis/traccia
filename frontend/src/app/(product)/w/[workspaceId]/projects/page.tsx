import { ProjectList } from "@/features/projects/project-list";
export default async function Page({
  params,
}: {
  params: Promise<{ workspaceId: string }>;
}) {
  const { workspaceId } = await params;
  return <ProjectList workspaceId={workspaceId} />;
}
