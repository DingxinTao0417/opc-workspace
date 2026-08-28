import { afterEach, describe, expect, it, vi } from "vitest";
import {
  forceResolveInboxItem,
  getInboxStats,
  normalizeInboxItem,
  resetRuntimeConnection,
  splitInboxItem,
} from "./client";

const inboxId = "018f0000-0000-7000-8000-000000000801";
const taskId = "018f0000-0000-7000-8000-000000000101";
const owner = {
  id: "00000000-0000-5000-8000-000000000001",
  type: "owner",
  display_name: "我",
  status: "active",
  is_builtin: true,
  version: 1,
};

function inboxPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: inboxId,
    kind: "manual",
    title: "拆分工作",
    summary: "",
    source_entity_type: "manual",
    source_entity_id: null,
    source_event_key: null,
    source_deleted_at: null,
    priority: "P1",
    status: "tracking",
    resolution_policy: "all_required_tasks_done",
    due_at: null,
    read_at: null,
    triaged_at: "2026-08-28T10:00:00Z",
    snoozed_until: null,
    resolved_by_actor_id: null,
    resolved_at: null,
    resolution_reason: null,
    resolution_mode: null,
    dismissed_by_actor_id: null,
    dismissed_at: null,
    dismiss_reason: null,
    payload_json: {},
    version: 2,
    created_at: "2026-08-28T10:00:00Z",
    updated_at: "2026-08-28T10:01:00Z",
    available_actions: [
      "edit",
      "read",
      "snooze",
      "resolve",
      "dismiss",
      "force-resolve",
    ],
    ...overrides,
  };
}

function taskPayload() {
  return {
    id: taskId,
    title: "准备发布资料",
    description: "",
    kind: "work",
    status: "todo",
    priority: "P1",
    project_id: null,
    project_name: null,
    parent_task_id: null,
    parent_task_title: null,
    completion_criteria: "",
    review_policy: "none",
    blocked_reason: null,
    blocked_at: null,
    blocked_from_status: null,
    due_date: null,
    planned_date: null,
    estimated_minutes: null,
    actual_minutes: 0,
    manual_order: null,
    version: 2,
    subtask_total: 0,
    subtask_completed: 0,
    tags: [],
    created_at: "2026-08-28T10:01:00Z",
    updated_at: "2026-08-28T10:01:00Z",
    completed_at: null,
    submitted_at: null,
    reviewed_at: null,
    current_submission_id: null,
  };
}

function assignmentPayload() {
  return {
    id: "assignment-1",
    task_id: taskId,
    role: "assignee",
    actor_id: owner.id,
    actor: owner,
    assigned_by_actor_id: owner.id,
    assigned_by_actor: owner,
    assigned_at: "2026-08-28T10:01:00Z",
    unassigned_at: null,
    reason: null,
    is_active: true,
    inferred: false,
  };
}

function relationPayload() {
  return {
    id: "relation-1",
    inbox_item_id: inboxId,
    task_ref_id: taskId,
    task_id: taskId,
    task_title_snapshot: "准备发布资料",
    task: {
      id: taskId,
      title: "准备发布资料",
      status: "todo",
      priority: "P1",
      kind: "work",
      project_id: null,
      project_name: null,
      version: 2,
    },
    relation_type: "created",
    is_required: true,
    position: 1,
    linked_by_actor_id: owner.id,
    linked_by_actor: owner,
    linked_at: "2026-08-28T10:01:00Z",
    unlinked_by_actor_id: null,
    unlinked_by_actor: null,
    unlinked_at: null,
    unlink_reason: null,
    is_active: true,
    task_deleted: false,
  };
}

function progressPayload() {
  return {
    active_total: 1,
    required_total: 1,
    required_done: 0,
    required_remaining: 1,
    required_blocked: 0,
    required_waiting_review: 0,
    required_cancelled: 0,
    percent: 0,
    all_required_done: false,
  };
}

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("Inbox orchestration API contract", () => {
  it("normalizes automatic policy/mode and sends an atomic split contract", async () => {
    expect(
      normalizeInboxItem(
        inboxPayload({
          status: "resolved",
          resolved_by_actor_id: "00000000-0000-5000-8000-000000000002",
          resolved_at: "2026-08-28T10:02:00Z",
          resolution_reason: "所有必需任务已完成",
          resolution_mode: "automatic",
          version: 3,
          available_actions: ["reopen"],
        }),
      ),
    ).toMatchObject({
      resolutionPolicy: "all_required_tasks_done",
      resolutionMode: "automatic",
    });

    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        data: {
          inbox_item: inboxPayload(),
          created: [
            {
              key: "task-1",
              task: taskPayload(),
              assignments: [assignmentPayload()],
              relation: relationPayload(),
            },
          ],
          progress: progressPayload(),
        },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const result = await splitInboxItem(
      {
        inboxItemId: inboxId,
        expectedVersion: 1,
        resolutionPolicy: "all_required_tasks_done",
        tasks: [
          {
            key: "task-1",
            parentKey: null,
            title: "准备发布资料",
            description: "",
            kind: "work",
            priority: "P1",
            projectId: null,
            completionCriteria: "",
            tagIds: [],
            dueDate: null,
            plannedDate: null,
            estimatedMinutes: null,
            reviewPolicy: "none",
            isRequired: true,
            assigneeActorId: owner.id,
          },
        ],
      },
      "split-key",
    );
    expect(result.created[0]).toMatchObject({
      key: "task-1",
      task: { id: taskId },
      relation: { relationType: "created", isRequired: true },
    });
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const headers = new Headers(init.headers);
    expect(headers.get("If-Match")).toBe('"1"');
    expect(headers.get("Idempotency-Key")).toBe("split-key");
    expect(JSON.parse(String(init.body))).toMatchObject({
      resolution_policy: "all_required_tasks_done",
      tasks: [
        {
          key: "task-1",
          parent_key: null,
          assignee_actor_id: owner.id,
          is_required: true,
        },
      ],
    });
  });

  it("requires an explicit force confirmation payload", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        data: inboxPayload({
          status: "resolved",
          resolved_by_actor_id: owner.id,
          resolved_at: "2026-08-28T10:03:00Z",
          resolution_reason: "业务例外",
          resolution_mode: "forced",
          version: 3,
          available_actions: ["reopen"],
        }),
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const result = await forceResolveInboxItem(
      {
        id: inboxId,
        expectedVersion: 2,
        reason: "业务例外",
        confirm: true,
      },
      "force-key",
    );
    expect(result.resolutionMode).toBe("forced");
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(JSON.parse(String(init.body))).toEqual({
      confirm: true,
      reason: "业务例外",
    });
    expect(new Headers(init.headers).get("Idempotency-Key")).toBe("force-key");
  });

  it("normalizes derived Inbox operational counts", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        data: {
          server_now: "2026-08-28T10:04:00Z",
          pending: 7,
          unread: 4,
          tracking: 3,
          blocked: 1,
          waiting_review: 2,
        },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(getInboxStats()).resolves.toEqual({
      serverNow: "2026-08-28T10:04:00Z",
      pending: 7,
      unread: 4,
      tracking: 3,
      blocked: 1,
      waitingReview: 2,
    });
    expect(fetchMock.mock.calls[0][0]).toContain("/api/v1/stats/inbox");
  });
});
