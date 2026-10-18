"use client";
import Link from "next/link";
import { useLocale } from "@/providers/locale-provider";
import type { IssueReference } from "./relation.service";
export function IssueReferenceLink({
  reference,
}: {
  reference: IssueReference;
}) {
  const { t } = useLocale();
  return (
    <span className="min-w-0 break-words">
      <Link
        className="text-primary underline"
        href={`/w/${reference.workspace_id}/issues/${reference.id}`}
      >
        {reference.identifier} · {reference.title}
      </Link>
      {!reference.active && (
        <span className="text-muted-foreground">
          {" "}
          · {t("relations.inactive")}
        </span>
      )}
    </span>
  );
}
