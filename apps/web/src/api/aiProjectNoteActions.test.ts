import { afterEach, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import {
  decideAiProjectNote,
  parseAiActionProposal,
  type AiActionProposal,
} from "./aiWorkspaceActions";
import {
  invalidateProjectNoteActionFacts,
  projectNoteQueryKey,
  projectDetailQueryKey,
} from "./hooks";

const mock = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("./client", async () => ({
  ...(await vi.importActual("./client")),
  apiRequest: mock.request,
}));
afterEach(() => vi.clearAllMocks());
const uuid = "00000000-0000-4000-8000-000000000001";
function proposal(action: "create" | "update" | "delete"): AiActionProposal {
  const fields = {
    project_id: uuid,
    project_name: "项目",
    project_status: "planning",
    project_version: 2,
    title: "笔记",
    body: "完整正文🙂",
    occurred_at: "2026-09-18T08:00:00Z",
    note_state: "visible",
  };
  return {
    id: uuid,
    generation_id: uuid,
    fingerprint: "a".repeat(64),
    action: {
      action: `project_note.${action}`,
      ...(action === "create"
        ? {}
        : { project_note_id: uuid, expected_version: 1 }),
      changes:
        action === "create"
          ? {
              project_id: uuid,
              title: fields.title,
              body: fields.body,
              occurred_at: fields.occurred_at,
            }
          : action === "update"
            ? { title: "修改后" }
            : { reason: "重复" },
    },
    preview: {
      label: action === "update" ? "修改后" : fields.title,
      before: action === "create" ? {} : { ...fields },
      after: {
        ...fields,
        ...(action === "update"
          ? { title: "修改后" }
          : action === "delete"
            ? { reason: "重复", note_state: "deleted" }
            : {}),
      },
    },
    status: "pending",
    can_confirm: true,
    result_id: null,
    result_version: null,
    route: "https://untrusted.example/",
    created_at: "2026-09-18T09:00:00Z",
    decided_at: null,
  };
}
it.each(["create", "update", "delete"] as const)(
  "validates %s with full preview and rebuilds its exact note route when possible",
  (action) => {
    const p = proposal(action);
    expect(parseAiActionProposal(p).route).toBe(
      action === "create"
        ? `/projects/${uuid}`
        : `/projects/${uuid}?note=${uuid}`,
    );
    const confirmed = {
      ...p,
      status: "confirmed",
      can_confirm: false,
      result_id: uuid,
      result_version: action === "create" ? 1 : 2,
      decided_at: "2026-09-18T10:00:00Z",
    };
    expect(parseAiActionProposal(confirmed)).toMatchObject({
      status: "confirmed",
      route: `/projects/${uuid}?note=${uuid}`,
    });
    expect(() =>
      parseAiActionProposal({ ...confirmed, result_version: 99 }),
    ).toThrow();
  },
);
it.each([
  (p: AiActionProposal) => {
    p.action.task_id = uuid;
  },
  (p: AiActionProposal) => {
    delete p.preview.before.body;
  },
  (p: AiActionProposal) => {
    p.preview.after.project_id = "not-an-id";
  },
  (p: AiActionProposal) => {
    p.preview.after.project_name = "different project";
  },
  (p: AiActionProposal) => {
    p.preview.after.title = "not the requested change";
  },
  (p: AiActionProposal) => {
    p.action.changes.body = null;
  },
  (p: AiActionProposal) => {
    p.preview.after.body = "truncated";
  },
  (p: AiActionProposal) => {
    p.preview.after.reason = "unexpected delete";
  },
  (p: AiActionProposal) => {
    p.preview.after.project_status = "archived";
  },
  (p: AiActionProposal) => {
    p.preview.tasks = [];
  },
])("rejects a malformed or inconsistent note card", (mutate) => {
  const p = proposal("update");
  mutate(p);
  expect(() => parseAiActionProposal(p)).toThrow();
});
it("sends deletion consent only for a human confirm, never for reject or create", async () => {
  for (const action of ["delete", "create"] as const) {
    const p = proposal(action);
    mock.request.mockResolvedValue({ data: p });
    for (const decision of ["confirm", "reject"] as const) {
      await decideAiProjectNote(p, decision, true);
      const [, options] = mock.request.mock.calls.at(-1)!;
      const body = JSON.parse(options.body);
      expect(body.confirm_project_note_delete).toBe(
        action === "delete" && decision === "confirm" ? true : undefined,
      );
      expect(body.fingerprint).toBe(p.fingerprint);
    }
  }
});
it("cancels stale note and project snapshots before refreshing, leaving conversations untouched", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const keys = [
    projectDetailQueryKey(uuid),
    [...projectNoteQueryKey(uuid), "list", {}],
  ];
  const releases: Array<(value: string) => void> = [];
  keys.forEach((k) => client.setQueryData(k, "current"));
  client.setQueryData(["ai", "messages"], "chat");
  const requests = keys.map((queryKey) =>
    client
      .fetchQuery({
        queryKey,
        queryFn: () => new Promise<string>((r) => releases.push(r)),
      })
      .catch(() => "cancelled"),
  );
  await invalidateProjectNoteActionFacts(client);
  releases.forEach((r) => r("old note"));
  await Promise.all(requests);
  for (const key of keys) {
    expect(client.getQueryData(key)).toBe("current");
    expect(client.getQueryState(key)?.isInvalidated).toBe(true);
  }
  expect(client.getQueryState(["ai", "messages"])?.isInvalidated).toBe(false);
  client.clear();
});
