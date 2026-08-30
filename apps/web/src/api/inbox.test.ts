import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  createInboxItem,
  executeInboxItemCommand,
  getInboxItemEvents,
  getInboxItemTasks,
  getInboxItems,
  linkInboxItemTask,
  markAllInboxItemsRead,
  normalizeInboxItem,
  normalizeInboxItemTaskListResult,
  resetRuntimeConnection,
  unlinkInboxItemTask,
  updateInboxItem,
  updateInboxItemTaskRequirement,
} from "./client";

function inboxPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "018f0000-0000-7000-8000-000000000801",
    kind: "manual",
    title: "确认本周交付范围",
    summary: "整理需要人工确认的边界",
    source_entity_type: "manual",
    source_entity_id: null,
    source_event_key: null,
    source_deleted_at: null,
    priority: "P1",
    status: "open",
    resolution_policy: "manual",
    due_at: "2026-08-29T12:00:00Z",
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
    updated_at: "2026-08-28T10:00:00Z",
    available_actions: ["edit", "read", "snooze", "resolve", "dismiss"],
    ...overrides,
  };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function actorPayload() {
  return {
    id: "00000000-0000-0000-0000-000000000001",
    type: "owner",
    display_name: "陶定鑫",
    status: "active",
    is_builtin: true,
    version: 1,
  };
}

function taskRelationPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "018f0000-0000-7000-8000-000000000921",
    inbox_item_id: "018f0000-0000-7000-8000-000000000801",
    task_ref_id: "018f0000-0000-7000-8000-000000000101",
    task_id: "018f0000-0000-7000-8000-000000000101",
    task_title_snapshot: "整理发布清单",
    task: {
      id: "018f0000-0000-7000-8000-000000000101",
      title: "整理发布清单",
      status: "in_progress",
      priority: "P1",
      kind: "work",
      project_id: "018f0000-0000-7000-8000-000000000201",
      project_name: "官网升级",
      version: 4,
    },
    relation_type: "linked",
    is_required: true,
    position: 1,
    linked_by_actor_id: "00000000-0000-0000-0000-000000000001",
    linked_by_actor: actorPayload(),
    linked_at: "2026-08-28T10:10:00Z",
    unlinked_by_actor_id: null,
    unlinked_by_actor: null,
    unlinked_at: null,
    unlink_reason: null,
    is_active: true,
    task_deleted: false,
    ...overrides,
  };
}

