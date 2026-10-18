import { createCommentOperationStore } from "./operation-state";
import type { CommentCommand } from "./comment.service";
export const commentOperations = createCommentOperationStore<CommentCommand>();
