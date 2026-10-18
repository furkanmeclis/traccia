import test from "node:test";
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { parseStoredJob } from "./job-schema.ts";
const id = () => randomUUID();
function fixture() {
  const job = {
    id: id(),
    actorID: id(),
    workspaceID: id(),
    draftID: id(),
    issueID: id(),
    filename: "İ 👩🏽‍💻.bin",
    contentType: "application/octet-stream",
    size: 256,
    lastModified: 1,
    sha256: "a".repeat(64),
    uploadID: id(),
    uploadVersion: 2,
    offset: 0,
    serverState: "uploading",
    expiresAt: null,
    attachmentID: null,
    attachmentVersion: null,
    fileRevision: null,
    boundIssueID: null,
    phase: "running",
    hashedBytes: 256,
    error: null,
    pending: null,
  };
  job.pending = {
    outcome: "uncertain",
    retryable: true,
    key: id(),
    command: {
      workspaceID: job.workspaceID,
      action: "upload.chunk",
      uploadID: job.uploadID,
      body: {
        draft_id: job.draftID,
        expected_version: 2,
        offset: 0,
        total_bytes: 256,
        data_base64: "AA==",
        chunk_sha256: "b".repeat(64),
      },
    },
    reviewed: true,
    canRetry: true,
    reviewError: false,
  };
  return job;
}
test("two persisted uncertainty cycles preserve exact chunk action/path identity/body/key but require a new review each time", () => {
  let job = fixture();
  const original = structuredClone(job.pending);
  const alias = structuredClone(job);
  alias.pending.command.workspaceID = alias.workspaceID.toUpperCase();
  alias.pending.command.uploadID = alias.uploadID.toUpperCase();
  alias.pending.command.body.draft_id = alias.draftID.toUpperCase();
  assert.doesNotThrow(() => parseStoredJob(alias));
  for (let i = 0; i < 2; i++) {
    job = parseStoredJob(JSON.parse(JSON.stringify(job)));
    assert.equal(job.phase, "uncertain");
    assert.equal(job.pending.reviewed, false);
    assert.equal(job.pending.canRetry, false);
    assert.equal(job.pending.key, original.key);
    assert.deepEqual(job.pending.command, original.command);
    job.pending.reviewed = true;
    job.pending.canRetry = true;
  }
});
test("persistent recovery rejects foreign workspace/draft, unknown wire properties and unsafe versions", () => {
  for (const alter of [
    (j) => (j.pending.command.workspaceID = id()),
    (j) => (j.pending.command.body.draft_id = id()),
    (j) => (j.pending.command.body.unknown = true),
    (j) =>
      (j.pending.command.body.expected_version = Number.MAX_SAFE_INTEGER + 1),
  ]) {
    const job = fixture();
    alter(job);
    assert.throws(() => parseStoredJob(job));
  }
});
test("completed and paused jobs remain stored; reload never invents a new upload identity or claims active transfer", () => {
  const a = fixture();
  a.pending = null;
  a.phase = "running";
  const b = parseStoredJob(a);
  assert.equal(b.phase, "paused");
  assert.equal(b.uploadID, a.uploadID);
  assert.equal(b.offset, a.offset);
  const c = {
    ...b,
    phase: "bound",
    attachmentID: id(),
    attachmentVersion: 1,
    fileRevision: id(),
    boundIssueID: id(),
  };
  assert.deepEqual(parseStoredJob(c), c);
});

test("persisted create and attachment commands remain tied to displayed file/resource while binding may choose another destination", () => {
  const job = fixture();
  job.pending.command = {
    workspaceID: job.workspaceID,
    action: "upload.create",
    body: {
      draft_id: job.draftID,
      filename: job.filename,
      content_type: job.contentType,
      size_bytes: job.size,
      sha256: job.sha256,
    },
  };
  assert.doesNotThrow(() => parseStoredJob(job));
  for (const field of ["filename", "content_type", "size_bytes", "sha256"]) {
    const bad = structuredClone(job);
    bad.pending.command.body[field] =
      field === "size_bytes" ? bad.size + 1 : "different";
    assert.throws(() => parseStoredJob(bad));
  }
  job.attachmentID = id();
  job.pending.command = {
    workspaceID: job.workspaceID,
    action: "attachment.bind",
    attachmentID: job.attachmentID,
    body: { draft_id: job.draftID, issue_id: id(), expected_version: 1 },
  };
  assert.doesNotThrow(() => parseStoredJob(job));
  job.pending.command.attachmentID = id();
  assert.throws(() => parseStoredJob(job), /ATTACHMENT_IDENTITY_MISMATCH/);
});
