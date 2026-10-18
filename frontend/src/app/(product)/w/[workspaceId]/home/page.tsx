import { PersonalHome } from "@/features/personalwork/personal-home";
export default async function HomePage({
  params,
}: {
  params: Promise<{ workspaceId: string }>;
}) {
  const { workspaceId } = await params;
  return <PersonalHome workspaceID={workspaceId} />;
}
