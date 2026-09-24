import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { workspaceApi } from "../api/workspace";
import { useAiProjectFiles } from "./aiProjectFiles";
import { useAiProjectFileReview } from "./aiProjectFileReview";
import {
  resetFileOperationRemindersForTests,
  useFileOperationReminders,
} from "./fileOperationReminders";
import type { AiProvider } from "../types/models";
// Exercise the isolated confirmation prototype, not production availability.
// The real disabled gate is covered by projectFileWriteGate.test.tsx.
vi.mock("../lib/projectFileWriteGate", () => ({
  projectFileWriteGate: { enabled: true, reason: "isolated prototype test" },
}));
vi.mock("../api/workspace", () => ({
  workspaceApi: {
    fileSnapshot: vi.fn(),
    validateFileSnapshot: vi.fn(async () => {}),
    releaseFileSnapshot: vi.fn(async () => {}),
    applyFileEdit: vi.fn(),
  },
}));
const session = "018f0000-0000-7000-8000-000000000101";
const provider = {
  id: "018f0000-0000-7000-8000-000000000102",
  version: 2,
  name: "模型",
  model: "test",
} as AiProvider;
const generation = "018f0000-0000-7000-8000-000000000104";
const proposal = {
  id: "018f0000-0000-7000-8000-000000000105",
  path: "a.txt",
  baseSHA256: "a".repeat(64),
  content: "\ufeffnew\r\n",
};
const meta = {
  generation_id: generation,
  session_id: session,
  provider_id: provider.id,
  model: "test",
  protocol: "openai_chat",
  sse_protocol: "v1",
};
async function begin() {
  await useAiProjectFiles
    .getState()
    .stage({ id: "root", name: "project", path: "D:/private" }, "a.txt");
  useAiProjectFiles.getState().approve(session, provider);
  const payload = (await useAiProjectFiles
    .getState()
    .prepare(session, provider))!;
  useAiProjectFiles.getState().consumeForReview(provider);
  return payload;
}
async function ready() {
  const payload = await begin();
  useAiProjectFileReview.getState().bind("request", payload);
  useAiProjectFileReview.getState().meta("request", meta);
  useAiProjectFileReview.getState().receive("request", generation, proposal);
  useAiProjectFileReview.getState().finish("request", true);
}
beforeEach(() => {
  useAiProjectFiles.getState().clear();
  resetFileOperationRemindersForTests();
  vi.clearAllMocks();
  vi.mocked(workspaceApi.fileSnapshot).mockResolvedValue({
    id: "018f0000-0000-7000-8000-000000000103",
    path: "a.txt",
    content: "old\r\n",
    size: 5,
    sha256: proposal.baseSHA256,
    expiresInSeconds: 600,
  });
});
afterEach(() => {
  useAiProjectFiles.getState().clear();
  vi.useRealTimers();
});

it("only invokes native writing from explicit apply, once, with the exact reviewed bytes", async () => {
  await ready();
  expect(workspaceApi.applyFileEdit).not.toHaveBeenCalled();
  let resolve!: (value: {
    id: string;
    status: "applied";
    backupPath: string;
  }) => void;
  vi.mocked(workspaceApi.applyFileEdit).mockReturnValueOnce(
    new Promise((r) => {
      resolve = r;
    }),
  );
  const writing = useAiProjectFileReview
    .getState()
    .apply(session, provider, proposal.id);
  await useAiProjectFileReview.getState().apply(session, provider, proposal.id);
  expect(workspaceApi.applyFileEdit).toHaveBeenCalledTimes(1);
  // The intent is persisted before the native call can outlive this window.
  expect(useFileOperationReminders.getState().reminders).toEqual([
    expect.objectContaining({ source: "ai_project_file", status: "in_flight" }),
  ]);
  expect(workspaceApi.applyFileEdit).toHaveBeenCalledWith(
    "root",
    "018f0000-0000-7000-8000-000000000103",
    proposal.id,
    proposal.content,
  );
  resolve({
    id: proposal.id,
    status: "applied",
    backupPath: "local recovery record",
  });
  await writing;
  expect(useAiProjectFileReview.getState().review?.status).toBe("applied");
  expect(useFileOperationReminders.getState().reminders).toEqual([]);
  await useAiProjectFileReview.getState().apply(session, provider, proposal.id);
  expect(workspaceApi.applyFileEdit).toHaveBeenCalledTimes(1);
});

