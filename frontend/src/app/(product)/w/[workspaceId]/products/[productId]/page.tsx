import { ProductDetail } from "@/features/products/product-detail";
export default async function Page({
  params,
}: {
  params: Promise<{ workspaceId: string; productId: string }>;
}) {
  const { workspaceId, productId } = await params;
  return <ProductDetail workspaceId={workspaceId} productId={productId} />;
}
