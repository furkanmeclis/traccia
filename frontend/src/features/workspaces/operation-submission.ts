export type OperationSubmission<T> = {
  key: string;
  intent: string;
  input: T;
};

// Keep the complete wire input, including absolute dates and expected versions,
// stable when a submission is retried after an uncertain response.
export function prepareSubmission<T>(
  previous: OperationSubmission<T> | null,
  intent: unknown,
  buildInput: () => T,
): OperationSubmission<T> {
  const fingerprint = JSON.stringify(intent);
  if (previous?.intent === fingerprint) return previous;
  return {
    key: crypto.randomUUID(),
    intent: fingerprint,
    input: structuredClone(buildInput()),
  };
}
