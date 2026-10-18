import { z } from "zod";
const uuid = z.string().uuid();
export const entrySchema = z
  .object({
    id: z.string(),
    actorID: uuid,
    command: z
      .object({
        workspaceID: uuid,
        action: z.literal("attachment.remove"),
        attachmentID: uuid,
        body: z
          .object({ expected_version: z.number().int().positive().safe() })
          .strict(),
      })
      .strict(),
    key: uuid,
    outcome: z.enum(["uncertain", "rejected"]).default("uncertain"),
    retryable: z.boolean().default(true),
    pending: z.boolean(),
    reviewed: z.boolean(),
    reviewing: z.boolean(),
    canRetry: z.boolean(),
    error: z.string().nullable(),
  })
  .strict();
export type RemovalEntry = z.infer<typeof entrySchema>;

export function removalFailure(status?: number) {
  return {
    outcome:
      status !== undefined && status < 500 && status !== 408
        ? ("rejected" as const)
        : ("uncertain" as const),
    retryable: status !== 400 && status !== 409,
  };
}
export function mayRetryRemoval(
  entry: Pick<RemovalEntry, "retryable">,
  manage: boolean,
  remove: boolean,
  removed: boolean,
  fileRemove: boolean,
) {
  return entry.retryable && manage && remove && (fileRemove || removed);
}
