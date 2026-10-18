import { createCaptureStore } from "./capture-state";
import { createViewOperationStore } from "./operation-state";
import type { CaptureBody, SearchPage, ViewCommand } from "./search.service";
export const captures = createCaptureStore<CaptureBody, SearchPage>();
export const viewOperations = createViewOperationStore<ViewCommand>();