it("binds a proposal for the second selected file and writes only that baseline", async () => {
  const second = {
    id: "018f0000-0000-7000-8000-000000000106",
    path: "b.txt",
    content: "old second",
    size: 10,
    sha256: "b".repeat(64),
    expiresInSeconds: 600,
  };
  vi.mocked(workspaceApi.fileSnapshot)
    .mockResolvedValueOnce({
      id: "018f0000-0000-7000-8000-000000000103",
      path: "a.txt",
      content: "old\r\n",
      size: 5,
      sha256: proposal.baseSHA256,
      expiresInSeconds: 600,
    })
    .mockResolvedValueOnce(second);
  const root = { id: "root", name: "project", path: "D:/private" };
  await useAiProjectFiles.getState().stage(root, "a.txt");
  await useAiProjectFiles.getState().stage(root, "b.txt");
  useAiProjectFiles.getState().approve(session, provider);
  const payload = await useAiProjectFiles.getState().prepare(session, provider);
  useAiProjectFiles.getState().consumeForReview(provider);
  const secondProposal = {
    ...proposal,
    path: second.path,
    baseSHA256: second.sha256,
    content: "updated second",
  };
  useAiProjectFileReview.getState().bind("request", payload);
  useAiProjectFileReview.getState().meta("request", meta);
  useAiProjectFileReview
    .getState()
    .receive("request", generation, secondProposal);
  useAiProjectFileReview.getState().finish("request", true);
  expect(
    useAiProjectFileReview.getState().review?.targetFile?.snapshot.path,
  ).toBe("b.txt");
  expect(useAiProjectFileReview.getState().review?.status).toBe("ready");
  await useAiProjectFileReview.getState().validate(session, provider);
  expect(workspaceApi.validateFileSnapshot).toHaveBeenLastCalledWith(
    root.id,
    second.id,
  );
  vi.mocked(workspaceApi.applyFileEdit).mockResolvedValueOnce({
    id: secondProposal.id,
    status: "applied",
    backupPath: "local recovery record",
  });
  await useAiProjectFileReview
    .getState()
    .apply(session, provider, secondProposal.id);
  expect(workspaceApi.applyFileEdit).toHaveBeenCalledWith(
    root.id,
    second.id,
    secondProposal.id,
    secondProposal.content,
  );
});

it.each(["session", "provider", "proposal", "expired", "conflict"])(
  "does not write a changed %s confirmation",
  async (mode) => {
    await ready();
    if (mode === "expired") {
      vi.useFakeTimers();
      vi.setSystemTime(Date.now() + 601000);
    }
    if (mode === "conflict")
      useAiProjectFileReview.setState((s) => ({
        review: { ...s.review!, status: "conflict" },
      }));
    await useAiProjectFileReview
      .getState()
      .apply(
        mode === "session" ? generation : session,
        mode === "provider" ? { ...provider, version: 3 } : provider,
        mode === "proposal" ? generation : proposal.id,
      );
    expect(workspaceApi.applyFileEdit).not.toHaveBeenCalled();
  },
);

it.each(["recovery_required", "already_recorded", "failed"])(
  "never automatically repeats a %s write",
  async (status) => {
    await ready();
    if (status === "failed")
      vi.mocked(workspaceApi.applyFileEdit).mockRejectedValueOnce(
        new Error("lost IPC"),
      );
    else
      vi.mocked(workspaceApi.applyFileEdit).mockResolvedValueOnce({
        id: proposal.id,
        status: status as "recovery_required" | "already_recorded",
        backupPath: "local only",
      });
    await useAiProjectFileReview
      .getState()
      .apply(session, provider, proposal.id);
    expect(useAiProjectFileReview.getState().review?.status).toBe(
      status === "recovery_required" ? status : "uncertain",
    );
    expect(useFileOperationReminders.getState().reminders).toEqual([
      expect.objectContaining({
        source: "ai_project_file",
        status: status === "recovery_required" ? status : "uncertain",
        sessionId: session,
      }),
    ]);
    await useAiProjectFileReview
      .getState()
      .apply(session, provider, proposal.id);
    expect(workspaceApi.applyFileEdit).toHaveBeenCalledTimes(1);
    expect(useFileOperationReminders.getState().reminders).toHaveLength(1);
  },
);

