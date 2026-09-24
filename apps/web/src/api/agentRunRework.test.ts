import { afterEach, describe, expect, it, vi } from "vitest";
import { getAgentRunReworkPreview } from "./agentRunRework";
import { resetRuntimeConnection } from "./client";
import type {
  AgentRunReworkContext,
  AgentRunReworkRequest,
} from "../types/models";

const id = {
  task: "018f0000-0000-7000-8000-000000000901",
  submission: "018f0000-0000-7000-8000-000000000902",
  artifact: "018f0000-0000-7000-8000-000000000903",
  other: "018f0000-0000-7000-8000-000000000904",
};
const request: AgentRunReworkRequest = {
  submissionId: id.submission,
  artifactIds: [id.artifact],
  expectedTaskVersion: 9,
};
function context(): AgentRunReworkContext {
  return {
    submission_id: id.submission,
    sequence: 2,
    review_reason: "补齐第三项，保留前两项🙂",
    reviewed_at: "2026-09-21T12:00:00.123456789Z",
    artifacts: [
      {
        id: id.artifact,
        storage_kind: "text",
        name: "先前交付.md",
        content: "原始正文🙂\n第二行",
        sha256: "a".repeat(64),
      },
    ],
  };
}
function data() {
  return { task_id: id.task, task_version: 9, rework_context: context() };
}
function respond(value: unknown) {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  resetRuntimeConnection();
});

