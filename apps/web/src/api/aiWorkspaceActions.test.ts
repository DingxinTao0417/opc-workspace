import { describe, expect, it, vi } from "vitest";
import type { AiActionProposal } from "./aiWorkspaceActions";
import {
  parseAiActionProposal,
  getAiWorkspaceActions,
  decideAiWorkspaceAction,
  decideAiTaskDelete,
  decideAiProjectDelete,
  decideAiClientDelete,
  decideAiContentItemDelete,
  decideAiContentPublished,
  decideAiRoadmapMilestoneDelete,
  decideAiTagDelete,
  decideAiAgentDelegation,
  confirmAiAgentDelegationBatch,
  decideAiAgentFollowup,
} from "./aiWorkspaceActions";
const mocks = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("./client", async () => ({
  ...(await vi.importActual("./client")),
  apiRequest: mocks.request,
}));
const valid = {
  id: "00000000-0000-4000-8000-000000000001",
  generation_id: "00000000-0000-4000-8000-000000000002",
  fingerprint: "a".repeat(64),
  action: { action: "task.create", changes: { title: "新任务" } },
  preview: {
    label: "新任务",
    before: {},
    after: { title: "新任务", status: "todo" },
  },
  status: "pending",
  can_confirm: true,
  result_id: null,
  result_version: null,
  route: "https://not-trusted.example/",
  created_at: "2026-09-18T12:00:00Z",
  decided_at: null,
};
const readAllCutoff = "2026-09-18T11:59:59.123456789Z";
const readAll = {
  ...valid,
  action: {
    action: "inbox.read_all",
    changes: { through_created_at: readAllCutoff },
  },
  preview: {
    label: "将快照内 3 条未读事项标为已读",
    before: {
      snapshot_at: readAllCutoff,
      snapshot_unread_count: 3,
    },
    after: {
      through_created_at: readAllCutoff,
      snapshot_unread_count: 0,
      marked_count: 3,
    },
    inbox_read_all: {
      through_created_at: readAllCutoff,
      candidate_count: 3,
      selection_fingerprint: "b".repeat(64),
    },
  },
  route: "javascript:alert('untrusted')",
};
describe("action API contract", () => {
  it("validates an existing-child follow-up and sends separate consent", async () => {
    const child = "00000000-0000-4000-8000-000000000031";
    const pending = {
      ...valid,
      action: {
        action: "agent_delegate.followup",
        changes: {
          child_session_id: child,
          message: "Check the revised evidence.",
          scopes: ["work", "outputs"],
        },
      },
      preview: {
        label: "fact_check",
        before: {},
        after: {},
        agent_followup: {
          target: {
            parent_session_id: "00000000-0000-4000-8000-000000000032",
            child_session_id: child,
            spawn_proposal_id: "00000000-0000-4000-8000-000000000034",
            previous_generation_id: "00000000-0000-4000-8000-000000000035",
            child_version: 2,
            provider_id: "00000000-0000-4000-8000-000000000033",
            provider_version: 1,
            provider_config_version: 2,
          },
          task_name: "fact_check",
          message: "Check the revised evidence.",
          scopes: ["work", "outputs"],
          provider_name: "Local",
          model: "test-model",
          leaves_device: false,
        },
      },
      route: "",
    };
    expect(parseAiActionProposal(pending).preview.agent_followup?.message).toBe(
      "Check the revised evidence.",
    );
    const confirmed = {
      ...pending,
      status: "confirmed",
      can_confirm: false,
      result_id: child,
      result_version: 1,
      route: `/ai?session=${child}`,
      decided_at: "2026-09-18T12:01:00Z",
      agent_followup_result: {
        session_id: child,
        generation_id: valid.id,
        status: "completed",
        error_code: null,
        pending_approvals: 0,
      },
    };
    expect(parseAiActionProposal(confirmed).route).toBe(`/ai?session=${child}`);
    expect(
      parseAiActionProposal({
        ...confirmed,
        route: "",
        agent_followup_result: {
          ...confirmed.agent_followup_result,
          status: "unavailable",
          error_code: "AI_DELEGATION_CHILD_UNAVAILABLE",
        },
      }).route,
    ).toBe("");
    for (const malformed of [
      {
        ...pending,
        action: {
          ...pending.action,
          changes: { ...pending.action.changes, scopes: ["outputs", "work"] },
        },
      },
      {
        ...pending,
        preview: {
          ...pending.preview,
          agent_followup: {
            ...pending.preview.agent_followup,
            message: "Different",
          },
        },
      },
      {
        ...pending,
        preview: {
          ...pending.preview,
          agent_followup: {
            ...pending.preview.agent_followup,
            target: {
              ...pending.preview.agent_followup.target,
              child_version: 0,
            },
          },
        },
      },
      {
        ...confirmed,
        agent_followup_result: {
          ...confirmed.agent_followup_result,
          pending_approvals: -1,
        },
      },
      { ...confirmed, route: "https://example.invalid" },
    ]) {
      expect(() => parseAiActionProposal(malformed)).toThrow();
    }
    mocks.request.mockResolvedValueOnce({ data: confirmed });
    await expect(
      decideAiAgentFollowup(parseAiActionProposal(pending), "confirm", true),
    ).resolves.toMatchObject({ status: "confirmed", result_id: child });
    expect(
      JSON.parse(mocks.request.mock.calls.at(-1)?.[1]?.body),
    ).toMatchObject({
      decision: "confirm",
      confirm_agent_delegation: true,
    });
  });

  it("validates and confirms a bounded child-agent delegation", async () => {
    const childSession = "00000000-0000-4000-8000-000000000031";
    const parentSession = "00000000-0000-4000-8000-000000000032";
    const provider = "00000000-0000-4000-8000-000000000033";
    const delegation = {
      ...valid,
      action: {
        action: "agent_delegate.spawn",
        changes: {
          task_name: "fact_check",
          message: "Verify the current task facts and report evidence.",
          scopes: ["work", "outputs"],
        },
      },
      preview: {
        label: "fact_check",
        before: {},
        after: {},
        agent_delegation: {
          task_name: "fact_check",
          message: "Verify the current task facts and report evidence.",
          scopes: ["work", "outputs"],
          parent_session_id: parentSession,
          parent_generation_id: valid.generation_id,
          child_session_id: childSession,
          provider_id: provider,
          provider_version: 1,
          provider_config_version: 2,
          provider_name: "Local",
          model: "test-model",
          leaves_device: false,
          depth: 1,
          max_depth: 1,
          sibling_index: 1,
          max_children: 4,
        },
      },
      route: "",
    };
    expect(parseAiActionProposal(delegation).route).toBe("");
    const confirmed = {
      ...delegation,
      status: "confirmed",
      can_confirm: false,
      result_id: childSession,
      result_version: 1,
      route: `/ai?session=${childSession}`,
      decided_at: "2026-09-18T12:01:00Z",
      agent_delegation_result: {
        session_id: childSession,
        generation_id: valid.id,
        status: "queued",
        error_code: null,
        pending_approvals: 0,
      },
    };
    expect(parseAiActionProposal(confirmed).route).toBe(
      `/ai?session=${childSession}`,
    );
    expect(
      parseAiActionProposal({
        ...confirmed,
        agent_delegation_result: {
          ...confirmed.agent_delegation_result,
          status: "completed",
          pending_approvals: 2,
        },
      }).agent_delegation_result?.pending_approvals,
    ).toBe(2);
    for (const pending of [-1, 0.5, null]) {
      expect(() =>
        parseAiActionProposal({
          ...confirmed,
          agent_delegation_result: {
            ...confirmed.agent_delegation_result,
            pending_approvals: pending,
          },
        }),
      ).toThrow();
    }
    for (const patch of [
      { route: "https://example.com" },
      {
        action: {
          ...delegation.action,
          changes: {
            ...delegation.action.changes,
            scopes: ["outputs", "work"],
          },
        },
      },
      {
        preview: {
          ...delegation.preview,
          agent_delegation: {
            ...delegation.preview.agent_delegation,
            parent_generation_id: parentSession,
          },
        },
      },
      { result_id: childSession, result_version: 1 },
    ])
      expect(() =>
        parseAiActionProposal({ ...delegation, ...patch }),
      ).toThrow();

    mocks.request.mockResolvedValueOnce({ data: confirmed });
    await expect(
      decideAiAgentDelegation(
        parseAiActionProposal(delegation),
        "confirm",
        true,
      ),
    ).resolves.toMatchObject({
      status: "confirmed",
      result_id: childSession,
    });
    expect(
      JSON.parse(mocks.request.mock.calls.at(-1)?.[1]?.body),
    ).toMatchObject({
      decision: "confirm",
      confirm_agent_delegation: true,
    });
  });

  it("confirms an exact ordered batch of independently scoped child proposals", async () => {
    mocks.request.mockReset();
    const generationId = valid.generation_id;
    const parentSessionId = "00000000-0000-4000-8000-000000000032";
    const providerId = "00000000-0000-4000-8000-000000000033";
    const makeProposal = (
      index: number,
      taskName: string,
      message: string,
    ): AiActionProposal => {
      const childSessionId = `00000000-0000-4000-8000-00000000003${index}`;
      return parseAiActionProposal({
        ...valid,
        id: `00000000-0000-4000-8000-00000000001${index}`,
        fingerprint: String.fromCharCode(96 + index).repeat(64),
        action: {
          action: "agent_delegate.spawn",
          changes: { task_name: taskName, message, scopes: ["work"] },
        },
        preview: {
          label: taskName,
          before: {},
          after: {},
          agent_delegation: {
            task_name: taskName,
            message,
            scopes: ["work"],
            parent_session_id: parentSessionId,
            parent_generation_id: generationId,
            child_session_id: childSessionId,
            provider_id: providerId,
            provider_version: 1,
            provider_config_version: 2,
            provider_name: "Local",
            model: "test-model",
            leaves_device: false,
            depth: 1,
            max_depth: 1,
            sibling_index: index,
            max_children: 4,
          },
        },
        route: "",
      });
    };
    const delegationOf = (proposal: AiActionProposal) => {
      const delegation = proposal.preview.agent_delegation;
      if (!delegation) throw new Error("expected an agent delegation preview");
      return delegation;
    };
    const first = makeProposal(
      1,
      "first_child",
      "Review the first workstream and report evidence.",
    );
    const second = makeProposal(
      2,
      "second_child",
      "Review the second workstream and report evidence.",
    );
    const confirm = (proposal: AiActionProposal): AiActionProposal => {
      const childSessionId = delegationOf(proposal).child_session_id;
      return parseAiActionProposal({
        ...proposal,
        status: "confirmed",
        can_confirm: false,
        result_id: childSessionId,
        result_version: 1,
        route: `/ai?session=${childSessionId}`,
        decided_at: "2026-09-18T12:01:00Z",
        agent_delegation_result: {
          session_id: childSessionId,
          generation_id: proposal.id,
          status: "queued",
          error_code: null,
          pending_approvals: 0,
        },
      });
    };
    const firstResult = confirm(first);
    const secondResult = confirm(second);
    for (const malformed of [
      [first],
      [
        first,
        { ...second, generation_id: "00000000-0000-4000-8000-000000000099" },
      ],
      [first, { ...second, id: first.id }],
      [
        first,
        {
          ...second,
          preview: {
            ...second.preview,
            agent_delegation: {
              ...delegationOf(second),
              parent_session_id: "00000000-0000-4000-8000-000000000099",
            },
          },
        },
      ],
    ]) {
      await expect(
        confirmAiAgentDelegationBatch(generationId, malformed, true),
      ).rejects.toThrow();
    }
    await expect(
      confirmAiAgentDelegationBatch(generationId, [first, second], false),
    ).rejects.toThrow();
    expect(mocks.request).not.toHaveBeenCalled();

    mocks.request.mockResolvedValueOnce({ data: [firstResult, secondResult] });
    await expect(
      confirmAiAgentDelegationBatch(generationId, [first, second], true),
    ).resolves.toMatchObject([
      { id: first.id, status: "confirmed", result_id: firstResult.result_id },
      { id: second.id, status: "confirmed", result_id: secondResult.result_id },
    ]);
    const [url, options] = mocks.request.mock.calls.at(-1) ?? [];
    expect(url).toBe(
      `/api/v1/ai/generations/${generationId}/agent-delegations/confirm`,
    );
    expect(JSON.parse(options.body)).toEqual({
      confirm_agent_delegations: true,
      items: [
        { proposal_id: first.id, fingerprint: first.fingerprint },
        { proposal_id: second.id, fingerprint: second.fingerprint },
      ],
    });

    mocks.request.mockResolvedValueOnce({ data: [secondResult, firstResult] });
    await expect(
      confirmAiAgentDelegationBatch(generationId, [first, second], true),
    ).rejects.toThrow();
  });

  it("validates a complete-quarter roadmap move and derives its local link", () => {
    const milestoneId = "00000000-0000-4000-8000-000000000121";
    const anchorId = "00000000-0000-4000-8000-000000000122";
    const move = {
      ...valid,
      action: {
        action: "roadmap_milestone.move",
        roadmap_milestone_id: milestoneId,
        expected_version: 2,
        changes: {
          anchor_milestone_id: anchorId,
          expected_anchor_version: 3,
          placement: "before",
        },
      },
      preview: {
        label: "发布里程碑",
        before: {},
        after: {},
        roadmap_order: {
          milestone_id: milestoneId,
          title: "发布里程碑",
          anchor_id: anchorId,
          anchor_title: "准备里程碑",
          placement: "before",
          year: 2026,
          quarter: 3,
          group_count: 3,
          before_position: 3,
          after_position: 1,
          group_fingerprint: "a".repeat(64),
        },
      },
      route: "https://untrusted.example/roadmap",
    };
    expect(parseAiActionProposal(move).route).toBe(
      `/roadmap?milestone=${milestoneId}`,
    );
    expect(
      parseAiActionProposal({
        ...move,
        status: "confirmed",
        can_confirm: false,
        result_id: milestoneId,
        result_version: 3,
      }).route,
    ).toBe(`/roadmap?milestone=${milestoneId}`);
    for (const patch of [
      { action: { ...move.action, task_id: anchorId } },
      {
        action: {
          ...move.action,
          changes: { ...move.action.changes, placement: "first" },
        },
      },
      {
        preview: {
          ...move.preview,
          roadmap_order: {
            ...move.preview.roadmap_order,
            anchor_id: milestoneId,
          },
        },
      },
      {
        preview: {
          ...move.preview,
          roadmap_order: {
            ...move.preview.roadmap_order,
            group_fingerprint: "bad",
          },
        },
      },
      {
        preview: {
          ...move.preview,
          roadmap_order: { ...move.preview.roadmap_order, after_position: 3 },
        },
      },
      { result_id: anchorId, result_version: 3 },
    ])
      expect(() => parseAiActionProposal({ ...move, ...patch })).toThrow();
  });

  it("validates one task move, rejects forged snapshots and keeps its link local", () => {
    const taskId = "00000000-0000-4000-8000-000000000111";
    const anchorId = "00000000-0000-4000-8000-000000000112";
    const move = {
      ...valid,
      action: {
        action: "task.move",
        task_id: taskId,
        expected_version: 2,
        changes: {
          anchor_task_id: anchorId,
          expected_anchor_version: 3,
          placement: "before",
        },
      },
      preview: {
        label: "写发布说明",
        before: {},
        after: {},
        task_order: {
          task_id: taskId,
          title: "写发布说明",
          anchor_task_id: anchorId,
          anchor_title: "整理素材",
          placement: "before",
          planned_date: "2026-09-18",
          group_count: 5,
          active_count: 3,
          before_position: 3,
          after_position: 1,
          group_fingerprint: "b".repeat(64),
        },
      },
      route: "https://untrusted.example/tasks",
    };
    expect(parseAiActionProposal(move).route).toBe(`/tasks/${taskId}`);
    expect(
      parseAiActionProposal({
        ...move,
        status: "confirmed",
        can_confirm: false,
        result_id: taskId,
        result_version: 3,
      }).route,
    ).toBe(`/tasks/${taskId}`);
    for (const patch of [
      { action: { ...move.action, project_id: anchorId } },
      {
        action: {
          ...move.action,
          changes: { ...move.action.changes, placement: "first" },
        },
      },
      {
        preview: {
          ...move.preview,
          task_order: { ...move.preview.task_order, task_id: anchorId },
        },
      },
      {
        preview: {
          ...move.preview,
          task_order: { ...move.preview.task_order, group_fingerprint: "bad" },
        },
      },
      {
        preview: {
          ...move.preview,
          task_order: { ...move.preview.task_order, after_position: 3 },
        },
      },
      { result_id: anchorId, result_version: 3 },
    ])
      expect(() => parseAiActionProposal({ ...move, ...patch })).toThrow();
  });

  it("strictly validates task batch proposals as proposal-scoped receipts", () => {
    const first = "00000000-0000-4000-8000-000000000101";
    const second = "00000000-0000-4000-8000-000000000102";
    const tag = "00000000-0000-4000-8000-000000000103";
    const batch = {
      ...valid,
      action: {
        action: "task.batch_update",
        changes: {
          batch_action: "add_tags",
          items: [first, second],
          expected_versions: [1, 3],
          tag_ids: [tag],
        },
      },
      preview: {
        label: "2 个任务",
        before: {},
        after: { count: 2 },
        task_batch: {
          action: "add_tags",
          count: 2,
          items: [
            {
              task_id: first,
              title: "准备发布",
              version: 1,
              field: "tags",
              before: "无标签",
              after: "Focus",
            },
            {
              task_id: second,
              title: "整理素材",
              version: 3,
              field: "tags",
              before: "Client",
              after: "Client、Focus",
            },
          ],
        },
      },
      route: "https://evil.test/tasks/123",
    };
    expect(parseAiActionProposal(batch).route).toBe("/tasks");
    expect(
      parseAiActionProposal({
        ...batch,
        status: "confirmed",
        can_confirm: false,
        result_id: valid.id,
        result_version: 1,
      }).route,
    ).toBe("/tasks");
    for (const patch of [
      { action: { ...batch.action, task_id: first } },
      {
        action: {
          action: "task.batch_update",
          changes: { ...batch.action.changes, expected_versions: [1] },
        },
      },
      {
        preview: {
          ...batch.preview,
          task_batch: {
            ...batch.preview.task_batch,
            items: [
              {
                ...batch.preview.task_batch.items[0],
                task_id: second,
              },
              batch.preview.task_batch.items[1],
            ],
          },
        },
      },
      { preview: { ...batch.preview, after: { count: 3 } } },
      { result_id: first, result_version: 1 },
    ])
      expect(() => parseAiActionProposal({ ...batch, ...patch })).toThrow();
  });

  it("validates a batch responsibility card and rejects missing or altered actor identity", () => {
    const task = "00000000-0000-4000-8000-000000000101";
    const actor = "00000000-0000-4000-8000-000000000111";
    const identity = {
      role: "assignee",
      actor_id: actor,
      actor_name: "小李",
      actor_type: "person",
      actor_status: "active",
      actor_version: 2,
    };
    const batch = {
      ...valid,
      action: {
        action: "task.batch_update",
        changes: {
          batch_action: "set_assignee",
          items: [task],
          expected_versions: [3],
          actor_id: actor,
          reason: "交由小李处理",
        },
      },
      preview: {
        label: "1 个任务",
        before: {},
        after: { count: 1 },
        task_batch: {
          action: "set_assignee",
          count: 1,
          reason: "交由小李处理",
          items: [
            {
              task_id: task,
              title: "准备发布",
              version: 3,
              field: "assignee",
              before: "未分派",
              after: "小李",
              after_actor: identity,
            },
          ],
        },
      },
    };
    expect(parseAiActionProposal(batch).route).toBe("/tasks");
    for (const mutate of [
      (v: any) => {
        delete v.preview.task_batch.items[0].after_actor;
      },
      (v: any) => {
        v.preview.task_batch.items[0].after_actor.actor_id = task;
      },
      (v: any) => {
        v.preview.task_batch.items[0].after_actor.actor_type = "external";
      },
      (v: any) => {
        v.preview.task_batch.items[0].after_actor.actor_status = "inactive";
      },
      (v: any) => {
        v.action.changes.reason = "其他原因";
      },
      (v: any) => {
        v.preview.task_batch.items[0].assignment_id = actor;
      },
    ]) {
      const changed = structuredClone(batch);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }
  });

  it("binds batch priority and UTC due-date previews to the requested values", () => {
    const taskId = "00000000-0000-4000-8000-000000000101";
    const make = (
      batchAction: string,
      changes: Record<string, unknown>,
      field: string,
      before: string,
      after: string,
    ) => ({
      ...valid,
      action: {
        action: "task.batch_update",
        changes: {
          batch_action: batchAction,
          items: [taskId],
          expected_versions: [2],
          ...changes,
        },
      },
      preview: {
        label: "1 个任务",
        before: {},
        after: { count: 1 },
        task_batch: {
          action: batchAction,
          count: 1,
          items: [
            {
              task_id: taskId,
              title: "准备发布",
              version: 2,
              field,
              before,
              after,
            },
          ],
        },
      },
    });
    const priority = make(
      "set_priority",
      { priority: "P0" },
      "priority",
      "P2",
      "P0",
    );
    const due = make(
      "set_due_date",
      { due_date: "2026-09-25T08:30:00Z" },
      "due_date",
      "未安排",
      "2026-09-25T08:30:00Z",
    );
    expect(parseAiActionProposal(priority).route).toBe("/tasks");
    expect(parseAiActionProposal(due).preview.task_batch?.items[0].after).toBe(
      "2026-09-25T08:30:00Z",
    );
    for (const bad of [
      make("set_priority", { priority: "P9" }, "priority", "P2", "P9"),
      make("set_priority", { priority: "P0" }, "priority", "P2", "P1"),
      make(
        "set_due_date",
        { due_date: "2026-09-25T16:30:00+08:00" },
        "due_date",
        "未安排",
        "2026-09-25T08:30:00Z",
      ),
      make(
        "set_due_date",
        { due_date: "2026-09-25T16:30:00" },
        "due_date",
        "未安排",
        "2026-09-25T16:30:00",
      ),
    ])
      expect(() => parseAiActionProposal(bad)).toThrow();
    expect(
      parseAiActionProposal(
        make(
          "set_due_date",
          { due_date: null },
          "due_date",
          "2026-09-25T08:30:00Z",
          "未安排",
        ),
      ).route,
    ).toBe("/tasks");
  });

  it("validates project task breakdown and exact created-task receipts", () => {
    const projectId = "00000000-0000-4000-8000-0000000000b1";
    const taskId = "00000000-0000-4000-8000-0000000000b2";
    const draft = { key: "first", title: "准备发布" };
    const item = {
      key: "first",
      title: "准备发布",
      description: "",
      kind: "work",
      priority: "P2",
      planned_date: null,
      due_date: null,
      estimated_minutes: null,
      completion_criteria: "",
      review_policy: "none",
      tag_ids: [],
      tag_names: [],
    };
    const batch = {
      ...valid,
      action: {
        action: "task.batch_create",
        project_id: projectId,
        expected_version: 3,
        changes: { drafts: [draft] },
      },
      preview: {
        label: "在项目中创建 1 个任务",
        before: {},
        after: { count: 1 },
        task_batch_create: {
          project_id: projectId,
          project_name: "发布项目",
          project_version: 3,
          count: 1,
          items: [item],
        },
      },
      route: `/projects/${projectId}`,
    };
    expect(parseAiActionProposal(batch).route).toBe(`/projects/${projectId}`);
    const confirmed = {
      ...batch,
      status: "confirmed",
      can_confirm: false,
      result_id: valid.id,
      result_version: 1,
      task_batch_create_result: {
        project_id: projectId,
        count: 1,
        items: [
          {
            key: "first",
            id: taskId,
            title: "准备发布",
            route: `/tasks/${taskId}`,
          },
        ],
      },
    };
    expect(
      parseAiActionProposal(confirmed).task_batch_create_result?.items[0].route,
    ).toBe(`/tasks/${taskId}`);
    const actorId = "00000000-0000-4000-8000-0000000000b3";
    const assigned = {
      ...batch,
      action: {
        ...batch.action,
        changes: { drafts: [{ ...draft, assignee_actor_id: actorId }] },
      },
      preview: {
        ...batch.preview,
        task_batch_create: {
          ...batch.preview.task_batch_create,
          items: [
            {
              ...item,
              assignments: [
                {
                  role: "assignee",
                  actor_id: actorId,
                  actor_name: "小李",
                  actor_type: "person",
                  actor_status: "active",
                  actor_version: 2,
                },
              ],
            },
          ],
        },
      },
    };
    expect(
      parseAiActionProposal(assigned).preview.task_batch_create?.items[0]
        .assignments?.[0].actor_name,
    ).toBe("小李");
    expect(() =>
      parseAiActionProposal({
        ...assigned,
        preview: {
          ...assigned.preview,
          task_batch_create: {
            ...assigned.preview.task_batch_create,
            items: [
              {
                ...item,
                assignments: [
                  {
                    ...assigned.preview.task_batch_create.items[0]
                      .assignments[0],
                    actor_id: taskId,
                  },
                ],
              },
            ],
          },
        },
      }),
    ).toThrow();
    expect(() =>
      parseAiActionProposal({
        ...batch,
        action: {
          ...batch.action,
          changes: { drafts: [{ ...draft, assignee_actor_id: actorId }] },
        },
      }),
    ).toThrow();
    const reviewer = {
      role: "reviewer",
      actor_id: "00000000-0000-5000-8000-000000000001",
      actor_name: "负责人",
      actor_type: "owner",
      actor_status: "active",
      actor_version: 1,
    };
    const manualAssigned = {
      ...assigned,
      action: {
        ...assigned.action,
        changes: {
          drafts: [
            { ...draft, review_policy: "manual", assignee_actor_id: actorId },
          ],
        },
      },
      preview: {
        ...assigned.preview,
        task_batch_create: {
          ...assigned.preview.task_batch_create,
          items: [
            {
              ...assigned.preview.task_batch_create.items[0],
              review_policy: "manual",
              assignments: [
                ...assigned.preview.task_batch_create.items[0].assignments,
                reviewer,
              ],
            },
          ],
        },
      },
    };
    expect(
      parseAiActionProposal(manualAssigned).preview.task_batch_create?.items[0]
        .assignments,
    ).toHaveLength(2);
    expect(() =>
      parseAiActionProposal({
        ...manualAssigned,
        preview: {
          ...manualAssigned.preview,
          task_batch_create: {
            ...manualAssigned.preview.task_batch_create,
            items: [
              {
                ...manualAssigned.preview.task_batch_create.items[0],
                assignments: [
                  assigned.preview.task_batch_create.items[0].assignments[0],
                ],
              },
            ],
          },
        },
      }),
    ).toThrow();
    for (const changed of [
      { ...batch, action: { ...batch.action, task_id: taskId } },
      { ...batch, preview: { ...batch.preview, after: { count: 2 } } },
      {
        ...batch,
        action: {
          ...batch.action,
          changes: { drafts: [{ ...draft, status: "done" }] },
        },
      },
      {
        ...confirmed,
        task_batch_create_result: {
          ...confirmed.task_batch_create_result,
          items: [
            {
              ...confirmed.task_batch_create_result.items[0],
              route: "/tasks/other",
            },
          ],
        },
      },
    ]) {
      expect(() => parseAiActionProposal(changed)).toThrow();
    }
  });

  it("strictly validates task saved view proposals and keeps navigation local", () => {
    const viewId = "00000000-0000-4000-8000-0000000000d1";
    const tagId = "00000000-0000-4000-8000-0000000000d2";
    const definition = {
      q: "",
      status: "active",
      priority: "P0",
      kind: "",
      project_id: "",
      client_id: "",
      tag_ids: [tagId],
      planned_date: "",
      planned_from: "",
      planned_to: "",
      due_from: "",
      due_to: "",
      sort: "-due_date",
    };
    const definitionCopy = () => ({
      ...definition,
      tag_ids: [...definition.tag_ids],
    });
    const create = {
      ...valid,
      action: {
        action: "task_view.create",
        changes: { name: "本周 P0", definition: definitionCopy() },
      },
      preview: {
        label: "本周 P0",
        before: {},
        after: { name: "本周 P0" },
        task_view: {
          name: "本周 P0",
          definition: definitionCopy(),
          next_version: 1,
          tag_names: ["Focus"],
        },
      },
      route: "https://evil.test/tasks",
    };
    expect(parseAiActionProposal(create).route).toBe("/tasks");
    expect(
      parseAiActionProposal({
        ...create,
        status: "confirmed",
        can_confirm: false,
        result_id: viewId,
        result_version: 1,
      }).route,
    ).toBe("/tasks");

    const update = {
      ...valid,
      action: {
        action: "task_view.update",
        task_saved_view_id: viewId,
        expected_version: 2,
        changes: { name: "本周 P0 与 P1" },
      },
      preview: {
        label: "本周 P0 与 P1",
        before: { name: "本周 P0" },
        after: { name: "本周 P0 与 P1" },
        task_view: {
          id: viewId,
          name: "本周 P0 与 P1",
          definition: { ...definitionCopy(), priority: "" },
          version: 2,
          next_version: 3,
        },
      },
    };
    expect(parseAiActionProposal(update).route).toBe("/tasks");

    const deletion = {
      ...valid,
      action: {
        action: "task_view.delete",
        task_saved_view_id: viewId,
        expected_version: 2,
        changes: {},
      },
      preview: {
        label: "本周 P0",
        before: { name: "本周 P0" },
        after: {},
        task_view: {
          id: viewId,
          name: "本周 P0",
          definition: definitionCopy(),
          version: 2,
          deleted: true,
        },
      },
    };
    expect(parseAiActionProposal(deletion).route).toBe("/tasks");
    expect(
      parseAiActionProposal({
        ...deletion,
        status: "confirmed",
        can_confirm: false,
        result_id: viewId,
        result_version: 2,
      }).route,
    ).toBe("/tasks");

    for (const mutate of [
      (value: any) => (value.preview.task_view.definition.priority = "P1"),
      (value: any) =>
        (value.action.changes.definition.tag_ids = [tagId, tagId]),
      (value: any) =>
        (value.action.changes.definition.planned_from = "2026-09-27"),
      (value: any) => (value.action.changes.definition.unknown = "x"),
      (value: any) => (value.action.expected_version = 1),
      (value: any) => (value.action.changes.definition.q = "a".repeat(201)),
      (value: any) => (value.preview.task_view.next_version = 5),
    ]) {
      const changed = structuredClone(create);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }
    for (const mutate of [
      (value: any) => (value.action.changes = {}),
      (value: any) => (value.preview.task_view.version = 1),
      (value: any) => (value.action.task_saved_view_id = "not-a-uuid"),
      (value: any) => (value.preview.after.name = "别的名字"),
    ]) {
      const changed = structuredClone(update);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }
    for (const mutate of [
      (value: any) => (value.action.changes = { name: "x" }),
      (value: any) => (value.preview.task_view.deleted = false),
      (value: any) => (value.preview.after.name = "x"),
    ]) {
      const changed = structuredClone(deletion);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }
  });

  it("strictly validates knowledge library proposals as metadata-only commands", () => {
    const sourceId = "00000000-0000-4000-8000-0000000000a1";
    const jobId = "00000000-0000-4000-8000-0000000000a2";
    const createContent = "hello world\n";
    const create = {
      ...valid,
      action: {
        action: "knowledge_source.create",
        changes: { name: "notes.md", title: "Notes", content: createContent },
      },
      preview: {
        label: "notes.md",
        before: {},
        after: {
          name: "notes.md",
          source_type: "markdown",
          status: "indexing",
          version: 1,
          size_bytes: 12,
        },
        knowledge: {
          name: "notes.md",
          source_type: "markdown",
          status: "pending",
          version: 0,
          next_status: "indexing",
          next_version: 1,
          new_job_operation: "import",
          chunk_count: 0,
          document_count: 0,
          proposed_content_bytes: 12,
          proposed_content_sha256: "b".repeat(64),
          proposed_excerpt: createContent,
          proposed_excerpt_truncated: false,
          content_included: false,
        },
      },
      route: "https://evil.test/knowledge",
    };
    expect(parseAiActionProposal(create).route).toBe("/knowledge");
    expect(
      parseAiActionProposal({
        ...create,
        status: "confirmed",
        can_confirm: false,
        result_id: jobId,
        result_version: 1,
      }).route,
    ).toBe("/knowledge");
    for (const mutate of [
      (value: any) => (value.preview.knowledge.proposed_excerpt = "tampered"),
      (value: any) => (value.preview.knowledge.proposed_content_bytes = 11),
      (value: any) => (value.preview.after.status = "ready"),
      (value: any) => (value.preview.before = { status: "pending" }),
      (value: any) => (value.action.expected_version = 1),
      (value: any) => (value.action.changes.name = "notes.exe"),
      (value: any) => (value.action.changes.content = ""),
      (value: any) => (value.action.changes.extra = "x"),
      (value: any) =>
        (value.preview.knowledge.proposed_content_sha256 = "short"),
    ]) {
      const changed = structuredClone(create);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }
    const oversized = structuredClone(create);
    oversized.action.changes.content = "a".repeat(24001);
    oversized.preview.knowledge.proposed_content_bytes = 24001;
    oversized.preview.after.size_bytes = 24001;
    oversized.preview.knowledge.proposed_excerpt = "a".repeat(800);
    oversized.preview.knowledge.proposed_excerpt_truncated = true;
    expect(() => parseAiActionProposal(oversized)).toThrow();

    const baseKnowledge = {
      source_id: sourceId,
      name: "billing-guide.md",
      source_type: "markdown",
      status: "ready",
      version: 3,
      chunk_count: 4,
      document_count: 1,
      content_included: false,
    };
    const reindex = {
      ...valid,
      action: {
        action: "knowledge_source.reindex",
        knowledge_source_id: sourceId,
        expected_version: 3,
        changes: {},
      },
      preview: {
        label: "billing-guide.md",
        before: { status: "ready", version: 3, chunks: 4, documents: 1 },
        after: {
          status: "indexing",
          version: 4,
          chunks: 4,
          documents: 1,
        },
        knowledge: {
          ...baseKnowledge,
          next_status: "indexing",
          next_version: 4,
          new_job_operation: "reindex",
          document_version: 1,
        },
      },
      route: "https://evil.test/knowledge",
    };
    expect(parseAiActionProposal(reindex).route).toBe("/knowledge");
    expect(
      parseAiActionProposal({
        ...reindex,
        status: "confirmed",
        can_confirm: false,
        result_id: jobId,
        result_version: 4,
      }).route,
    ).toBe("/knowledge");

    const deletion = {
      ...reindex,
      action: {
        action: "knowledge_source.delete",
        knowledge_source_id: sourceId,
        expected_version: 3,
        changes: { reason: "过期示例" },
      },
      preview: {
        label: "billing-guide.md",
        before: { status: "ready", version: 3, chunks: 4, documents: 1 },
        after: { status: "deleted", version: 4, chunks: 0, documents: 0 },
        knowledge: {
          ...baseKnowledge,
          next_status: "deleted",
          next_version: 4,
        },
      },
    };
    expect(parseAiActionProposal(deletion).route).toBe("/knowledge");

    const retry = {
      ...reindex,
      action: {
        action: "knowledge_index_job.retry",
        knowledge_index_job_id: jobId,
        expected_version: 3,
        changes: {},
      },
      preview: {
        label: "billing-guide.md",
        before: {
          status: "ready",
          version: 3,
          chunks: 4,
          documents: 1,
          job_status: "failed",
          job_attempt: 1,
        },
        after: {
          status: "indexing",
          version: 4,
          chunks: 4,
          documents: 1,
          job_status: "queued",
        },
        knowledge: {
          ...baseKnowledge,
          knowledge_index_job_id: jobId,
          next_status: "indexing",
          next_version: 4,
          new_job_operation: "reindex",
          job: {
            id: jobId,
            operation: "reindex",
            status: "failed",
            attempt: 1,
            error_code: "KNOWLEDGE_INDEX_FAILED",
          },
        },
      },
    };
    expect(parseAiActionProposal(retry).route).toBe("/knowledge");

    const cancel = {
      ...retry,
      action: {
        action: "knowledge_index_job.cancel",
        knowledge_index_job_id: jobId,
        expected_version: 3,
        changes: {},
      },
      preview: {
        label: "billing-guide.md",
        before: {
          status: "ready",
          version: 3,
          chunks: 4,
          documents: 1,
          job_status: "queued",
          job_attempt: 1,
        },
        after: {
          status: "ready",
          chunks: 4,
          documents: 1,
          job_status: "cancelled",
        },
        knowledge: {
          ...baseKnowledge,
          knowledge_index_job_id: jobId,
          next_status: "ready",
          job: {
            id: jobId,
            operation: "reindex",
            status: "queued",
            attempt: 1,
          },
        },
      },
    };
    expect(parseAiActionProposal(cancel).route).toBe("/knowledge");

    for (const mutate of [
      (value: any) => (value.preview.knowledge.content_included = true),
      (value: any) => (value.preview.knowledge.version = 9),
      (value: any) => (value.preview.knowledge.source_id = jobId),
      (value: any) => (value.action.changes = { reason: "不该出现在重建索引" }),
      (value: any) => delete value.preview.knowledge,
    ]) {
      const changed = structuredClone(reindex);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }
    for (const mutate of [
      (value: any) => (value.preview.knowledge.next_status = "ready"),
      (value: any) => (value.preview.after.chunks = 2),
      (value: any) => (value.action.knowledge_source_id = jobId),
    ]) {
      const changed = structuredClone(deletion);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }
    for (const mutate of [
      (value: any) => (value.preview.knowledge.job.status = "succeeded"),
      (value: any) => (value.preview.after.job_status = "running"),
      (value: any) => (value.preview.before.job_attempt = 4),
    ]) {
      const changed = structuredClone(retry);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }
    for (const mutate of [
      (value: any) => (value.preview.knowledge.job.status = "succeeded"),
      (value: any) => (value.preview.knowledge.next_status = "indexing"),
    ]) {
      const changed = structuredClone(cancel);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }
  });

  it("strictly validates permanent Task deletion and sends separate human consent", async () => {
    mocks.request.mockClear();
    const taskId = "00000000-0000-4000-8000-000000000099";
    const impact = {
      task_deleted: true,
      child_tasks_moved_to_top_level: 2,
      submissions_deleted: 1,
      artifacts_deleted: 3,
      file_artifacts_deleted: 1,
      assignments_deleted: 1,
      agent_runs_deleted: 2,
      focus_sessions_detached: 4,
      inbox_relations_detached: 2,
      inbox_sources_coordinated: 3,
      tag_links_deleted: 2,
    };
    const deletion = {
      ...valid,
      action: {
        action: "task.delete",
        task_id: taskId,
        expected_version: 7,
        changes: {},
      },
      preview: { label: "永久删除", before: {}, after: impact },
      route: "javascript:alert('untrusted')",
    };
    expect(parseAiActionProposal(deletion).route).toBe(`/tasks/${taskId}`);
    for (const mutate of [
      (value: any) => (value.action.changes.reason = "delete"),
      (value: any) => (value.preview.before.status = "todo"),
      (value: any) => (value.preview.after.task_deleted = false),
      (value: any) => delete value.preview.after.agent_runs_deleted,
      (value: any) => (value.preview.after.artifacts_deleted = -1),
      (value: any) => (value.preview.after.extra = 0),
    ]) {
      const changed = structuredClone(deletion);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }

    const confirmed = {
      ...deletion,
      status: "confirmed",
      can_confirm: false,
      result_id: taskId,
      result_version: 7,
      route: `/tasks/${taskId}`,
      decided_at: "2026-09-18T12:01:00Z",
    };
    mocks.request.mockResolvedValueOnce({ data: confirmed });
    const result = await decideAiTaskDelete(
      parseAiActionProposal(deletion),
      "confirm",
      true,
    );
    expect(result.route).toBe("/tasks");
    expect(JSON.parse(mocks.request.mock.calls[0][1].body)).toEqual({
      fingerprint: deletion.fingerprint,
      decision: "confirm",
      confirm_task_delete: true,
    });
  });

  it("validates Tag CRUD previews and keeps permanent deletion consent human-only", async () => {
    mocks.request.mockClear();
    const tagId = "00000000-0000-4000-8000-000000000098";
    const creation = {
      ...valid,
      action: {
        action: "tag.create",
        changes: { name: "紧急", color: "#AA11CC" },
      },
      preview: {
        label: "紧急",
        before: {},
        after: { name: "紧急", color: "#AA11CC" },
      },
    };
    expect(parseAiActionProposal(creation).route).toBe("/tasks");

    const deletion = {
      ...valid,
      action: {
        action: "tag.delete",
        tag_id: tagId,
        expected_version: 3,
        changes: {},
      },
      preview: {
        label: "紧急",
        before: { name: "紧急", color: "#AA11CC", tag_version: 3 },
        after: { tag_deleted: true, detached_tasks: 2 },
      },
    };
    expect(parseAiActionProposal(deletion).route).toBe("/tasks");
    for (const mutate of [
      (value: any) => (value.action.changes.reason = "delete"),
      (value: any) => (value.preview.before.tag_version = 2),
      (value: any) => (value.preview.before.color = "#aa11cc"),
      (value: any) => (value.preview.after.detached_tasks = -1),
      (value: any) => (value.preview.after.extra = 0),
    ]) {
      const changed = structuredClone(deletion);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }

    const confirmed = {
      ...deletion,
      status: "confirmed",
      can_confirm: false,
      result_id: tagId,
      result_version: 3,
      decided_at: "2026-09-18T12:01:00Z",
    };
    mocks.request.mockResolvedValueOnce({ data: confirmed });
    const result = await decideAiTagDelete(
      parseAiActionProposal(deletion),
      "confirm",
      true,
    );
    expect(result.route).toBe("/tasks");
    expect(JSON.parse(mocks.request.mock.calls[0][1].body)).toEqual({
      fingerprint: deletion.fingerprint,
      decision: "confirm",
      confirm_tag_delete: true,
    });
  });

  it("strictly validates permanent Project deletion and sends separate human consent", async () => {
    mocks.request.mockClear();
    const projectId = "00000000-0000-4000-8000-000000000097";
    const deletion = {
      ...valid,
      action: {
        action: "project.delete",
        project_id: projectId,
        expected_version: 8,
        changes: {},
      },
      preview: {
        label: "已归档项目",
        before: { status: "archived", project_version: 8 },
        after: {
          project_deleted: true,
          project_tasks_detached: 2,
          project_draft_invoices_detached: 1,
          project_financial_entries_detached: 3,
          project_notes_deleted: 4,
          project_attachments_deleted: 2,
          project_attachment_files_deleted: 1,
          project_inbox_sources_coordinated: 1,
        },
      },
    };
    expect(parseAiActionProposal(deletion).route).toBe(
      `/projects/${projectId}`,
    );
    for (const mutate of [
      (value: any) => (value.action.changes.reason = "delete"),
      (value: any) => (value.preview.before.status = "paused"),
      (value: any) => (value.preview.before.project_version = 7),
      (value: any) => (value.preview.after.project_deleted = false),
      (value: any) => (value.preview.after.project_notes_deleted = -1),
      (value: any) => delete value.preview.after.project_attachments_deleted,
    ]) {
      const changed = structuredClone(deletion);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }

    const confirmed = {
      ...deletion,
      status: "confirmed",
      can_confirm: false,
      result_id: projectId,
      result_version: 8,
      decided_at: "2026-09-18T12:01:00Z",
    };
    mocks.request.mockResolvedValueOnce({ data: confirmed });
    const result = await decideAiProjectDelete(
      parseAiActionProposal(deletion),
      "confirm",
      true,
    );
    expect(result.route).toBe("/projects");
    expect(JSON.parse(mocks.request.mock.calls[0][1].body)).toEqual({
      fingerprint: deletion.fingerprint,
      decision: "confirm",
      confirm_project_delete: true,
    });
  });

  it("strictly validates permanent Client deletion and sends separate human consent", async () => {
    mocks.request.mockClear();
    const clientId = "00000000-0000-4000-8000-000000000096";
    const deletion = {
      ...valid,
      action: {
        action: "client.delete",
        client_id: clientId,
        expected_version: 6,
        changes: {},
      },
      preview: {
        label: "已停用客户",
        before: { status: "inactive", client_version: 6 },
        after: {
          client_deleted: true,
          client_projects_detached: 2,
          client_activities_deleted: 3,
          client_contact_history_deleted: 1,
          client_attachments_deleted: 2,
          client_attachment_files_deleted: 1,
        },
      },
    };
    expect(parseAiActionProposal(deletion).route).toBe(`/clients/${clientId}`);
    for (const mutate of [
      (value: any) => (value.action.changes.reason = "delete"),
      (value: any) => (value.preview.before.status = "active"),
      (value: any) => (value.preview.before.client_version = 5),
      (value: any) => (value.preview.after.client_deleted = false),
      (value: any) => (value.preview.after.client_activities_deleted = -1),
      (value: any) => delete value.preview.after.client_attachments_deleted,
    ]) {
      const changed = structuredClone(deletion);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }

    const confirmed = {
      ...deletion,
      status: "confirmed",
      can_confirm: false,
      result_id: clientId,
      result_version: 6,
      decided_at: "2026-09-18T12:01:00Z",
    };
    mocks.request.mockResolvedValueOnce({ data: confirmed });
    const result = await decideAiClientDelete(
      parseAiActionProposal(deletion),
      "confirm",
      true,
    );
    expect(result.route).toBe("/clients");
    expect(JSON.parse(mocks.request.mock.calls[0][1].body)).toEqual({
      fingerprint: deletion.fingerprint,
      decision: "confirm",
      confirm_client_delete: true,
    });
  });

  it("strictly validates permanent Content Item deletion and sends separate human consent", async () => {
    mocks.request.mockClear();
    const contentItemId = "00000000-0000-4000-8000-000000000095";
    const deletion = {
      ...valid,
      action: {
        action: "content_item.delete",
        content_item_id: contentItemId,
        expected_version: 4,
        changes: {},
      },
      preview: {
        label: "已归档内容",
        before: { status: "archived", content_item_version: 4 },
        after: {
          content_item_deleted: true,
          content_item_task_links_deleted: 2,
          content_item_inbox_sources_marked: 1,
        },
      },
    };
    expect(parseAiActionProposal(deletion).route).toBe(
      `/content-calendar?item=${contentItemId}`,
    );
    for (const mutate of [
      (value: any) => (value.action.changes.reason = "delete"),
      (value: any) => (value.preview.before.status = "draft"),
      (value: any) => (value.preview.before.content_item_version = 3),
      (value: any) => (value.preview.after.content_item_deleted = false),
      (value: any) =>
        (value.preview.after.content_item_task_links_deleted = -1),
      (value: any) =>
        delete value.preview.after.content_item_inbox_sources_marked,
    ]) {
      const changed = structuredClone(deletion);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }

    const confirmed = {
      ...deletion,
      status: "confirmed",
      can_confirm: false,
      result_id: contentItemId,
      result_version: 4,
      decided_at: "2026-09-18T12:01:00Z",
    };
    mocks.request.mockResolvedValueOnce({ data: confirmed });
    const result = await decideAiContentItemDelete(
      parseAiActionProposal(deletion),
      "confirm",
      true,
    );
    expect(result.route).toBe("/content-calendar");
    expect(JSON.parse(mocks.request.mock.calls[0][1].body)).toEqual({
      fingerprint: deletion.fingerprint,
      decision: "confirm",
      confirm_content_item_delete: true,
    });
  });

  it("validates a publication fact and sends only separate human verification", async () => {
    mocks.request.mockClear();
    const contentId = "00000000-0000-4000-8000-0000000000a5";
    const publishing = {
      ...valid,
      action: {
        action: "content_item.publish",
        content_item_id: contentId,
        expected_version: 3,
        changes: { external_link: " https://example.test/post " },
      },
      preview: {
        label: "已发布的文章",
        before: {
          status: "scheduled",
          published_at: null,
          external_link: null,
        },
        after: {
          status: "published",
          published_at: "confirmation_time",
          external_link: "https://example.test/post",
        },
      },
    };
    expect(parseAiActionProposal(publishing).route).toBe(
      `/content-calendar?item=${contentId}`,
    );
    for (const mutate of [
      (value: any) => {
        value.action.changes.status = "published";
      },
      (value: any) => {
        value.preview.after.external_link = "https://different.test";
      },
      (value: any) => {
        value.preview.after.published_at = "2026-09-18T12:00:00Z";
      },
      (value: any) => {
        value.preview.before.status = "archived";
      },
      (value: any) => {
        value.action.project_id = contentId;
      },
    ]) {
      const altered = structuredClone(publishing);
      mutate(altered);
      expect(() => parseAiActionProposal(altered)).toThrow();
    }
    const confirmed = {
      ...publishing,
      status: "confirmed",
      can_confirm: false,
      result_id: contentId,
      result_version: 4,
      decided_at: "2026-09-18T12:01:00Z",
    };
    mocks.request.mockResolvedValueOnce({ data: confirmed });
    const result = await decideAiContentPublished(
      parseAiActionProposal(publishing),
      "confirm",
      true,
    );
    expect(result.route).toBe(`/content-calendar?item=${contentId}`);
    expect(JSON.parse(mocks.request.mock.calls[0][1].body)).toEqual({
      fingerprint: publishing.fingerprint,
      decision: "confirm",
      confirm_content_published: true,
    });
  });

  it("strictly validates permanent roadmap milestone deletion and sends separate human consent", async () => {
    mocks.request.mockClear();
    const milestoneId = "00000000-0000-4000-8000-000000000096";
    const deletion = {
      ...valid,
      action: {
        action: "roadmap_milestone.delete",
        roadmap_milestone_id: milestoneId,
        expected_version: 2,
        changes: {},
      },
      preview: {
        label: "已归档里程碑",
        before: { status: "archived", roadmap_milestone_version: 2 },
        after: {
          roadmap_milestone_deleted: true,
          roadmap_milestone_project_links_deleted: 2,
          roadmap_milestone_inbox_sources_marked: 1,
        },
      },
    };
    expect(parseAiActionProposal(deletion).route).toBe(
      `/roadmap?milestone=${milestoneId}`,
    );
    for (const mutate of [
      (value: any) => (value.action.changes.reason = "delete"),
      (value: any) => (value.preview.before.status = "active"),
      (value: any) => (value.preview.before.roadmap_milestone_version = 1),
      (value: any) => (value.preview.after.roadmap_milestone_deleted = false),
      (value: any) =>
        (value.preview.after.roadmap_milestone_project_links_deleted = -1),
      (value: any) =>
        delete value.preview.after.roadmap_milestone_inbox_sources_marked,
    ]) {
      const changed = structuredClone(deletion);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }

    const confirmed = {
      ...deletion,
      status: "confirmed",
      can_confirm: false,
      result_id: milestoneId,
      result_version: 2,
      decided_at: "2026-09-18T12:01:00Z",
    };
    mocks.request.mockResolvedValueOnce({ data: confirmed });
    const result = await decideAiRoadmapMilestoneDelete(
      parseAiActionProposal(deletion),
      "confirm",
      true,
    );
    expect(result.route).toBe("/roadmap");
    expect(JSON.parse(mocks.request.mock.calls[0][1].body)).toEqual({
      fingerprint: deletion.fingerprint,
      decision: "confirm",
      confirm_roadmap_milestone_delete: true,
    });
  });

  it("validates automation previews, exact configs and continuing-effects consent", async () => {
    const record = {
      preset_key: "daily-today-reminder",
      name: "每日查看今日任务",
      available: true,
      unavailable_reason: "",
      trigger_type: "schedule",
      trigger_label: "每天",
      action_type: "reminder",
      action_label: "创建本地提醒",
      permission_summary: "读取时间\n创建提醒",
      rule_enabled: false,
      local_time: "09:00",
      timezone: "UTC",
    };
    const automation = {
      ...valid,
      action: {
        action: "automation.enable",
        automation_rule_id: "00000000-0000-5000-8000-000000000102",
        expected_version: 1,
        changes: {},
      },
      preview: {
        label: record.name,
        before: record,
        after: { ...record, rule_enabled: true },
      },
    };
    const parsed = parseAiActionProposal(automation);
    expect(parsed.route).toBe(
      `/settings/automation?rule=${automation.action.automation_rule_id}`,
    );
    for (const mutate of [
      (v: any) => {
        delete v.preview.after.permission_summary;
      },
      (v: any) => {
        v.preview.after.rule_enabled = "true";
      },
      (v: any) => {
        v.preview.after.action_label = "Other action";
      },
      (v: any) => {
        v.preview.before.available = false;
        v.preview.after.available = false;
      },
      (v: any) => {
        v.preview.after.local_time = "10:00";
      },
      (v: any) => {
        v.action.task_id = valid.id;
      },
      (v: any) => {
        v.action.automation_rule_id = valid.id;
      },
      (v: any) => {
        v.action.changes.script = "run";
      },
      (v: any) => {
        v.preview.after.body = "unexpected";
      },
    ]) {
      const changed = structuredClone(automation);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }
    const update = {
      ...automation,
      action: {
        ...automation.action,
        action: "automation.update",
        changes: { local_time: "08:30", timezone: "Asia/Shanghai" },
      },
      preview: {
        label: record.name,
        before: { ...record, rule_enabled: true },
        after: {
          ...record,
          rule_enabled: true,
          local_time: "08:30",
          timezone: "Asia/Shanghai",
        },
      },
    };
    expect(parseAiActionProposal(update).preview.after.local_time).toBe(
      "08:30",
    );
    for (const changes of [
      { local_time: "08:30" },
      { local_time: "25:30", timezone: "UTC" },
      { local_time: "08:30", timezone: "Local" },
      {},
    ]) {
      expect(() =>
        parseAiActionProposal({
          ...update,
          action: { ...update.action, changes },
        }),
      ).toThrow();
    }
    const event = {
      ...record,
      preset_key: "invoice-overdue-task",
      trigger_type: "event",
      action_type: "task",
      priority: "P2",
    } as Record<string, unknown>;
    delete event.local_time;
    delete event.timezone;
    expect(
      parseAiActionProposal({
        ...automation,
        action: {
          ...automation.action,
          action: "automation.update",
          automation_rule_id: "00000000-0000-5000-8000-000000000104",
          changes: { priority: "P2" },
        },
        preview: {
          label: event.name,
          before: { ...event, priority: "P1" },
          after: event,
        },
      }).preview.after.priority,
    ).toBe("P2");
    mocks.request.mockResolvedValue({ data: automation });
    await decideAiWorkspaceAction(
      parsed,
      "confirm",
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      true,
    );
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
      fingerprint: parsed.fingerprint,
      decision: "confirm",
      confirm_automation_effects: true,
    });
    await decideAiWorkspaceAction(
      parsed,
      "reject",
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      true,
    );
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).not.toHaveProperty(
      "confirm_automation_effects",
    );
  });
  it("validates full activity bodies, immutable identity and separate deletion consent", async () => {
    const record = {
      client_id: valid.id,
      client_name: "客户",
      client_status: "inactive",
      kind: "note",
      title: "实际会议备注",
      body: "<".repeat(10000),
      occurred_at: "2026-09-17T00:00:00Z",
      activity_state: "visible",
    };
    const changes = {
      client_id: record.client_id,
      kind: record.kind,
      title: record.title,
      body: record.body,
      occurred_at: record.occurred_at,
    };
    const create = {
      ...valid,
      action: { action: "client_activity.create", changes },
      preview: { label: record.title, before: {}, after: record },
    };
    expect(parseAiActionProposal(create).preview.after.body).toBe(record.body);
    expect(parseAiActionProposal(create).route).toBe(`/clients/${valid.id}`);
    const updated = {
      ...create,
      action: {
        action: "client_activity.update",
        client_activity_id: valid.generation_id,
        expected_version: 1,
        changes: { body: ">".repeat(10000) },
      },
      preview: {
        label: record.title,
        before: record,
        after: { ...record, body: ">".repeat(10000) },
      },
    };
    expect(parseAiActionProposal(updated).preview.before.body).toBe(
      record.body,
    );
    for (const mutate of [
      (v: any) => {
        delete v.preview.before.body;
      },
      (v: any) => {
        v.preview.after.body = "truncated";
      },
      (v: any) => {
        v.preview.after.client_id = valid.generation_id;
      },
      (v: any) => {
        v.action.task_id = valid.id;
      },
      (v: any) => {
        v.preview.after.kind = "system_reference";
      },
      (v: any) => {
        v.action.changes.confirm_activity_delete = true;
      },
      (v: any) => {
        v.preview.next_followup = {};
      },
    ]) {
      const bad = structuredClone(updated);
      mutate(bad);
      expect(() => parseAiActionProposal(bad)).toThrow();
    }
    const { body: _body, ...metadata } = record;
    const deleting = {
      ...valid,
      action: {
        action: "client_activity.delete",
        client_activity_id: valid.generation_id,
        expected_version: 1,
        changes: { reason: "重复" },
      },
      preview: {
        label: record.title,
        before: metadata,
        after: { ...metadata, activity_state: "deleted", reason: "重复" },
      },
    };
    const proposal = parseAiActionProposal(deleting);
    mocks.request.mockResolvedValue({ data: deleting });
    await decideAiWorkspaceAction(
      proposal,
      "confirm",
      undefined,
      undefined,
      undefined,
      undefined,
      true,
    );
    expect(JSON.parse(mocks.request.mock.calls.at(-1)![1].body)).toEqual({
      fingerprint: valid.fingerprint,
      decision: "confirm",
      confirm_activity_delete: true,
    });
    await decideAiWorkspaceAction(
      proposal,
      "reject",
      undefined,
      undefined,
      undefined,
      undefined,
      true,
    );
    expect(
      JSON.parse(mocks.request.mock.calls.at(-1)![1].body),
    ).not.toHaveProperty("confirm_activity_delete");
    mocks.request.mockResolvedValue({ data: create });
    await decideAiWorkspaceAction(
      parseAiActionProposal(create),
      "confirm",
      undefined,
      undefined,
      undefined,
      undefined,
      true,
    );
    expect(
      JSON.parse(mocks.request.mock.calls.at(-1)![1].body),
    ).not.toHaveProperty("confirm_activity_delete");
  });
  it("validates compound followup previews and human-only completion consent", async () => {
    const original = {
      client_id: valid.id,
      client_name: "客户",
      client_status: "active",
      assigned_actor_id: valid.generation_id,
      assigned_actor_name: "负责人",
      assigned_actor_type: "person",
      assigned_actor_status: "active",
      scheduled_at: "2026-09-17T09:00:00Z",
      timezone: "UTC",
      channel: "电话",
      purpose: "原计划",
      priority: "normal",
      status: "planned",
    };
    const nextPlan = {
      assigned_actor_id: valid.id,
      scheduled_at: "2026-10-02T01:00:00Z",
      timezone: "Asia/Shanghai",
      channel: "会议",
      purpose: "续排",
      notes: null,
      priority: "high",
    };
    const changes = {
      result: "用户确认已经沟通",
      completed_at: "2026-09-18T08:00:00Z",
      next_step: null,
      next_followup: nextPlan,
    };
    const nextPreview = {
      ...original,
      ...nextPlan,
      assigned_actor_name: "新负责人",
    };
    const complete = {
      ...valid,
      action: {
        action: "client_followup.complete",
        client_followup_id: valid.id,
        expected_version: 1,
        changes,
      },
      preview: {
        label: "原计划",
        before: original,
        after: {
          ...original,
          status: "completed",
          result: changes.result,
          completed_at: changes.completed_at,
          next_step: null,
        },
        next_followup: nextPreview,
      },
    };
    const parsed = parseAiActionProposal(complete);
    expect(parsed.preview.next_followup?.purpose).toBe("续排");
    expect(parsed.route).toBe(`/clients/${valid.id}?followup=${valid.id}`);
    expect(
      parseAiActionProposal({
        ...complete,
        status: "confirmed",
        can_confirm: false,
        result_id: valid.generation_id,
        result_version: 1,
      }).route,
    ).toBe(`/clients/${valid.id}?followup=${valid.generation_id}`);
    for (const bad of [
      {
        ...complete,
        preview: { ...complete.preview, next_followup: undefined },
      },
      {
        ...complete,
        action: {
          ...complete.action,
          changes: { ...changes, confirm_followup_completed: true },
        },
      },
      {
        ...complete,
        action: {
          ...complete.action,
          changes: { ...changes, next_followup: null },
        },
      },
      ...[
        { client_id: valid.generation_id },
        { purpose: "隐藏变化" },
        { assigned_actor_status: "inactive" },
        { status: "completed" },
        { client_name: "别的客户" },
      ].map((patch) => ({
        ...complete,
        preview: {
          ...complete.preview,
          next_followup: { ...nextPreview, ...patch },
        },
      })),
      ...[
        { result: "" },
        { completed_at: "tomorrow" },
        { scheduled_at: "2026-11-01T00:00:00Z" },
      ].map((patch) => ({
        ...complete,
        preview: {
          ...complete.preview,
          after: { ...complete.preview.after, ...patch },
        },
      })),
    ])
      expect(() => parseAiActionProposal(bad)).toThrow();
    const reschedule = {
      ...complete,
      action: {
        ...complete.action,
        action: "client_followup.reschedule",
        changes: { ...nextPlan, reason: "客户改期" },
      },
      preview: {
        ...complete.preview,
        after: { ...original, status: "cancelled", reason: "客户改期" },
      },
    };
    expect(
      parseAiActionProposal(reschedule).preview.next_followup
        ?.assigned_actor_name,
    ).toBe("新负责人");
    expect(() =>
      parseAiActionProposal({
        ...reschedule,
        preview: { ...reschedule.preview, next_followup: undefined },
      }),
    ).toThrow();
    mocks.request.mockResolvedValue({
      data: {
        ...complete,
        status: "confirmed",
        can_confirm: false,
        result_id: valid.generation_id,
        result_version: 1,
      },
    });
    await decideAiWorkspaceAction(
      parsed,
      "confirm",
      undefined,
      undefined,
      undefined,
      true,
    );
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
      fingerprint: valid.fingerprint,
      decision: "confirm",
      confirm_followup_completed: true,
    });
    mocks.request.mockResolvedValue({
      data: { ...complete, status: "rejected", can_confirm: false },
    });
    await decideAiWorkspaceAction(
      parsed,
      "reject",
      undefined,
      undefined,
      undefined,
      true,
    );
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
      fingerprint: valid.fingerprint,
      decision: "reject",
    });
  });
  it("validates followup identity, plan and payload alignment and derives only a Client route", () => {
    const fields = {
      client_id: valid.id,
      client_name: "客户",
      client_status: "active",
      assigned_actor_id: valid.generation_id,
      assigned_actor_name: "负责人",
      assigned_actor_type: "person",
      assigned_actor_status: "active",
      scheduled_at: "2026-10-01T09:00:00Z",
      timezone: "Asia/Shanghai",
      channel: "电话",
      purpose: "确认交付",
      notes: null,
      priority: "normal",
      status: "planned",
    };
    const changes = {
      client_id: fields.client_id,
      assigned_actor_id: fields.assigned_actor_id,
      scheduled_at: fields.scheduled_at,
      timezone: fields.timezone,
      channel: fields.channel,
      purpose: fields.purpose,
      notes: fields.notes,
      priority: fields.priority,
    };
    const create = {
      ...valid,
      action: { action: "client_followup.create", changes },
      preview: { label: "确认交付", before: {}, after: fields },
    };
    expect(parseAiActionProposal(create).route).toBe(`/clients/${valid.id}`);
    const cancel = {
      ...create,
      action: {
        action: "client_followup.cancel",
        client_followup_id: valid.generation_id,
        expected_version: 1,
        changes: { reason: "不再需要" },
      },
      preview: {
        ...create.preview,
        before: fields,
        after: { ...fields, status: "cancelled", reason: "不再需要" },
      },
    };
    expect(parseAiActionProposal(cancel).route).toBe(
      `/clients/${valid.id}?followup=${valid.generation_id}`,
    );
    for (const broken of [
      { ...create, action: { ...create.action, task_id: valid.id } },
      { ...create, action: { ...create.action, expected_version: 1 } },
      {
        ...create,
        action: {
          ...create.action,
          changes: { ...changes, result: "已经沟通" },
        },
      },
      {
        ...create,
        action: {
          ...create.action,
          changes: { ...changes, scheduled_at: "2026-11-01T09:00:00Z" },
        },
      },
      ...[
        { client_id: "https://example.com" },
        { assigned_actor_type: "agent" },
        { assigned_actor_name: null },
        { client_status: "inactive" },
        { priority: "P1" },
        { scheduled_at: "tomorrow" },
        { status: "completed" },
      ].map((patch) => ({
        ...create,
        preview: { ...create.preview, after: { ...fields, ...patch } },
      })),
      {
        ...cancel,
        preview: {
          ...cancel.preview,
          before: { ...fields, client_id: valid.generation_id },
        },
      },
      { ...cancel, action: { ...cancel.action, changes: {} } },
    ])
      expect(() => parseAiActionProposal(broken)).toThrow();
    const update = {
      ...cancel,
      action: {
        ...cancel.action,
        action: "client_followup.update",
        changes: { notes: "修改备注" },
      },
      preview: {
        label: "确认交付",
        before: fields,
        after: { ...fields, notes: "修改备注" },
      },
    };
    expect(parseAiActionProposal(update).action.changes.notes).toBe("修改备注");
  });
  it("validates Focus start task/duration consent data and rejects ambiguous targets", () => {
    const start = {
      ...valid,
      action: {
        action: "focus.start",
        changes: {
          task_id: valid.id,
          expected_task_version: 2,
          planned_seconds: 300,
        },
      },
      preview: {
        label: "专注任务",
        before: { status: "idle" },
        after: {
          status: "active",
          task_id: valid.id,
          task_title: "专注任务",
          task_version: 2,
          planned_seconds: 300,
        },
      },
    };
    expect(parseAiActionProposal(start).route).toBe("");
    const unbound = {
      ...start,
      action: {
        action: "focus.start",
        changes: { task_id: null, planned_seconds: 300 },
      },
      preview: {
        ...start.preview,
        after: {
          ...start.preview.after,
          task_id: null,
          task_title: null,
          task_version: null,
        },
      },
    };
    expect(parseAiActionProposal(unbound).action.changes.task_id).toBeNull();
    for (const changes of [
      { task_id: valid.id, planned_seconds: 300 },
      { task_id: valid.id, expected_task_version: 1, planned_seconds: 300 },
      { task_id: valid.id, expected_task_version: 2, planned_seconds: 600 },
      { ...start.action.changes, confirm_focus_start: true },
    ]) {
      expect(() =>
        parseAiActionProposal({
          ...start,
          action: { ...start.action, changes },
        }),
      ).toThrow();
    }
    for (const target of [
      { focus_session_id: valid.id },
      { expected_version: 1 },
      { task_id: null },
    ])
      expect(() =>
        parseAiActionProposal({
          ...start,
          action: { ...start.action, ...target },
        }),
      ).toThrow();
    expect(() =>
      parseAiActionProposal({
        ...unbound,
        action: { ...unbound.action, changes: { planned_seconds: 300 } },
      }),
    ).toThrow();
    expect(() =>
      parseAiActionProposal({
        ...unbound,
        action: {
          ...unbound.action,
          changes: { ...unbound.action.changes, expected_task_version: 2 },
        },
      }),
    ).toThrow();
  });
  it.each(["include_gap_resume", "exclude_gap_resume", "interrupt"])(
    "validates recovery %s and sends gap consent only for the matching human confirmation",
    async (recovery_action) => {
      const focus = {
        ...valid,
        action: {
          action: "focus.recover",
          focus_session_id: valid.id,
          expected_version: 2,
          changes: { recovery_action },
        },
        preview: {
          label: "中断",
          before: { status: "recovery_pending" },
          after: {
            status: recovery_action === "interrupt" ? "interrupted" : "active",
            task_id: null,
            task_title: null,
            planned_seconds: 300,
            recovery_action,
            last_heartbeat_at: "2026-09-18T12:00:00Z",
            accumulated_seconds: 20,
          },
        },
      };
      const parsed = parseAiActionProposal(focus);
      for (const patch of [
        { last_heartbeat_at: undefined },
        { last_heartbeat_at: "bad" },
        { accumulated_seconds: -1 },
        { recovery_action: "guess" },
        { status: "completed" },
      ])
        expect(() =>
          parseAiActionProposal({
            ...focus,
            preview: {
              ...focus.preview,
              after: { ...focus.preview.after, ...patch },
            },
          }),
        ).toThrow();
      mocks.request.mockResolvedValue({
        data: {
          ...focus,
          status: "confirmed",
          can_confirm: false,
          result_id: valid.id,
          result_version: 3,
        },
      });
      await decideAiWorkspaceAction(parsed, "confirm", undefined, undefined, {
        start: true,
        includeGap: true,
      });
      expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
        fingerprint: valid.fingerprint,
        decision: "confirm",
        ...(recovery_action === "include_gap_resume"
          ? { confirm_focus_gap: true }
          : {}),
      });
      mocks.request.mockResolvedValue({
        data: { ...focus, status: "rejected", can_confirm: false },
      });
      await decideAiWorkspaceAction(parsed, "reject", undefined, undefined, {
        start: true,
        includeGap: true,
      });
      expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
        fingerprint: valid.fingerprint,
        decision: "reject",
      });
    },
  );
  it("sends new-cycle consent only for confirming Focus start", async () => {
    const start = {
      ...valid,
      action: {
        action: "focus.start",
        changes: { task_id: null, planned_seconds: 300 },
      },
      preview: {
        label: "未绑定",
        before: { status: "idle" },
        after: {
          status: "active",
          task_id: null,
          task_title: null,
          task_version: null,
          planned_seconds: 300,
        },
      },
    };
    mocks.request.mockResolvedValue({
      data: {
        ...start,
        status: "confirmed",
        can_confirm: false,
        result_id: valid.id,
        result_version: 1,
      },
    });
    await decideAiWorkspaceAction(
      parseAiActionProposal(start),
      "confirm",
      undefined,
      undefined,
      { start: true, includeGap: true },
    );
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
      fingerprint: valid.fingerprint,
      decision: "confirm",
      confirm_focus_start: true,
    });
  });
  it.each(["stop", "cancel"])(
    "requires valid Focus %s terminal preview without model consent",
    (command) => {
      const terminal = {
        ...valid,
        action: {
          action: `focus.${command}`,
          focus_session_id: valid.id,
          expected_version: 1,
          changes: {},
        },
        preview: {
          label: "未绑定",
          before: { status: "paused" },
          after: {
            status: command === "stop" ? "completed" : "cancelled",
            task_id: null,
            task_title: null,
            planned_seconds: 300,
          },
        },
      };
      expect(parseAiActionProposal(terminal).route).toBe("/focus");
      expect(() =>
        parseAiActionProposal({
          ...terminal,
          preview: {
            ...terminal.preview,
            before: { status: "recovery_pending" },
          },
        }),
      ).toThrow();
      expect(() =>
        parseAiActionProposal({
          ...terminal,
          action: {
            ...terminal.action,
            changes: { confirm_focus_start: true },
          },
        }),
      ).toThrow();
    },
  );
  it("requires a real Focus target and complete pause/resume transition, using only the Focus page route", () => {
    const focus = {
      ...valid,
      action: {
        action: "focus.pause",
        focus_session_id: valid.id,
        expected_version: 1,
        changes: {},
      },
      preview: {
        label: "未绑定任务的专注",
        before: { status: "active" },
        after: {
          status: "paused",
          task_id: null,
          task_title: null,
          planned_seconds: 300,
        },
      },
    };
    expect(parseAiActionProposal(focus).route).toBe("/focus");
    for (const patch of [
      { status: "completed" },
      { planned_seconds: -1 },
      { task_id: valid.id },
      { task_title: undefined },
    ]) {
      expect(() =>
        parseAiActionProposal({
          ...focus,
          preview: {
            ...focus.preview,
            after: { ...focus.preview.after, ...patch },
          },
        }),
      ).toThrow();
    }
    for (const patch of [
      { focus_session_id: undefined },
      { task_id: valid.id },
      { changes: { confirm: true } },
    ]) {
      expect(() =>
        parseAiActionProposal({
          ...focus,
          action: { ...focus.action, ...patch },
        }),
      ).toThrow();
    }
    const resume = {
      ...focus,
      action: { ...focus.action, action: "focus.resume" },
      preview: {
        ...focus.preview,
        before: { status: "paused" },
        after: { ...focus.preview.after, status: "active" },
      },
    };
    expect(parseAiActionProposal(resume).route).toBe("/focus");
    expect(() =>
      parseAiActionProposal({
        ...valid,
        action: { ...valid.action, focus_session_id: valid.id },
      }),
    ).toThrow();
  });
  it("requires a complete force-resolve preview and sends consent only on that human decision", async () => {
    const before = {
      status: "tracking",
      resolution_policy: "all_required_tasks_done",
      active_task_count: 3,
      required_task_count: 2,
      required_done_count: 0,
      required_remaining_count: 2,
      required_blocked_count: 1,
      required_waiting_review_count: 1,
      required_cancelled_count: 0,
    };
    const after = {
      ...before,
      status: "resolved",
      resolution_mode: "forced",
      reason: "不再交付",
    };
    const value = {
      ...valid,
      action: {
        action: "inbox.force_resolve",
        inbox_item_id: valid.id,
        expected_version: 2,
        changes: { reason: " 不再交付 " },
      },
      preview: { label: "例外事项", before, after },
    };
    const parsed = parseAiActionProposal(value);
    expect(parsed.route).toBe(`/inbox/${valid.id}`);
    for (const patch of [
      { required_remaining_count: -1 },
      { required_done_count: 1 },
      { required_waiting_review_count: undefined },
      { resolution_mode: "automatic" },
      { reason: "其他原因" },
    ]) {
      expect(() =>
        parseAiActionProposal({
          ...value,
          preview: { ...value.preview, after: { ...after, ...patch } },
        }),
      ).toThrow();
    }
    expect(() =>
      parseAiActionProposal({
        ...value,
        action: {
          ...value.action,
          changes: { ...value.action.changes, confirm_force_resolve: true },
        },
      }),
    ).toThrow();
    mocks.request.mockResolvedValue({ data: value });
    await decideAiWorkspaceAction(parsed, "confirm", undefined, true);
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
      fingerprint: valid.fingerprint,
      decision: "confirm",
      confirm_force_resolve: true,
    });
    await decideAiWorkspaceAction(parsed, "reject", undefined, true);
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
      fingerprint: valid.fingerprint,
      decision: "reject",
    });
    mocks.request.mockResolvedValue({ data: valid });
    await decideAiWorkspaceAction(
      parseAiActionProposal(valid),
      "confirm",
      undefined,
      true,
    );
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
      fingerprint: valid.fingerprint,
      decision: "confirm",
    });
  });

  it("requires a complete Reminder schedule and constructs a safe detail route", () => {
    const after = {
      title: "月末复盘",
      summary: "",
      priority: "P2",
      trigger_at: "2026-10-31T01:00:00Z",
      status: "scheduled",
      recurrence_type: "monthly",
      recurrence_interval: 1,
      recurrence_timezone: "Asia/Shanghai",
      recurrence_anchor_day: 31,
      occurrence_number: 1,
    };
    const value = {
      ...valid,
      action: {
        action: "reminder.create",
        changes: { title: after.title, trigger_at: after.trigger_at },
      },
      preview: { label: after.title, before: {}, after },
    };
    expect(parseAiActionProposal(value).route).toBe("");
    expect(
      parseAiActionProposal({
        ...value,
        status: "confirmed",
        can_confirm: false,
        result_id: valid.id,
        result_version: 1,
      }).route,
    ).toBe(`/inbox?reminder=${valid.id}`);
    for (const patch of [
      { trigger_at: "tomorrow" },
      { recurrence_timezone: "" },
      { recurrence_interval: 0 },
      { recurrence_type: "none" },
      { recurrence_anchor_day: 32 },
    ]) {
      expect(() =>
        parseAiActionProposal({
          ...value,
          preview: { ...value.preview, after: { ...after, ...patch } },
        }),
      ).toThrow();
    }
    expect(() =>
      parseAiActionProposal({
        ...value,
        action: { ...value.action, task_id: valid.id },
      }),
    ).toThrow();
    expect(() =>
      parseAiActionProposal({
        ...valid,
        action: { ...valid.action, reminder_id: valid.id },
      }),
    ).toThrow();
    const { summary: _summary, ...scheduleOnly } = after;
    const cancellation = {
      ...value,
      action: {
        action: "reminder.cancel",
        reminder_id: valid.id,
        expected_version: 1,
        changes: { reason: "不再需要" },
      },
      preview: {
        label: after.title,
        before: scheduleOnly,
        after: { ...scheduleOnly, status: "cancelled", reason: "不再需要" },
      },
    };
    expect(parseAiActionProposal(cancellation).route).toBe(
      `/inbox?reminder=${valid.id}`,
    );
    expect(() =>
      parseAiActionProposal({
        ...value,
        preview: { ...value.preview, after: scheduleOnly },
      }),
    ).toThrow();
    expect(() =>
      parseAiActionProposal({
        ...cancellation,
        action: {
          ...cancellation.action,
          action: "reminder.update",
          changes: { summary: "新说明" },
        },
        preview: { ...cancellation.preview, after: scheduleOnly },
      }),
    ).toThrow();
  });

  it("requires every split draft and complete identities before offering approval", () => {
    const owner = {
      id: "00000000-0000-5000-8000-000000000001",
      label: "本人",
      type: "owner",
      version: 1,
    };
    const raw = {
      key: "first",
      title: "批量创建",
      assignee_actor_id: owner.id,
      is_required: true,
    };
    const draft = {
      key: raw.key,
      parent_key: "",
      is_required: true,
      assignee: owner,
      reviewer: null,
      project: null,
      tags: [],
      fields: {
        title: raw.title,
        description: "",
        priority: "P2",
        kind: "work",
        planned_date: null,
        due_date: null,
        estimated_minutes: null,
        project_id: null,
        completion_criteria: "",
        review_policy: "none",
        status: "todo",
      },
    };
    const split = {
      ...valid,
      action: {
        action: "inbox.split",
        inbox_item_id: valid.id,
        expected_version: 1,
        changes: { tasks: [raw] },
      },
      preview: {
        label: "批量拆分",
        before: {},
        after: {
          status: "tracking",
          resolution_policy: "all_required_tasks_done",
          active_task_count: 1,
          required_task_count: 1,
          required_done_count: 0,
          created_task_count: 1,
        },
        tasks: [draft],
      },
    };
    expect(parseAiActionProposal(split).preview.tasks).toEqual([draft]);
    expect(parseAiActionProposal(split).route).toBe("/inbox/" + valid.id);
    const confirmedSplit = {
      ...split,
      status: "confirmed",
      can_confirm: false,
      result_id: valid.id,
      result_version: 2,
      decided_at: valid.created_at,
      created_task_ids: [valid.generation_id],
    };
    expect(parseAiActionProposal(confirmedSplit).created_task_ids).toEqual([
      valid.generation_id,
    ]);
    for (const ids of [
      [],
      ["../../file"],
      [valid.generation_id, valid.generation_id],
    ]) {
      expect(() =>
        parseAiActionProposal({ ...confirmedSplit, created_task_ids: ids }),
      ).toThrow();
    }
    expect(() =>
      parseAiActionProposal({
        ...split,
        created_task_ids: [valid.generation_id],
      }),
    ).toThrow();
    for (const tasks of [
      undefined,
      [],
      [null],
      [{ ...draft, parent_key: "unknown" }],
      [{ ...draft, assignee: { ...owner, id: "../../file" } }],
      [{ ...draft, is_required: false }],
      [{ ...draft, reviewer: owner }],
      [
        {
          ...draft,
          fields: { ...draft.fields, completion_criteria: undefined },
        },
      ],
      [{ ...draft, fields: { ...draft.fields, estimated_minutes: "50" } }],
      [{ ...draft, tags: [{ ...owner, type: "tag" }] }],
    ])
      expect(() =>
        parseAiActionProposal({
          ...split,
          preview: { ...split.preview, tasks },
        }),
      ).toThrow();
    expect(() =>
      parseAiActionProposal({
        ...split,
        preview: {
          ...split.preview,
          after: { ...split.preview.after, created_task_count: 2 },
        },
      }),
    ).toThrow();
  });
  it("requires complete relation effects and binds the preview to the selected Task", () => {
    const relation = {
      ...valid,
      action: {
        action: "inbox.set_required",
        inbox_item_id: valid.id,
        expected_version: 2,
        changes: {
          task_id: valid.generation_id,
          expected_task_version: 3,
          is_required: false,
        },
      },
      preview: {
        label: "事项",
        before: { is_required: true, status: "tracking" },
        after: {
          task_id: valid.generation_id,
          task_title: "关联工作",
          task_version: 3,
          task_status: "todo",
          is_required: false,
          relation_state: "linked",
          status: "resolved",
          resolution_policy: "all_required_tasks_done",
          active_task_count: 2,
          required_task_count: 1,
          required_done_count: 1,
          required_remaining_count: 0,
        },
      },
    };
    expect(parseAiActionProposal(relation).route).toBe("/inbox/" + valid.id);
    for (const patch of [
      { task_id: "../../escape" },
      { task_version: 99 },
      { is_required: "false" },
      { required_remaining_count: undefined },
      { required_task_count: -1 },
      { status: true },
    ])
      expect(() =>
        parseAiActionProposal({
          ...relation,
          preview: {
            ...relation.preview,
            after: { ...relation.preview.after, ...patch },
          },
        }),
      ).toThrow();
  });
  it("supports Inbox commands with a single typed target and safe local route", () => {
    const inbox = {
      ...valid,
      action: {
        action: "inbox.snooze",
        inbox_item_id: valid.id,
        expected_version: 3,
        changes: { snoozed_until: "2026-09-19T12:00:00Z" },
      },
      preview: {
        label: "事项",
        before: { snoozed_until: null },
        after: { snoozed_until: "2026-09-19T12:00:00Z" },
      },
    };
    expect(parseAiActionProposal(inbox).route).toBe("/inbox/" + valid.id);
    for (const action of [
      { ...inbox.action, inbox_item_id: "../../escape" },
      { ...inbox.action, task_id: valid.id },
      { ...inbox.action, project_id: valid.id },
      { ...inbox.action, expected_version: 0 },
      { ...inbox.action, action: "inbox.force-resolve" },
    ])
      expect(() => parseAiActionProposal({ ...inbox, action })).toThrow();
    expect(
      parseAiActionProposal({
        ...valid,
        action: { action: "inbox.create", changes: { title: "新事项" } },
        status: "confirmed",
        can_confirm: false,
        result_id: valid.id,
        result_version: 1,
      }).route,
    ).toBe("/inbox/" + valid.id);
    expect(() =>
      parseAiActionProposal({
        ...valid,
        action: { ...valid.action, inbox_item_id: valid.id },
      }),
    ).toThrow();
  });
  it("strictly binds Inbox read-all to one targetless server snapshot and a fixed route", () => {
    const parsed = parseAiActionProposal(readAll);

    expect(parsed.route).toBe("/inbox");
    expect(parsed.action).not.toHaveProperty("inbox_item_id");
    expect(parsed.action).not.toHaveProperty("expected_version");
    expect(parsed.preview.inbox_read_all).toEqual({
      through_created_at: readAllCutoff,
      candidate_count: 3,
      selection_fingerprint: "b".repeat(64),
    });

    for (const mutate of [
      (value: any) => {
        value.action.inbox_item_id = valid.id;
      },
      (value: any) => {
        value.action.expected_version = 1;
      },
      (value: any) => {
        value.action.task_id = valid.id;
      },
      (value: any) => {
        value.action.changes.reason = "also resolve everything";
      },
      (value: any) => {
        value.action.changes.through_created_at = "2026-09-18T12:00:00+00:00";
      },
      (value: any) => {
        value.preview.inbox_read_all.through_created_at =
          "2026-09-18T11:59:58Z";
      },
      (value: any) => {
        value.preview.inbox_read_all.candidate_count = 2;
      },
      (value: any) => {
        value.preview.inbox_read_all.selection_fingerprint = "B".repeat(64);
      },
      (value: any) => {
        value.preview.before.snapshot_unread_count = 2;
      },
      (value: any) => {
        value.preview.after.marked_count = 1_001;
        value.preview.before.snapshot_unread_count = 1_001;
        value.preview.inbox_read_all.candidate_count = 1_001;
      },
      (value: any) => {
        value.preview.after.through_created_at = "2026-09-18T11:59:58Z";
      },
      (value: any) => {
        value.preview.inbox_read_all.candidate_ids = [valid.id];
      },
      (value: any) => {
        value.action.redirect = "https://example.com/steal";
      },
      (value: any) => {
        value.preview.tasks = [];
      },
      (value: any) => {
        value.invoice_pdf_result = { download_url: "file:///private" };
      },
      (value: any) => {
        value.inbox_read_all_result = {
          through_created_at: readAllCutoff,
          marked_count: 3,
        };
      },
    ]) {
      const changed = structuredClone(readAll);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }
  });
  it("accepts only a confirmed Inbox read-all result aligned to the approved snapshot", async () => {
    const confirmed = {
      ...readAll,
      status: "confirmed",
      can_confirm: false,
      result_id: readAll.id,
      result_version: 1,
      decided_at: "2026-09-18T12:00:02Z",
      inbox_read_all_result: {
        through_created_at: readAllCutoff,
        marked_count: 3,
      },
    };

    expect(parseAiActionProposal(confirmed)).toMatchObject({
      route: "/inbox",
      result_id: readAll.id,
      result_version: 1,
      inbox_read_all_result: {
        through_created_at: readAllCutoff,
        marked_count: 3,
      },
    });

    for (const mutate of [
      (value: any) => {
        value.result_id = valid.generation_id;
      },
      (value: any) => {
        value.result_version = 2;
      },
      (value: any) => {
        value.inbox_read_all_result.through_created_at = "2026-09-18T11:59:58Z";
      },
      (value: any) => {
        value.inbox_read_all_result.marked_count = 2;
      },
      (value: any) => {
        value.inbox_read_all_result.receipt_url = "https://example.com";
      },
      (value: any) => {
        delete value.inbox_read_all_result;
      },
    ]) {
      const changed = structuredClone(confirmed);
      mutate(changed);
      expect(() => parseAiActionProposal(changed)).toThrow();
    }

    mocks.request.mockResolvedValueOnce({ data: confirmed });
    await expect(
      decideAiWorkspaceAction(
        parseAiActionProposal(readAll),
        "confirm",
        true,
        true,
      ),
    ).resolves.toMatchObject({ status: "confirmed", route: "/inbox" });
    expect(mocks.request).toHaveBeenLastCalledWith(
      `/api/v1/ai/actions/${readAll.id}/decision`,
      {
        method: "POST",
        body: JSON.stringify({
          fingerprint: readAll.fingerprint,
          decision: "confirm",
        }),
      },
    );
  });
  it("strictly validates roadmap milestone previews and derives local routes", () => {
    const milestoneId = "00000000-0000-4000-8000-000000000090";
    const projectId = "00000000-0000-4000-8000-000000000091";
    const update = {
      ...valid,
      action: {
        action: "roadmap_milestone.update",
        roadmap_milestone_id: milestoneId,
        expected_version: 3,
        changes: {
          title: "交付里程碑",
          status: "achieved",
          project_ids: [projectId],
        },
      },
      preview: {
        label: "原里程碑",
        before: {
          title: "原里程碑",
          status: "active",
          project_ids: [],
        },
        after: {
          title: "交付里程碑",
          status: "achieved",
          project_ids: [projectId],
        },
      },
    };
    expect(parseAiActionProposal(update).route).toBe(
      `/roadmap?milestone=${milestoneId}`,
    );
    const created = {
      ...valid,
      action: {
        action: "roadmap_milestone.create",
        changes: {
          title: "新里程碑",
          year: 2026,
          quarter: 4,
          target_date: "2026-10-10",
        },
      },
      preview: {
        label: "新里程碑",
        before: {},
        after: {
          title: "新里程碑",
          description: null,
          year: 2026,
          quarter: 4,
          target_date: "2026-10-10",
          status: "planned",
          project_ids: [],
        },
      },
    };
    expect(parseAiActionProposal(created).route).toBe("");
    for (const mutate of [
      (value: any) => {
        value.action.project_id = projectId;
      },
      (value: any) => {
        value.action.changes.status = "archived";
      },
      (value: any) => {
        value.preview.after.project_ids = ["not-a-uuid"];
      },
      (value: any) => {
        delete value.preview.before.title;
      },
      (value: any) => {
        value.preview.after.manual_order = 1024;
      },
    ]) {
      const malformed = structuredClone(update);
      mutate(malformed);
      expect(() => parseAiActionProposal(malformed)).toThrow();
    }
    const archived = {
      ...update,
      action: {
        action: "roadmap_milestone.archive",
        roadmap_milestone_id: milestoneId,
        expected_version: 3,
        changes: {},
      },
      preview: {
        label: "原里程碑",
        before: { status: "active", project_ids: [projectId] },
        after: { status: "archived", project_ids: [projectId] },
      },
    };
    expect(parseAiActionProposal(archived).route).toBe(
      `/roadmap?milestone=${milestoneId}`,
    );
  });
  it("strictly validates content item previews and derives content-calendar routes", () => {
    const contentId = "00000000-0000-4000-8000-000000000092";
    const projectId = "00000000-0000-4000-8000-000000000093";
    const update = {
      ...valid,
      action: {
        action: "content_item.update",
        content_item_id: contentId,
        expected_version: 3,
        changes: {
          title: "发布稿",
          project_id: projectId,
          notes: null,
        },
      },
      preview: {
        label: "原内容",
        before: {
          title: "原内容",
          project_id: null,
          notes: "草稿备注",
        },
        after: {
          title: "发布稿",
          project_id: projectId,
          notes: null,
        },
      },
    };
    expect(parseAiActionProposal(update).route).toBe(
      `/content-calendar?item=${contentId}`,
    );
    const created = {
      ...valid,
      action: {
        action: "content_item.create",
        changes: {
          title: "新内容",
          platform: "website",
          status: "scheduled",
          scheduled_at: "2026-09-20T17:00:00Z",
          scheduled_timezone: "America/Tijuana",
        },
      },
      preview: {
        label: "新内容",
        before: {},
        after: {
          title: "新内容",
          platform: "website",
          status: "scheduled",
          scheduled_at: "2026-09-20T17:00:00Z",
          scheduled_timezone: "America/Tijuana",
          project_id: null,
          notes: null,
          external_link: null,
        },
      },
    };
    expect(parseAiActionProposal(created).route).toBe("");
    expect(
      parseAiActionProposal({
        ...created,
        status: "confirmed",
        can_confirm: false,
        result_id: contentId,
        result_version: 1,
      }).route,
    ).toBe(`/content-calendar?item=${contentId}`);
    const scheduled = {
      ...update,
      action: {
        action: "content_item.schedule",
        content_item_id: contentId,
        expected_version: 3,
        changes: {
          scheduled_at: "2026-09-20T17:00:00Z",
          scheduled_timezone: "America/Tijuana",
        },
      },
      preview: {
        label: "原内容",
        before: {
          scheduled_at: null,
          scheduled_timezone: null,
          status: "draft",
        },
        after: {
          scheduled_at: "2026-09-20T17:00:00Z",
          scheduled_timezone: "America/Tijuana",
          status: "scheduled",
        },
      },
    };
    expect(parseAiActionProposal(scheduled).route).toBe(
      `/content-calendar?item=${contentId}`,
    );
    const taskId = "00000000-0000-4000-8000-000000000094";
    const relation = {
      ...valid,
      action: {
        action: "content_item.set_task_required",
        content_item_id: contentId,
        expected_version: 3,
        changes: {
          task_id: taskId,
          expected_task_version: 2,
          is_required: false,
        },
      },
      preview: {
        label: "原内容",
        before: {
          relation_state: "linked",
          is_required: true,
          content_item_version: 3,
        },
        after: {
          task_id: taskId,
          task_title: "准备发布素材",
          task_version: 2,
          task_status: "in_progress",
          relation_state: "linked",
          is_required: false,
          content_item_version: 4,
        },
      },
    };
    expect(parseAiActionProposal(relation).route).toBe(
      `/content-calendar?item=${contentId}`,
    );
    for (const mutate of [
      (value: any) => {
        value.action.changes.expected_task_version = 3;
      },
      (value: any) => {
        value.preview.after.task_id = "00000000-0000-4000-8000-000000000095";
      },
      (value: any) => {
        value.preview.after.content_item_version = 3;
      },
      (value: any) => {
        value.preview.before.relation_state = "unlinked";
      },
      (value: any) => {
        value.action.changes.reason = "unexpected";
      },
    ]) {
      const malformed = structuredClone(relation);
      mutate(malformed);
      expect(() => parseAiActionProposal(malformed)).toThrow();
    }
    for (const mutate of [
      (value: any) => {
        value.action.project_id = projectId;
      },
      (value: any) => {
        value.action.action = "content_item.publish";
      },
      (value: any) => {
        value.action.changes.status = "published";
      },
      (value: any) => {
        delete value.preview.before.title;
      },
      (value: any) => {
        value.preview.after.manual_order = 1024;
      },
    ]) {
      const malformed = structuredClone(update);
      mutate(malformed);
      expect(() => parseAiActionProposal(malformed)).toThrow();
    }
  });
  it("strictly validates client profile previews and derives client routes", () => {
    const clientId = "00000000-0000-4000-8000-000000000094";
    const created = {
      ...valid,
      action: {
        action: "client.create",
        changes: {
          name: "新客户",
          contact_name: "Ada",
          email: "ada@example.com",
          status: "lead",
        },
      },
      preview: {
        label: "新客户",
        before: {},
        after: {
          name: "新客户",
          contact_name: "Ada",
          email: "ada@example.com",
          phone: null,
          notes: null,
          status: "lead",
        },
      },
    };
    expect(parseAiActionProposal(created).route).toBe("");
    expect(
      parseAiActionProposal({
        ...created,
        status: "confirmed",
        can_confirm: false,
        result_id: clientId,
        result_version: 1,
      }).route,
    ).toBe(`/clients/${clientId}`);

    const updated = {
      ...valid,
      action: {
        action: "client.update",
        client_id: clientId,
        expected_version: 2,
        changes: { name: "客户新名称", email: null, status: "inactive" },
      },
      preview: {
        label: "原客户",
        before: {
          name: "原客户",
          email: "old@example.com",
          status: "lead",
        },
        after: { name: "客户新名称", email: null, status: "inactive" },
      },
    };
    expect(parseAiActionProposal(updated).route).toBe(`/clients/${clientId}`);
    for (const mutate of [
      (value: any) => {
        value.action.task_id = clientId;
      },
      (value: any) => {
        value.action.action = "client.delete";
      },
      (value: any) => {
        value.action.changes.email = "not-an-email";
        value.preview.after.email = "not-an-email";
      },
      (value: any) => {
        delete value.preview.before.name;
      },
      (value: any) => {
        value.preview.after.notes = "hidden extra field";
      },
    ]) {
      const malformed = structuredClone(updated);
      mutate(malformed);
      expect(() => parseAiActionProposal(malformed)).toThrow();
    }
  });
  it("strictly binds Client contact link and unlink previews", () => {
    const clientId = "00000000-0000-4000-8000-000000000094";
    const actorId = "00000000-0000-4000-8000-000000000095";
    const linkId = "00000000-0000-4000-8000-000000000096";
    const active = {
      client_id: clientId,
      client_name: "示例客户",
      client_status: "active",
      actor_id: actorId,
      actor_name: "本地联系人",
      actor_type: "person",
      actor_status: "active",
      actor_version: 2,
      role: "contact",
      link_state: "active",
    };
    const linked = {
      ...valid,
      action: {
        action: "client_contact.link",
        client_id: clientId,
        expected_version: 3,
        changes: { actor_id: actorId },
      },
      preview: {
        label: "示例客户",
        before: { link_state: "none" },
        after: active,
      },
      route: `/clients/${clientId}`,
    };
    expect(parseAiActionProposal(linked).route).toBe(`/clients/${clientId}`);
    const unlinked = {
      ...linked,
      action: {
        action: "client_contact.unlink",
        client_id: clientId,
        client_actor_link_id: linkId,
        expected_version: 4,
        changes: { reason: "联系人职责已变更" },
      },
      preview: {
        label: "示例客户",
        before: active,
        after: {
          ...active,
          link_state: "unlinked",
          reason: "联系人职责已变更",
        },
      },
    };
    expect(parseAiActionProposal(unlinked).route).toBe(`/clients/${clientId}`);
    for (const mutate of [
      (value: any) => {
        value.action.task_id = clientId;
      },
      (value: any) => {
        value.action.changes.actor_id = "00000000-0000-5000-8000-000000000001";
      },
      (value: any) => {
        value.preview.after.actor_type = "agent";
      },
      (value: any) => {
        value.preview.after.reason = "未披露变化";
      },
    ]) {
      const malformed = structuredClone(linked);
      mutate(malformed);
      expect(() => parseAiActionProposal(malformed)).toThrow();
    }
    for (const mutate of [
      (value: any) => {
        delete value.action.client_actor_link_id;
      },
      (value: any) => {
        value.preview.before.actor_version = 3;
      },
      (value: any) => {
        value.preview.after.reason = "不一致";
      },
    ]) {
      const malformed = structuredClone(unlinked);
      mutate(malformed);
      expect(() => parseAiActionProposal(malformed)).toThrow();
    }
  });
  it("strictly validates local person previews without inventing a route", () => {
    const actorId = "00000000-0000-4000-8000-000000000097";
    const created = {
      ...valid,
      action: {
        action: "person.create",
        changes: { display_name: "Ada", notes: "由用户明确提供" },
      },
      preview: {
        label: "Ada",
        before: {},
        after: {
          display_name: "Ada",
          notes: "由用户明确提供",
          status: "active",
          person_type: "person",
        },
      },
    };
    expect(parseAiActionProposal(created).route).toBe("");
    expect(
      parseAiActionProposal({
        ...created,
        status: "confirmed",
        can_confirm: false,
        result_id: actorId,
        result_version: 1,
      }).route,
    ).toBe("");

    const updated = {
      ...valid,
      action: {
        action: "person.update",
        actor_id: actorId,
        expected_version: 2,
        changes: { display_name: "Ada Lovelace", status: "inactive" },
      },
      preview: {
        label: "Ada",
        before: { display_name: "Ada", status: "active" },
        after: { display_name: "Ada Lovelace", status: "inactive" },
      },
    };
    expect(parseAiActionProposal(updated).route).toBe("");
    for (const mutate of [
      (value: any) => {
        value.action.client_id = actorId;
      },
      (value: any) => {
        value.action.action = "person.delete";
      },
      (value: any) => {
        value.action.changes.metadata = { token: "secret" };
      },
      (value: any) => {
        value.preview.after.notes = "未披露字段";
      },
      (value: any) => {
        value.action.changes.status = "owner";
        value.preview.after.status = "owner";
      },
    ]) {
      const malformed = structuredClone(updated);
      mutate(malformed);
      expect(() => parseAiActionProposal(malformed)).toThrow();
    }
  });
  const project = {
    ...valid,
    action: {
      action: "project.complete",
      project_id: valid.id,
      expected_version: 2,
      changes: {},
    },
    preview: {
      label: "项目",
      before: { status: "in_progress" },
      after: { status: "completed", incomplete_task_count: 1 },
    },
  };
  it("derives project routes and requires complete warning data", () => {
    expect(parseAiActionProposal(project).route).toBe("/projects/" + valid.id);
    for (const patch of [
      { action: { ...project.action, task_id: valid.id } },
      { action: { ...project.action, project_id: "../../escape" } },
      { action: { ...project.action, expected_version: 0 } },
      { preview: { ...project.preview, after: { status: "completed" } } },
      { preview: { ...project.preview, after: { incomplete_task_count: -1 } } },
    ])
      expect(() => parseAiActionProposal({ ...project, ...patch })).toThrow();
  });
  it("sends human incomplete-task consent only with project completion confirmation", async () => {
    mocks.request.mockResolvedValue({
      data: {
        ...project,
        status: "confirmed",
        can_confirm: false,
        result_id: valid.id,
        result_version: 3,
      },
    });
    await decideAiWorkspaceAction(
      parseAiActionProposal(project),
      "confirm",
      true,
    );
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
      fingerprint: valid.fingerprint,
      decision: "confirm",
      confirm_incomplete_tasks: true,
    });
    mocks.request.mockResolvedValue({
      data: { ...project, status: "rejected", can_confirm: false },
    });
    await decideAiWorkspaceAction(
      parseAiActionProposal(project),
      "reject",
      true,
    );
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
      fingerprint: valid.fingerprint,
      decision: "reject",
    });
  });
  it("derives local task navigation instead of trusting response URLs", () => {
    expect(parseAiActionProposal(valid).route).toBe("");
    expect(
      parseAiActionProposal({
        ...valid,
        status: "confirmed",
        can_confirm: false,
        result_id: valid.id,
        result_version: 1,
      }).route,
    ).toBe(`/tasks/${valid.id}`);
  });
  it("accepts bounded task tag previews and keeps navigation local", () => {
    const tagged = {
      ...valid,
      action: {
        action: "task.update",
        task_id: valid.id,
        expected_version: 2,
        changes: {
          tag_ids: ["00000000-0000-4000-8000-000000000010"],
        },
      },
      preview: {
        label: "新任务",
        before: { tag_names: ["旧标签"] },
        after: { tag_names: ["新标签"] },
      },
    };
    expect(parseAiActionProposal(tagged).route).toBe(`/tasks/${valid.id}`);
    expect(() =>
      parseAiActionProposal({
        ...tagged,
        preview: {
          ...tagged.preview,
          after: { tag_names: [{ name: "伪造标签" }] },
        },
      }),
    ).toThrow();
    expect(() =>
      parseAiActionProposal({
        ...tagged,
        preview: {
          ...tagged.preview,
          after: { tag_names: ["标签一", "标签二"] },
        },
      }),
    ).toThrow();
    expect(() =>
      parseAiActionProposal({
        ...valid,
        preview: { ...valid.preview, after: { tag_names: ["伪造标签"] } },
      }),
    ).toThrow();
  });
  it("accepts human-readable task parent previews and rejects mismatched shapes", () => {
    const parentId = "00000000-0000-4000-8000-000000000011";
    const reparented = {
      ...valid,
      action: {
        action: "task.update",
        task_id: valid.id,
        expected_version: 2,
        changes: { parent_task_id: parentId },
      },
      preview: {
        label: "子任务",
        before: { parent_task_title: "原父任务" },
        after: { parent_task_title: "新父任务" },
      },
    };
    expect(parseAiActionProposal(reparented).route).toBe(`/tasks/${valid.id}`);
    expect(
      parseAiActionProposal({
        ...reparented,
        action: {
          action: "task.create",
          changes: { parent_task_id: null },
        },
        preview: {
          label: "顶层任务",
          before: {},
          after: { parent_task_title: null },
        },
      }).route,
    ).toBe("");
    for (const patch of [
      {
        action: {
          ...reparented.action,
          changes: { parent_task_id: "NOT-UUID" },
        },
      },
      { preview: { ...reparented.preview, after: {} } },
      {
        action: { ...valid.action, changes: {} },
        preview: {
          ...valid.preview,
          after: { parent_task_title: "伪造父任务" },
        },
      },
    ])
      expect(() =>
        parseAiActionProposal({ ...reparented, ...patch }),
      ).toThrow();
  });
  it.each([
    { fingerprint: "wrong" },
    { action: { action: "shell.run", changes: {} } },
    { preview: { label: "fake", before: {}, after: { command: "echo" } } },
    { status: "confirmed" },
    { status: "rejected", can_confirm: true },
    { result_id: "../../escape" },
    {
      action: {
        action: "task.update",
        task_id: valid.id,
        changes: { title: "missing version" },
      },
    },
  ])("rejects malformed approval response %j", (patch) =>
    expect(() => parseAiActionProposal({ ...valid, ...patch })).toThrow(),
  );
  it("sends only the immutable fingerprint and explicit decision, not model-authored changes", async () => {
    mocks.request.mockResolvedValue({
      data: { ...valid, status: "rejected", can_confirm: false },
    });
    await decideAiWorkspaceAction(parseAiActionProposal(valid), "reject");
    expect(mocks.request).toHaveBeenLastCalledWith(
      `/api/v1/ai/actions/${valid.id}/decision`,
      {
        method: "POST",
        body: JSON.stringify({
          fingerprint: valid.fingerprint,
          decision: "reject",
        }),
      },
    );
  });
  it("checks the bounded list and response identity", async () => {
    mocks.request.mockResolvedValue({ data: Array(9).fill(valid) });
    await expect(getAiWorkspaceActions(valid.generation_id)).rejects.toThrow();
    mocks.request.mockResolvedValue({ data: [valid, valid] });
    await expect(getAiWorkspaceActions(valid.generation_id)).rejects.toThrow();
    mocks.request.mockResolvedValue({
      data: [{ ...valid, generation_id: valid.id }],
    });
    await expect(getAiWorkspaceActions(valid.generation_id)).rejects.toThrow();
    mocks.request.mockResolvedValue({
      data: { ...valid, id: valid.generation_id },
    });
    await expect(
      decideAiWorkspaceAction(parseAiActionProposal(valid), "confirm"),
    ).rejects.toThrow();
  });
});