it("consumes disclosure without releasing review baseline, never inherits it, and only checks on click", async () => {
  await ready();
  expect(useAiProjectFiles.getState().selection).toBeNull();
  expect(useAiProjectFiles.getState().approval).toBeNull();
  expect(
    await useAiProjectFiles.getState().prepare(session, provider),
  ).toBeUndefined();
  expect(workspaceApi.releaseFileSnapshot).not.toHaveBeenCalled();
  expect(useAiProjectFileReview.getState().review?.status).toBe("ready");
  expect(workspaceApi.validateFileSnapshot).toHaveBeenCalledTimes(1);
  await useAiProjectFileReview.getState().validate(session, provider);
  expect(useAiProjectFileReview.getState().review?.status).toBe("checked");
  expect(workspaceApi.validateFileSnapshot).toHaveBeenCalledTimes(2);
  useAiProjectFileReview.getState().clear();
  expect(workspaceApi.releaseFileSnapshot).toHaveBeenCalledWith(
    "root",
    "018f0000-0000-7000-8000-000000000103",
  );
});

it.each([
  "request",
  "provider",
  "session",
  "generation",
  "path",
  "digest",
  "replaced",
  "oversized",
  "cancelled",
  "expired",
])("rejects stale or invalid %s without a disk write", async (mode) => {
  const payload = await begin();
  useAiProjectFileReview
    .getState()
    .bind(
      "request",
      mode === "request"
        ? { ...payload, files: [{ ...payload.files[0], content: "other" }] }
        : payload,
    );
  useAiProjectFileReview.getState().meta("request", {
    ...meta,
    ...(mode === "provider" ? { provider_id: session } : {}),
    ...(mode === "session" ? { session_id: provider.id } : {}),
  });
  if (mode === "expired") {
    vi.useFakeTimers();
    vi.setSystemTime(Date.now() + 601000);
  }
  if (mode === "replaced")
    useAiProjectFileReview.getState().receive("request", generation, proposal);
  useAiProjectFileReview
    .getState()
    .receive("request", mode === "generation" ? session : generation, {
      ...proposal,
      ...(mode === "path" ? { path: "other.txt" } : {}),
      ...(mode === "digest" ? { baseSHA256: "0".repeat(64) } : {}),
      ...(mode === "replaced" ? { content: "replaced" } : {}),
      ...(mode === "oversized" ? { content: "汉".repeat(12000) } : {}),
    });
  useAiProjectFileReview.getState().finish("request", mode !== "cancelled");
  expect(useAiProjectFileReview.getState().review).toBeNull();
  expect(workspaceApi.validateFileSnapshot).toHaveBeenCalledTimes(1);
  expect(workspaceApi.releaseFileSnapshot).toHaveBeenCalled();
});

it("keeps a disk conflict failed closed and ignores a late check after replacement", async () => {
  await ready();
  vi.mocked(workspaceApi.validateFileSnapshot).mockRejectedValueOnce(
    new Error("changed"),
  );
  await useAiProjectFileReview.getState().validate(session, provider);
  expect(useAiProjectFileReview.getState().review?.status).toBe("conflict");
  await useAiProjectFileReview.getState().validate(session, provider);
  expect(workspaceApi.validateFileSnapshot).toHaveBeenCalledTimes(2);
  await ready();
  let resolve!: () => void;
  vi.mocked(workspaceApi.validateFileSnapshot).mockReturnValueOnce(
    new Promise<void>((r) => {
      resolve = r;
    }),
  );
  const checking = useAiProjectFileReview
    .getState()
    .validate(session, provider);
  useAiProjectFileReview.getState().clear();
  resolve();
  await checking;
  expect(useAiProjectFileReview.getState().review).toBeNull();
});

it.each(["no-proposal", "new-message", "other-provider", "other-version"])(
  "releases %s review",
  async (mode) => {
    await ready();
    if (mode === "no-proposal") {
      const payload = await begin();
      useAiProjectFileReview.getState().bind("request", payload);
      useAiProjectFileReview.getState().finish("request", true);
    } else if (mode === "new-message")
      useAiProjectFileReview.getState().bind("next-request", undefined);
    else
      await useAiProjectFileReview.getState().validate(session, {
        ...provider,
        ...(mode === "other-provider" ? { id: session } : { version: 3 }),
      });
    expect(useAiProjectFileReview.getState().review).toBeNull();
  },
);