describe("explicit read-only Agent rework preview", () => {
  it("posts only selected source identity, preserves full source text and passes cancellation", async () => {
    const fetchMock = vi.fn<typeof fetch>(async () =>
      respond({ data: data() }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const abort = new AbortController();
    await expect(
      getAgentRunReworkPreview(id.task, request, abort.signal),
    ).resolves.toEqual(context());
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining(
        `/api/v1/tasks/${id.task}/agent-runs/rework-preview`,
      ),
      expect.objectContaining({ method: "POST", signal: abort.signal }),
    );
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      rework: {
        submission_id: id.submission,
        artifact_ids: [id.artifact],
        expected_task_version: 9,
      },
    });
    expect(String(fetchMock.mock.calls[0][1]?.body)).not.toMatch(
      /provider|confirm|review_reason|content/,
    );
  });

  it("allows explicit reason-only selection without collecting old outputs", async () => {
    const response = data();
    response.rework_context.artifacts = [];
    const fetchMock = vi.fn<typeof fetch>(async () =>
      respond({ data: response }),
    );
    vi.stubGlobal("fetch", fetchMock);
    await expect(
      getAgentRunReworkPreview(id.task, { ...request, artifactIds: [] }),
    ).resolves.toMatchObject({ artifacts: [] });
    expect(
      JSON.parse(String(fetchMock.mock.calls[0][1]?.body)).rework.artifact_ids,
    ).toEqual([]);
  });

  it.each([
    [
      "missing selection",
      { submissionId: id.submission, expectedTaskVersion: 9 },
    ],
    ["null selection", { ...request, artifactIds: null }],
    [
      "duplicate selection",
      { ...request, artifactIds: [id.artifact, id.artifact] },
    ],
    [
      "oversized selection",
      {
        ...request,
        artifactIds: Array.from(
          { length: 5 },
          (_, i) => `018f0000-0000-7000-8000-00000000091${i}`,
        ),
      },
    ],
    ["zero version", { ...request, expectedTaskVersion: 0 }],
    [
      "unsafe version",
      { ...request, expectedTaskVersion: Number.MAX_SAFE_INTEGER + 1 },
    ],
    ["fractional version", { ...request, expectedTaskVersion: 1.5 }],
    ["invalid batch", { ...request, submissionId: "not-a-submission" }],
    ["forged body", { ...request, reviewReason: "override actual review" }],
  ])("rejects %s before HTTP", async (_name, input) => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    await expect(
      getAgentRunReworkPreview(id.task, input as AgentRunReworkRequest),
    ).rejects.toMatchObject({ code: "INVALID_INPUT" });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it.each(["", "not-a-task", `${id.task}?extra=1`])(
    "rejects invalid Task %s before HTTP",
    async (task) => {
      const fetchMock = vi.fn();
      vi.stubGlobal("fetch", fetchMock);
      await expect(
        getAgentRunReworkPreview(task, request),
      ).rejects.toMatchObject({ code: "INVALID_INPUT" });
      expect(fetchMock).not.toHaveBeenCalled();
    },
  );

  it.each([
    [
      "Task mismatch",
      (value: ReturnType<typeof data>) => {
        value.task_id = id.other;
      },
    ],
    [
      "version mismatch",
      (value: ReturnType<typeof data>) => {
        value.task_version++;
      },
    ],
    [
      "batch mismatch",
      (value: ReturnType<typeof data>) => {
        value.rework_context.submission_id = id.other;
      },
    ],
    [
      "selection mismatch",
      (value: ReturnType<typeof data>) => {
        value.rework_context.artifacts[0].id = id.other;
      },
    ],
    [
      "missing selected source",
      (value: ReturnType<typeof data>) => {
        value.rework_context.artifacts = [];
      },
    ],
    [
      "extra selected source",
      (value: ReturnType<typeof data>) => {
        value.rework_context.artifacts.push({
          ...value.rework_context.artifacts[0],
          id: id.other,
        });
      },
    ],
    [
      "blank reason",
      (value: ReturnType<typeof data>) => {
        value.rework_context.review_reason = " ";
      },
    ],
    [
      "NUL reason",
      (value: ReturnType<typeof data>) => {
        value.rework_context.review_reason = "a\0b";
      },
    ],
    [
      "invalid timestamp",
      (value: ReturnType<typeof data>) => {
        value.rework_context.reviewed_at = "yesterday";
      },
    ],
    [
      "invalid sequence",
      (value: ReturnType<typeof data>) => {
        value.rework_context.sequence = 0;
      },
    ],
    [
      "invalid hash",
      (value: ReturnType<typeof data>) => {
        value.rework_context.artifacts[0].sha256 = "A".repeat(64);
      },
    ],
    [
      "lone surrogate",
      (value: ReturnType<typeof data>) => {
        value.rework_context.artifacts[0].content = "\ud800";
      },
    ],
    [
      "unexpected metadata",
      (value: ReturnType<typeof data>) => {
        Object.assign(value, { provider_id: id.other });
      },
    ],
    [
      "unexpected context",
      (value: ReturnType<typeof data>) => {
        Object.assign(value.rework_context, { summary: "not requested" });
      },
    ],
    [
      "unexpected source path",
      (value: ReturnType<typeof data>) => {
        Object.assign(value.rework_context.artifacts[0], {
          relative_path: "private/store",
        });
      },
    ],
    [
      "unsupported file source",
      (value: ReturnType<typeof data>) => {
        Object.assign(value.rework_context.artifacts[0], {
          storage_kind: "file",
        });
      },
    ],
  ])("fails closed on %s from preview", async (_name, mutate) => {
    const value = data();
    mutate(value);
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(async () => respond({ data: value })),
    );
    await expect(
      getAgentRunReworkPreview(id.task, request),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it("rejects reordered sources even when the set matches", async () => {
    const value = data();
    value.rework_context.artifacts.push({
      ...value.rework_context.artifacts[0],
      id: id.other,
    });
    value.rework_context.artifacts.reverse();
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(async () => respond({ data: value })),
    );
    await expect(
      getAgentRunReworkPreview(id.task, {
        ...request,
        artifactIds: [id.artifact, id.other],
      }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it("uses the Go JSON encoded 64 KiB boundary without silently clipping", async () => {
    const value = data();
    value.rework_context.review_reason = "review";
    value.rework_context.artifacts[0].name = "output.txt";
    value.rework_context.artifacts[0].content = "";
    const overhead = new TextEncoder().encode(
      JSON.stringify(value.rework_context),
    ).length;
    value.rework_context.artifacts[0].content = "a".repeat(65_536 - overhead);
    const fetchMock = vi.fn<typeof fetch>(async () => respond({ data: value }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(
      getAgentRunReworkPreview(id.task, request),
    ).resolves.toMatchObject({
      artifacts: [{ content: value.rework_context.artifacts[0].content }],
    });
    value.rework_context.artifacts[0].content += "a";
    await expect(
      getAgentRunReworkPreview(id.task, request),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
    value.rework_context.artifacts[0].content = "<".repeat(11_000);
    await expect(
      getAgentRunReworkPreview(id.task, request),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it.each([
    {},
    { data: null },
    { data: [] },
    { data: { task_id: id.task, task_version: 9 } },
  ])("rejects incomplete envelope %#", async (body) => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(async () => respond(body)),
    );
    await expect(getAgentRunReworkPreview(id.task, request)).rejects.toThrow();
  });
});