function progressPayload(overrides: Record<string, unknown> = {}) {
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
    ...overrides,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("inbox API contract", () => {
  it("strictly normalizes manual item facts and executable actions", () => {
    expect(normalizeInboxItem(inboxPayload())).toMatchObject({
      id: "018f0000-0000-7000-8000-000000000801",
      kind: "manual",
      title: "确认本周交付范围",
      sourceEntityType: "manual",
      priority: "P1",
      status: "open",
      resolutionPolicy: "manual",
      version: 2,
      availableActions: ["edit", "read", "snooze", "resolve", "dismiss"],
    });
    expect(() =>
      normalizeInboxItem(inboxPayload({ kind: "notification" })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem(inboxPayload({ priority: "urgent" })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem(inboxPayload({ payload_json: [] })),
    ).toThrow(ApiError);
  });

  it("accepts only internally consistent Reminder Inbox projections", () => {
    const reminderId = "018f0000-0000-7000-8000-000000000811";
    expect(
      normalizeInboxItem(
        inboxPayload({
          kind: "reminder",
          source_entity_type: "reminder",
          source_entity_id: reminderId,
          source_event_key: `reminder:${reminderId}:due`,
          payload_json: {
            reminder_id: reminderId,
            trigger_at: "2026-08-28T10:00:00Z",
          },
        }),
      ),
    ).toMatchObject({
      kind: "reminder",
      sourceEntityType: "reminder",
      sourceEntityId: reminderId,
    });
    expect(() =>
      normalizeInboxItem(
        inboxPayload({
          kind: "reminder",
          source_entity_type: "manual",
          source_entity_id: reminderId,
        }),
      ),
    ).toThrow(ApiError);
  });

  it("strictly validates Task Artifact follow-up source snapshots", () => {
    const artifactId = "018f0000-0000-7000-8000-000000000812";
    const taskId = "018f0000-0000-7000-8000-000000000813";
    const submissionId = "018f0000-0000-7000-8000-000000000814";
    const projected = inboxPayload({
      kind: "event",
      source_entity_type: "task_artifact",
      source_entity_id: artifactId,
      source_event_key: `task-artifact:${artifactId}:followup`,
      payload_json: {
        artifact_id: artifactId,
        artifact_name: "交付清单",
        storage_kind: "text",
        task_id: taskId,
        task_title: "准备项目交付",
        submission_id: submissionId,
        submission_sequence: 1,
      },
    });
    expect(normalizeInboxItem(projected)).toMatchObject({
      kind: "event",
      sourceEntityType: "task_artifact",
      sourceEntityId: artifactId,
      payloadJson: { task_id: taskId, artifact_name: "交付清单" },
    });
    expect(() =>
      normalizeInboxItem({
        ...projected,
        payload_json: { ...projected.payload_json, artifact_id: "wrong" },
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem({
        ...projected,
        source_entity_type: "task",
      }),
    ).toThrow(ApiError);
  });

  it("strictly validates Task blocked source snapshots and event keys", () => {
    const taskId = "018f0000-0000-7000-8000-000000000815";
    const projected = inboxPayload({
      kind: "event",
      source_entity_type: "task",
      source_entity_id: taskId,
      source_event_key: `task:${taskId}:blocked:4`,
      payload_json: {
        task_id: taskId,
        task_title: "等待客户确认",
        blocked_reason: "尚未收到确认",
        blocked_at: "2026-08-28T10:00:00Z",
        blocked_from_status: "in_progress",
        block_version: 4,
      },
    });
    expect(normalizeInboxItem(projected)).toMatchObject({
      kind: "event",
      sourceEntityType: "task",
      sourceEntityId: taskId,
      payloadJson: {
        task_title: "等待客户确认",
        blocked_reason: "尚未收到确认",
        block_version: 4,
      },
    });
    expect(() =>
      normalizeInboxItem({
        ...projected,
        source_event_key: `task:${taskId}:blocked:3`,
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem({
        ...projected,
        payload_json: {
          ...projected.payload_json,
          blocked_from_status: "blocked",
        },
      }),
    ).toThrow(ApiError);
  });

  it("strictly validates Task due source snapshots and event keys", () => {
    const taskId = "018f0000-0000-7000-8000-000000000816";
    const dueAt = "2026-08-29T10:00:00Z";
    const projected = inboxPayload({
      kind: "event",
      source_entity_type: "task_due",
      source_entity_id: taskId,
      source_event_key: `task:${taskId}:due:${dueAt}`,
      due_at: dueAt,
      payload_json: {
        task_id: taskId,
        task_title: "准备项目交付",
        due_at: dueAt,
        projected_at: "2026-08-28T10:00:00Z",
        due_state: "due_soon",
        lead_minutes: 1440,
      },
    });
    expect(normalizeInboxItem(projected)).toMatchObject({
      kind: "event",
      sourceEntityType: "task_due",
      sourceEntityId: taskId,
      dueAt,
      payloadJson: {
        task_title: "准备项目交付",
        due_state: "due_soon",
        lead_minutes: 1440,
      },
    });
    expect(() =>
      normalizeInboxItem({
        ...projected,
        source_event_key: `task:${taskId}:due:2026-08-30T10:00:00Z`,
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem({
        ...projected,
        payload_json: { ...projected.payload_json, lead_minutes: 60 },
      }),
    ).toThrow(ApiError);
  });

  it("strictly validates Invoice due snapshots, stages, and event keys", () => {
    const invoiceId = "018f0000-0000-7000-8000-000000000826";
    const clientId = "018f0000-0000-7000-8000-000000000827";
    const projectId = "018f0000-0000-7000-8000-000000000828";
    const payload = {
      invoice_id: invoiceId,
      invoice_number: "INV-2026-0829",
      client_id: clientId,
      client_name: "星河设计事务所",
      project_id: projectId,
      project_name: "品牌视觉升级",
      amount_minor: 128045,
      currency: "CNY",
      due_date: "2026-09-01",
      due_state: "due_soon",
      occurrence_date: "2026-08-29",
      invoice_version: 4,
      projected_at: "2026-08-29T08:00:00.123456789Z",
      lead_days: 3,
    };
    const dueSoon = inboxPayload({
      kind: "event",
      source_entity_type: "invoice_due",
      source_entity_id: invoiceId,
      source_event_key:
        "invoice:" + invoiceId + ":due_soon:" + payload.due_date,
      source_deleted_at: null,
      due_at: "2026-08-29T09:00:00+08:00",
      payload_json: payload,
    });
    expect(normalizeInboxItem(dueSoon)).toMatchObject({
      kind: "event",
      sourceEntityType: "invoice_due",
      sourceEntityId: invoiceId,
      dueAt: "2026-08-29T09:00:00+08:00",
      payloadJson: {
        invoice_number: "INV-2026-0829",
        client_name: "星河设计事务所",
        amount_minor: 128045,
        currency: "CNY",
        due_state: "due_soon",
        lead_days: 3,
      },
    });

    const duePayload = {
      ...payload,
      due_state: "due",
      occurrence_date: payload.due_date,
    };
    expect(
      normalizeInboxItem({
        ...dueSoon,
        source_event_key: "invoice:" + invoiceId + ":due:" + payload.due_date,
        payload_json: duePayload,
      }),
    ).toMatchObject({ payloadJson: { due_state: "due" } });

    const overduePayload = {
      ...payload,
      project_id: null,
      project_name: null,
      due_state: "overdue",
      occurrence_date: "2026-09-02",
    };
    expect(
      normalizeInboxItem({
        ...dueSoon,
        source_event_key:
          "invoice:" + invoiceId + ":overdue:" + overduePayload.occurrence_date,
        source_deleted_at: "2026-09-03T10:00:00Z",
        payload_json: overduePayload,
      }),
    ).toMatchObject({
      sourceDeletedAt: "2026-09-03T10:00:00Z",
      payloadJson: {
        due_state: "overdue",
        project_id: null,
        project_name: null,
      },
    });

    const invalidPayloads = [
      { ...payload, unexpected: true },
      { ...payload, amount_minor: 0 },
      { ...payload, amount_minor: 9_000_000_000_000_001 },
      { ...payload, currency: "cny" },
      { ...payload, due_date: "2026-02-30" },
      { ...payload, occurrence_date: "2026-08-28" },
      { ...payload, invoice_version: 0 },
      { ...payload, projected_at: "2026-08-29 08:00:00Z" },
      { ...payload, lead_days: 2 },
      { ...payload, project_name: " " },
      { ...payload, project_name: null },
    ];
    for (const invalidPayload of invalidPayloads) {
      expect(() =>
        normalizeInboxItem({
          ...dueSoon,
          source_event_key:
            "invoice:" +
            invoiceId +
            ":due_soon:" +
            String(invalidPayload.due_date),
          payload_json: invalidPayload,
        }),
      ).toThrow(ApiError);
    }
    expect(() =>
      normalizeInboxItem({
        ...dueSoon,
        source_entity_id: clientId,
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem({
        ...dueSoon,
        source_event_key: "invoice:" + invoiceId + ":due_soon:2026-09-02",
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem({
        ...dueSoon,
        due_at: "2026-08-29",
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem({
        ...dueSoon,
        payload_json: { ...duePayload, occurrence_date: "2026-09-02" },
        source_event_key: "invoice:" + invoiceId + ":due:" + payload.due_date,
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem({
        ...dueSoon,
        payload_json: {
          ...overduePayload,
          occurrence_date: overduePayload.due_date,
        },
        source_event_key:
          "invoice:" + invoiceId + ":overdue:" + overduePayload.due_date,
      }),
    ).toThrow(ApiError);
  });

  it("strictly validates Client Follow-up due source snapshots and event keys", () => {
    const followupId = "018f0000-0000-7000-8000-000000000821";
    const clientId = "018f0000-0000-7000-8000-000000000822";
    const dueAt = "2026-08-30T10:00:00Z";
    const projected = inboxPayload({
      kind: "event",
      source_entity_type: "client_followup",
      source_entity_id: followupId,
      source_event_key: `followup:${followupId}:due:2`,
      due_at: dueAt,
      payload_json: {
        client_followup_id: followupId,
        client_id: clientId,
        scheduled_at: dueAt,
        timezone: "Asia/Shanghai",
        channel: "微信",
      },
    });
    expect(normalizeInboxItem(projected)).toMatchObject({
      kind: "event",
      sourceEntityType: "client_followup",
      sourceEntityId: followupId,
      dueAt,
      payloadJson: {
        client_id: clientId,
        timezone: "Asia/Shanghai",
        channel: "微信",
      },
    });
    expect(() =>
      normalizeInboxItem({
        ...projected,
        payload_json: { ...projected.payload_json, client_id: "" },
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem({
        ...projected,
        source_event_key: `followup:${followupId}:due:0`,
      }),
    ).toThrow(ApiError);
  });

  it("strictly validates Content Item due source snapshots and event keys", () => {
    const contentItemId = "018f0000-0000-7000-8000-000000000823";
    const dueAt = "2026-08-30T10:00:00Z";
    const projected = inboxPayload({
      kind: "event",
      source_entity_type: "content_item",
      source_entity_id: contentItemId,
      source_event_key: `content:${contentItemId}:publish_due:3`,
      due_at: dueAt,
      payload_json: {
        content_item_id: contentItemId,
        event_type: "publish_due",
        content_version: 3,
        scheduled_at: dueAt,
        scheduled_timezone: "Asia/Shanghai",
      },
    });
    expect(normalizeInboxItem(projected)).toMatchObject({
      kind: "event",
      sourceEntityType: "content_item",
      sourceEntityId: contentItemId,
      dueAt,
      payloadJson: {
        event_type: "publish_due",
        content_version: 3,
        scheduled_timezone: "Asia/Shanghai",
      },
    });
    expect(() =>
      normalizeInboxItem({
        ...projected,
        source_event_key: `content:${contentItemId}:review_due:3`,
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem({
        ...projected,
        payload_json: { ...projected.payload_json, content_version: 0 },
      }),
    ).toThrow(ApiError);
  });

  it("strictly validates Roadmap Milestone source snapshots and event keys", () => {
    const milestoneId = "018f0000-0000-7000-8000-000000000824";
    const projected = inboxPayload({
      kind: "event",
      source_entity_type: "roadmap_milestone",
      source_entity_id: milestoneId,
      source_event_key: `roadmap:${milestoneId}:due:4`,
      due_at: "2026-09-30T23:59:59Z",
      payload_json: {
        roadmap_milestone_id: milestoneId,
        event_type: "due",
        milestone_version: 4,
        target_date: "2026-09-30",
        year: 2026,
        quarter: 3,
      },
    });
    expect(normalizeInboxItem(projected)).toMatchObject({
      kind: "event",
      sourceEntityType: "roadmap_milestone",
      sourceEntityId: milestoneId,
      payloadJson: {
        event_type: "due",
        milestone_version: 4,
        target_date: "2026-09-30",
      },
    });
    expect(() =>
      normalizeInboxItem({
        ...projected,
        source_event_key: `roadmap:${milestoneId}:achieved:4`,
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem({
        ...projected,
        payload_json: { ...projected.payload_json, quarter: 4 },
      }),
    ).toThrow(ApiError);
  });

  it("strictly validates Project completion source snapshots", () => {
    const projectId = "018f0000-0000-7000-8000-000000000822";
    const projected = inboxPayload({
      kind: "event",
      title: "项目完成待跟进：官网升级",
      summary: "项目已标记完成，请确认交付收尾、归档或其他后续工作。",
      source_entity_type: "project_completion",
      source_entity_id: projectId,
      source_event_key: `project:${projectId}:completed:5`,
      source_deleted_at: null,
      priority: "P1",
      due_at: null,
      payload_json: {
        project_id: projectId,
        project_name: "官网升级",
        completed_at: "2026-08-28T12:00:00Z",
        completion_version: 5,
        incomplete_task_count: 1,
      },
    });
    expect(normalizeInboxItem(projected)).toMatchObject({
      kind: "event",
      sourceEntityType: "project_completion",
      sourceEntityId: projectId,
      priority: "P1",
      dueAt: null,
      payloadJson: {
        project_name: "官网升级",
        completion_version: 5,
        incomplete_task_count: 1,
      },
    });
    expect(() =>
      normalizeInboxItem({
        ...projected,
        source_event_key: `project:${projectId}:completed:4`,
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem({
        ...projected,
        payload_json: { ...projected.payload_json, unexpected: "value" },
      }),
    ).toThrow(ApiError);
  });

  it("strictly validates Automation Inbox snapshots and source identity", () => {
    const ruleId = "00000000-0000-5000-8000-000000000101";
    const runId = "018f0000-0000-7000-8000-000000000826";
    const projectId = "018f0000-0000-7000-8000-000000000201";
    const sourceEventId = "018f0000-0000-7000-8000-000000000825";
    const projected = inboxPayload({
      kind: "event",
      title: "核对并准备发票：官网升级",
      summary:
        "项目已完成。请人工核对是否需要开票，并准备后续资料；自动化不会生成或发送发票。",
      source_entity_type: "automation",
      source_entity_id: runId,
      source_event_key: `automation:event:${ruleId}:${sourceEventId}`,
      source_deleted_at: null,
      due_at: null,
      payload_json: {
        automation_rule_id: ruleId,
        automation_run_id: runId,
        preset_key: "project-completed-inbox",
        project_id: projectId,
        project_name: "官网升级",
      },
    });

    expect(normalizeInboxItem(projected)).toMatchObject({
      kind: "event",
      sourceEntityType: "automation",
      sourceEntityId: runId,
      sourceEventKey: `automation:event:${ruleId}:${sourceEventId}`,
      sourceDeletedAt: null,
      dueAt: null,
      payloadJson: {
        automation_rule_id: ruleId,
        automation_run_id: runId,
        preset_key: "project-completed-inbox",
        project_id: projectId,
        project_name: "官网升级",
      },
    });
    expect(
      normalizeInboxItem({
        ...projected,
        due_at: "2026-08-30T09:30:00+08:00",
      }),
    ).toMatchObject({ dueAt: "2026-08-30T09:30:00+08:00" });

    for (const invalid of [
      {
        ...projected,
        payload_json: {
          ...projected.payload_json,
          automation_run_id: "018f0000-0000-7000-8000-000000000827",
        },
      },
      {
        ...projected,
        payload_json: { ...projected.payload_json, internal_path: "secret" },
      },
      {
        ...projected,
        payload_json: {
          ...projected.payload_json,
          preset_key: "unknown-preset",
        },
      },
      {
        ...projected,
        source_event_key: `automation:event:018f0000-0000-7000-8000-000000000828:${sourceEventId}`,
      },
      {
        ...projected,
        payload_json: { ...projected.payload_json, project_id: "not-a-uuid" },
      },
      { ...projected, due_at: "2026-08-29" },
      { ...projected, due_at: "2026-08-29T24:00:00Z" },
      { ...projected, due_at: 123 },
      {
        ...projected,
        source_deleted_at: "2026-08-29T12:00:00Z",
      },
    ]) {
      expect(() => normalizeInboxItem(invalid)).toThrow(ApiError);
    }
  });

  it("strictly validates backup-create system maintenance snapshots", () => {
    const projected = inboxPayload({
      kind: "event",
      title: "本地备份需要处理",
      summary:
        "无法创建已验证的本地备份；现有数据没有被修改。请检查本地存储后重试。",
      source_entity_type: "system_maintenance",
      source_entity_id: "backup:create",
      source_event_key:
        "system:backup:create:018f0000-0000-7000-8000-000000000817",
      priority: "P2",
      due_at: null,
      payload_json: {
        component: "backup",
        operation: "create",
        failure_code: "backup_create_failed",
        occurred_at: "2026-08-28T12:00:00.000000000Z",
        message:
          "无法创建已验证的本地备份；现有数据没有被修改。请检查本地存储后重试。",
      },
    });
    expect(normalizeInboxItem(projected)).toMatchObject({
      kind: "event",
      sourceEntityType: "system_maintenance",
      sourceEntityId: "backup:create",
      priority: "P2",
      dueAt: null,
      sourceDeletedAt: null,
      payloadJson: {
        component: "backup",
        operation: "create",
        failure_code: "backup_create_failed",
      },
    });
    expect(() =>
      normalizeInboxItem({
        ...projected,
        payload_json: {
          ...projected.payload_json,
          path: "C:\\\\Users\\\\secret\\\\opc-workspace.db",
        },
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem({
        ...projected,
        payload_json: {
          ...projected.payload_json,
          message: "mkdir C:\\\\Users\\\\secret\\\\backups: access denied",
        },
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem({
        ...projected,
        source_event_key: "system:backup:verify:abc",
      }),
    ).toThrow(ApiError);
  });

  it("strictly validates backup-verify system maintenance snapshots", () => {
    const projected = inboxPayload({
      kind: "event",
      title: "本地备份校验需要处理",
      summary:
        "无法完成已发布备份的完整性校验。现有工作区数据没有被修改。请稍后重试。",
      source_entity_type: "system_maintenance",
      source_entity_id: "backup:verify",
      source_event_key:
        "system:backup:verify:018f0000-0000-7000-8000-000000000819",
      priority: "P3",
      due_at: null,
      payload_json: {
        component: "backup",
        operation: "verify",
        failure_code: "backup_verify_failed",
        occurred_at: "2026-08-28T12:00:00.000000000Z",
        message:
          "无法完成已发布备份的完整性校验。现有工作区数据没有被修改。请稍后重试。",
      },
    });
    expect(normalizeInboxItem(projected)).toMatchObject({
      kind: "event",
      sourceEntityType: "system_maintenance",
      sourceEntityId: "backup:verify",
      priority: "P3",
      dueAt: null,
      payloadJson: {
        component: "backup",
        operation: "verify",
        failure_code: "backup_verify_failed",
      },
    });
    expect(() =>
      normalizeInboxItem({
        ...projected,
        payload_json: {
          ...projected.payload_json,
          backup_id: "018f0000-0000-7000-8000-000000000001",
        },
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem({
        ...projected,
        source_entity_id: "backup:create",
        source_event_key:
          "system:backup:create:018f0000-0000-7000-8000-000000000819",
      }),
    ).toThrow(ApiError);
  });

  it.each([
    {
      source: "backup:drill",
      component: "backup",
      operation: "drill",
      failureCode: "backup_drill_failed",
      message:
        "无法在隔离环境中完成本地备份恢复演练。现有工作区数据没有被修改。请检查备份状态后重试。",
    },
    {
      source: "backup:restore",
      component: "backup",
      operation: "restore",
      failureCode: "backup_restore_failed",
      message:
        "无法安全安排本地备份恢复。现有工作区数据没有被修改。请检查本地存储后重试。",
    },
    {
      source: "database:startup",
      component: "database",
      operation: "startup",
      failureCode: "database_startup_failed",
      message:
        "上次启动未能安全打开本地数据库。工作区没有进入就绪状态；请检查本地存储和应用日志。",
    },
    {
      source: "database:migration",
      component: "database",
      operation: "migration",
      failureCode: "database_migration_failed",
      message:
        "上次启动未能完成受保护的数据库迁移。已有数据未被新版本继续使用；请检查回滚备份和应用日志。",
    },
    {
      source: "database:runtime",
      component: "database",
      operation: "runtime",
      failureCode: "database_runtime_failed",
      message:
        "运行中的本地数据库操作失败。请检查可用磁盘空间和应用日志，并在继续重要写入前创建或校验备份。",
    },
    {
      source: "storage:low_space",
      component: "storage",
      operation: "low_space",
      failureCode: "storage_low_space",
      message:
        "本地数据或备份所在磁盘的可用空间已低于 1 GiB。请释放空间，并在继续重要写入前创建或校验备份。",
    },
    {
      source: "sidecar:startup",
      component: "sidecar",
      operation: "startup",
      failureCode: "sidecar_startup_failed",
      message: "上次本地服务启动未能进入就绪状态。请检查应用日志后重新启动。",
    },
  ])("strictly validates $source maintenance snapshots", (definition) => {
    const projected = inboxPayload({
      kind: "event",
      title: "系统维护需要处理",
      summary: definition.message,
      source_entity_type: "system_maintenance",
      source_entity_id: definition.source,
      source_event_key: `system:${definition.source}:018f0000-0000-7000-8000-000000000820`,
      priority: "P1",
      due_at: null,
      payload_json: {
        component: definition.component,
        operation: definition.operation,
        failure_code: definition.failureCode,
        occurred_at: "2026-08-28T12:00:00.000000000Z",
        message: definition.message,
      },
    });
    expect(normalizeInboxItem(projected)).toMatchObject({
      sourceEntityType: "system_maintenance",
      sourceEntityId: definition.source,
      payloadJson: {
        component: definition.component,
        operation: definition.operation,
        failure_code: definition.failureCode,
      },
    });
    expect(() =>
      normalizeInboxItem({
        ...projected,
        payload_json: {
          ...projected.payload_json,
          failure_code: "unexpected_failure",
        },
      }),
    ).toThrow(ApiError);
  });

  it("serializes view, filters, paging, and snapshot metadata", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({
          data: [inboxPayload()],
          meta: {
            page: 2,
            page_size: 20,
            total: 21,
            unread_total: 4,
            snapshot_at: "2026-08-28T10:05:00.123456789Z",
            server_now: "2026-08-28T10:05:01.123456789Z",
          },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getInboxItems({
      view: "snoozed",
      q: " 交付 ",
      priority: "P1",
      sourceEntityType: "client_followup",
      page: 2,
      pageSize: 20,
    });

    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      view: "snoozed",
      page: "2",
      page_size: "20",
      q: "交付",
      priority: "P1",
      source_entity_type: "client_followup",
    });
    expect(result.meta).toEqual({
      page: 2,
      pageSize: 20,
      total: 21,
      unreadTotal: 4,
      snapshotAt: "2026-08-28T10:05:00.123456789Z",
      serverNow: "2026-08-28T10:05:01.123456789Z",
    });
  });

  it("keeps server-owned manual facts out of create and uses stable-write headers", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({ data: inboxPayload() }, 201),
    );
    vi.stubGlobal("fetch", fetchMock);
    const id = "018f0000-0000-7000-8000-000000000801";

    await createInboxItem(
      {
        title: "确认本周交付范围",
        summary: "整理需要人工确认的边界",
        priority: "P1",
        dueAt: null,
      },
      "inbox-create-1",
    );
    await updateInboxItem(id, {
      title: "确认最终交付范围",
      expectedVersion: 2,
    });
    await executeInboxItemCommand(
      {
        id,
        action: "snooze",
        snoozedUntil: "2026-08-29T12:00:00Z",
        expectedVersion: 3,
      },
      "inbox-snooze-1",
    );

    const createInit = fetchMock.mock.calls[0][1];
    expect(new Headers(createInit?.headers).get("Idempotency-Key")).toBe(
      "inbox-create-1",
    );
    expect(JSON.parse(String(createInit?.body))).toEqual({
      title: "确认本周交付范围",
      summary: "整理需要人工确认的边界",
      priority: "P1",
      due_at: null,
    });
    expect(
      new Headers(fetchMock.mock.calls[1][1]?.headers).get("If-Match"),
    ).toBe('"2"');
    expect(
      new Headers(fetchMock.mock.calls[2][1]?.headers).get("If-Match"),
    ).toBe('"3"');
    expect(
      new Headers(fetchMock.mock.calls[2][1]?.headers).get("Idempotency-Key"),
    ).toBe("inbox-snooze-1");
    expect(JSON.parse(String(fetchMock.mock.calls[2][1]?.body))).toEqual({
      snoozed_until: "2026-08-29T12:00:00Z",
    });
  });

  it("uses the exact list snapshot for read-all and normalizes the actor timeline", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, _init?: RequestInit) => {
        if (String(input).endsWith("/events?page=1&page_size=20")) {
          return jsonResponse({
            data: [
              {
                id: "018f0000-0000-7000-8000-000000000901",
                action: "created",
                actor_id: "00000000-0000-0000-0000-000000000001",
                actor: {
                  id: "00000000-0000-0000-0000-000000000001",
                  type: "owner",
                  display_name: "陶定鑫",
                  status: "active",
                  is_builtin: true,
                  version: 1,
                },
                request_id: "request-1",
                previous: null,
                current: { status: "open", reason: "手工创建" },
                reason: "手工创建",
                created_at: "2026-08-28T10:00:00Z",
              },
            ],
            meta: {
              page: 1,
              page_size: 20,
              total: 1,
              inbox_item_version: 2,
            },
          });
        }
        return jsonResponse({
          data: {
            through_created_at: "2026-08-28T10:05:00.123456789Z",
            marked_count: 3,
          },
        });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      markAllInboxItemsRead(
        "2026-08-28T10:05:00.123456789Z",
        "inbox-read-all-1",
      ),
    ).resolves.toEqual({
      throughCreatedAt: "2026-08-28T10:05:00.123456789Z",
      markedCount: 3,
    });
    const events = await getInboxItemEvents(
      "018f0000-0000-7000-8000-000000000801",
      { page: 1, pageSize: 20 },
    );

    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      through_created_at: "2026-08-28T10:05:00.123456789Z",
    });
    expect(events.items[0].actor?.displayName).toBe("陶定鑫");
    expect(events.items[0].reason).toBe("手工创建");
    expect(events.meta.inboxItemVersion).toBe(2);
  });

  it("strictly normalizes active relations and authoritative progress", () => {
    const result = normalizeInboxItemTaskListResult({
      data: { active: [taskRelationPayload()], history: [] },
      meta: {
        page: 1,
        page_size: 20,
        total: 0,
        inbox_item_version: 2,
        progress: progressPayload(),
      },
    });
    expect(result.active[0]).toMatchObject({
      taskRefId: "018f0000-0000-7000-8000-000000000101",
      taskTitleSnapshot: "整理发布清单",
      isRequired: true,
      position: 1,
      isActive: true,
    });
    expect(result.meta.progress).toMatchObject({
      activeTotal: 1,
      requiredRemaining: 1,
      percent: 0,
    });
    expect(() =>
      normalizeInboxItemTaskListResult({
        data: { active: [taskRelationPayload()], history: [] },
        meta: {
          page: 1,
          page_size: 20,
          total: 0,
          inbox_item_version: 2,
          progress: progressPayload({ active_total: 0 }),
        },
      }),
    ).toThrow(ApiError);
  });

  it("accepts stable ascending active positions with gaps", () => {
    const first = taskRelationPayload({ position: 2 });
    const secondTaskId = "018f0000-0000-7000-8000-000000000102";
    const second = taskRelationPayload({
      id: "018f0000-0000-7000-8000-000000000902",
      task_ref_id: secondTaskId,
      task_id: secondTaskId,
      task_title_snapshot: "核对上线清单",
      task: {
        ...taskRelationPayload().task,
        id: secondTaskId,
        title: "核对上线清单",
      },
      position: 4,
    });
    const result = normalizeInboxItemTaskListResult({
      data: { active: [first, second], history: [] },
      meta: {
        page: 1,
        page_size: 20,
        total: 0,
        inbox_item_version: 4,
        progress: progressPayload({
          active_total: 2,
          required_total: 2,
          required_remaining: 2,
        }),
      },
    });

    expect(result.active.map((relation) => relation.position)).toEqual([2, 4]);
    expect(() =>
      normalizeInboxItemTaskListResult({
        data: {
          active: [
            { ...first, position: 4 },
            { ...second, position: 2 },
          ],
          history: [],
        },
        meta: {
          page: 1,
          page_size: 20,
          total: 0,
          inbox_item_version: 4,
          progress: progressPayload({
            active_total: 2,
            required_total: 2,
            required_remaining: 2,
          }),
        },
      }),
    ).toThrow(ApiError);
  });

  it("uses version and idempotency headers for all relation writes", async () => {
    const inboxItemId = "018f0000-0000-7000-8000-000000000801";
    const taskId = "018f0000-0000-7000-8000-000000000101";
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        const method = init?.method ?? "GET";
        const relation =
          method === "DELETE"
            ? taskRelationPayload({
                is_active: false,
                unlinked_by_actor_id: "00000000-0000-0000-0000-000000000001",
                unlinked_by_actor: actorPayload(),
                unlinked_at: "2026-08-28T10:20:00Z",
                unlink_reason: "不再需要",
              })
            : taskRelationPayload({
                is_required: method === "PATCH" ? false : true,
              });
        return jsonResponse({
          data: {
            inbox_item: inboxPayload({ version: 3, status: "tracking" }),
            relation,
            progress:
              method === "DELETE"
                ? progressPayload({
                    active_total: 0,
                    required_total: 0,
                    required_remaining: 0,
                    percent: null,
                  })
                : method === "PATCH"
                  ? progressPayload({
                      required_total: 0,
                      required_remaining: 0,
                      percent: null,
                    })
                  : progressPayload(),
          },
        });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    await linkInboxItemTask(
      { inboxItemId, taskId, isRequired: true, expectedVersion: 2 },
      "link-key",
    );
    await updateInboxItemTaskRequirement(
      { inboxItemId, taskId, isRequired: false, expectedVersion: 3 },
      "required-key",
    );
    await unlinkInboxItemTask(
      { inboxItemId, taskId, reason: "不再需要", expectedVersion: 4 },
      "unlink-key",
    );

    expect(fetchMock.mock.calls.map((call) => call[1]?.method)).toEqual([
      "POST",
      "PATCH",
      "DELETE",
    ]);
    expect(
      fetchMock.mock.calls.map((call) =>
        new Headers(call[1]?.headers).get("If-Match"),
      ),
    ).toEqual(['"2"', '"3"', '"4"']);
    expect(
      fetchMock.mock.calls.map((call) =>
        new Headers(call[1]?.headers).get("Idempotency-Key"),
      ),
    ).toEqual(["link-key", "required-key", "unlink-key"]);
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      is_required: true,
    });
    expect(JSON.parse(String(fetchMock.mock.calls[2][1]?.body))).toEqual({
      reason: "不再需要",
    });
  });

  it("requests all active relations with paginated history", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: { active: [taskRelationPayload()], history: [] },
        meta: {
          page: 2,
          page_size: 10,
          total: 12,
          inbox_item_version: 2,
          progress: progressPayload(),
        },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getInboxItemTasks(
      "018f0000-0000-7000-8000-000000000801",
      { page: 2, pageSize: 10 },
    );
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "10",
    });
    expect(result.meta.total).toBe(12);
  });
});
