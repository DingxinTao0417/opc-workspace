import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import type { InboxItem } from "../types/models";
import { useUiStore } from "../store/ui";
import { InboxSourceContext } from "./InboxSourceContext";

const sourceItem: InboxItem = {
  id: "018f0000-0000-7000-8000-000000000801",
  kind: "event",
  title: "跟进产出：交付清单",
  summary: "需要处理",
  sourceEntityType: "task_artifact",
  sourceEntityId: "018f0000-0000-7000-8000-000000000802",
  sourceEventKey: "task-artifact:018f0000-0000-7000-8000-000000000802:followup",
  sourceDeletedAt: null,
  priority: "P1",
  status: "open",
  resolutionPolicy: "manual",
  dueAt: null,
  readAt: null,
  triagedAt: null,
  snoozedUntil: null,
  resolvedByActorId: null,
  resolvedAt: null,
  resolutionReason: null,
  resolutionMode: null,
  dismissedByActorId: null,
  dismissedAt: null,
  dismissReason: null,
  payloadJson: {
    artifact_id: "018f0000-0000-7000-8000-000000000802",
    artifact_name: "交付清单",
    storage_kind: "file",
    task_id: "018f0000-0000-7000-8000-000000000803",
    task_title: "准备项目交付",
    submission_id: "018f0000-0000-7000-8000-000000000804",
    submission_sequence: 2,
    project_id: "018f0000-0000-7000-8000-000000000805",
    project_name: "官网升级",
  },
  version: 1,
  createdAt: "2026-08-28T10:00:00Z",
  updatedAt: "2026-08-28T10:00:00Z",
  availableActions: ["edit", "read", "snooze", "resolve", "dismiss"],
};

afterEach(() => {
  cleanup();
  useUiStore.setState({ settingsOpen: false, settingsModule: "general" });
});

describe("InboxSourceContext", () => {
  it("shows the immutable Task Artifact snapshot and precise Task link", () => {
    render(
      <MemoryRouter>
        <InboxSourceContext item={sourceItem} />
      </MemoryRouter>,
    );

    expect(screen.getByText("交付清单")).toBeTruthy();
    expect(screen.getByText("准备项目交付")).toBeTruthy();
    expect(screen.getByText("官网升级")).toBeTruthy();
    expect(screen.getByText("第 2 批提交")).toBeTruthy();
    expect(screen.getByRole("link", { name: /查看来源任务/ })).toHaveAttribute(
      "href",
      "/tasks/018f0000-0000-7000-8000-000000000803",
    );
  });

  it("explains a deleted source without dropping its snapshot", () => {
    render(
      <MemoryRouter>
        <InboxSourceContext
          item={{ ...sourceItem, sourceDeletedAt: "2026-08-28T11:00:00Z" }}
        />
      </MemoryRouter>,
    );

    expect(screen.getByRole("status")).toHaveTextContent("来源产出已删除");
    expect(screen.getByText("准备项目交付")).toBeTruthy();
    expect(screen.queryByRole("link", { name: /查看来源任务/ })).toBeNull();
  });

  it("shows a Task blocked source and hides its dead link after deletion", () => {
    const blockedItem: InboxItem = {
      ...sourceItem,
      title: "任务阻塞：等待客户确认",
      sourceEntityType: "task",
      sourceEntityId: "018f0000-0000-7000-8000-000000000806",
      sourceEventKey: "task:018f0000-0000-7000-8000-000000000806:blocked:4",
      payloadJson: {
        task_id: "018f0000-0000-7000-8000-000000000806",
        task_title: "等待客户确认",
        blocked_reason: "尚未收到确认",
        blocked_at: "2026-08-28T10:00:00Z",
        blocked_from_status: "in_progress",
        block_version: 4,
        project_name: "官网升级",
      },
    };
    const { rerender } = render(
      <MemoryRouter>
        <InboxSourceContext item={blockedItem} />
      </MemoryRouter>,
    );

    expect(screen.getByText("任务阻塞")).toBeTruthy();
    expect(screen.getByText("等待客户确认")).toBeTruthy();
    expect(screen.getByText("尚未收到确认")).toBeTruthy();
    expect(screen.getByText("进行中")).toBeTruthy();
    expect(screen.getByRole("link", { name: /查看来源任务/ })).toHaveAttribute(
      "href",
      "/tasks/018f0000-0000-7000-8000-000000000806",
    );

    rerender(
      <MemoryRouter>
        <InboxSourceContext
          item={{ ...blockedItem, sourceDeletedAt: "2026-08-28T11:00:00Z" }}
        />
      </MemoryRouter>,
    );
    expect(screen.getByRole("status")).toHaveTextContent("来源任务已删除");
    expect(screen.queryByRole("link", { name: /查看来源任务/ })).toBeNull();
  });

  it("shows a Task due snapshot and keeps it after source deletion", () => {
    const dueItem: InboxItem = {
      ...sourceItem,
      title: "任务临期：准备项目交付",
      sourceEntityType: "task_due",
      sourceEntityId: "018f0000-0000-7000-8000-000000000807",
      sourceEventKey:
        "task:018f0000-0000-7000-8000-000000000807:due:2026-08-29T10:00:00Z",
      dueAt: "2026-08-29T10:00:00Z",
      payloadJson: {
        task_id: "018f0000-0000-7000-8000-000000000807",
        task_title: "准备项目交付",
        due_at: "2026-08-29T10:00:00Z",
        projected_at: "2026-08-28T10:00:00Z",
        due_state: "due_soon",
        lead_minutes: 1440,
        project_name: "官网升级",
      },
    };
    const { rerender } = render(
      <MemoryRouter>
        <InboxSourceContext item={dueItem} />
      </MemoryRouter>,
    );

    expect(screen.getByText("任务临期")).toBeTruthy();
    expect(screen.getByText("提前 24 小时进入收件箱")).toBeTruthy();
    expect(screen.getByText("准备项目交付")).toBeTruthy();
    expect(screen.getByText("官网升级")).toBeTruthy();
    expect(screen.getByRole("link", { name: /查看来源任务/ })).toHaveAttribute(
      "href",
      "/tasks/018f0000-0000-7000-8000-000000000807",
    );

    rerender(
      <MemoryRouter>
        <InboxSourceContext
          item={{ ...dueItem, sourceDeletedAt: "2026-08-30T10:00:00Z" }}
        />
      </MemoryRouter>,
    );
    expect(screen.getByRole("status")).toHaveTextContent("来源任务已删除");
    expect(screen.getByText("准备项目交付")).toBeTruthy();
    expect(screen.queryByRole("link", { name: /查看来源任务/ })).toBeNull();
  });

  it("routes a due Client Follow-up to its client without inventing an external action", () => {
    const clientId = "018f0000-0000-7000-8000-000000000808";
    const followupId = "018f0000-0000-7000-8000-000000000809";
    const followupItem: InboxItem = {
      ...sourceItem,
      title: "确认项目验收",
      summary: "客户：星河工作室 · 渠道：微信",
      sourceEntityType: "client_followup",
      sourceEntityId: followupId,
      sourceEventKey: `followup:${followupId}:due:2`,
      dueAt: "2026-08-30T10:00:00Z",
      payloadJson: {
        client_followup_id: followupId,
        client_id: clientId,
        scheduled_at: "2026-08-30T10:00:00Z",
        timezone: "Asia/Shanghai",
        channel: "微信",
      },
    };
    render(
      <MemoryRouter>
        <InboxSourceContext item={followupItem} />
      </MemoryRouter>,
    );

    expect(screen.getByText("客户回访到期")).toBeTruthy();
    expect(screen.getByText("本地计划提醒")).toBeTruthy();
    expect(screen.getByText("微信")).toBeTruthy();
    expect(screen.getByRole("link", { name: "查看客户回访" })).toHaveAttribute(
      "href",
      `/clients/${clientId}`,
    );
  });

  it("shows a Content Item schedule snapshot and removes the live link after deletion", () => {
    const contentItemId = "018f0000-0000-7000-8000-000000000823";
    const contentItem: InboxItem = {
      ...sourceItem,
      title: "内容待发布：版本更新说明",
      summary: "平台：Newsletter · 计划时间：2026-08-30T10:00:00Z",
      sourceEntityType: "content_item",
      sourceEntityId: contentItemId,
      sourceEventKey: `content:${contentItemId}:publish_due:3`,
      dueAt: "2026-08-30T10:00:00Z",
      payloadJson: {
        content_item_id: contentItemId,
        event_type: "publish_due",
        content_version: 3,
        scheduled_at: "2026-08-30T10:00:00Z",
        scheduled_timezone: "Asia/Shanghai",
      },
    };
    const { rerender } = render(
      <MemoryRouter>
        <InboxSourceContext item={contentItem} />
      </MemoryRouter>,
    );

    expect(screen.getByText("内容待发布")).toBeTruthy();
    expect(screen.getByText("本地内容排期")).toBeTruthy();
    expect(screen.getByText("Asia/Shanghai")).toBeTruthy();
    expect(screen.getByText("v3")).toBeTruthy();
    expect(screen.getByRole("link", { name: /查看内容日历/ })).toHaveAttribute(
      "href",
      "/content-calendar",
    );

    rerender(
      <MemoryRouter>
        <InboxSourceContext
          item={{ ...contentItem, sourceDeletedAt: "2026-08-31T10:00:00Z" }}
        />
      </MemoryRouter>,
    );
    expect(screen.getByRole("status")).toHaveTextContent("来源内容已删除");
    expect(screen.queryByRole("link", { name: /查看内容日历/ })).toBeNull();
  });

  it("shows a Project completion snapshot and hides its link after deletion", () => {
    const projectId = "018f0000-0000-7000-8000-000000000822";
    const projectItem: InboxItem = {
      ...sourceItem,
      title: "项目完成待跟进：官网升级",
      summary: "项目已标记完成，请确认交付收尾、归档或其他后续工作。",
      sourceEntityType: "project_completion",
      sourceEntityId: projectId,
      sourceEventKey: `project:${projectId}:completed:5`,
      dueAt: null,
      payloadJson: {
        project_id: projectId,
        project_name: "官网升级",
        completed_at: "2026-08-28T12:00:00Z",
        completion_version: 5,
        incomplete_task_count: 1,
      },
    };
    const { rerender } = render(
      <MemoryRouter>
        <InboxSourceContext item={projectItem} />
      </MemoryRouter>,
    );

    expect(screen.getByText("项目完成")).toBeTruthy();
    expect(screen.getByText("官网升级")).toBeTruthy();
    expect(screen.getByText("1 项")).toBeTruthy();
    expect(screen.getByRole("link", { name: /查看来源项目/ })).toHaveAttribute(
      "href",
      `/projects/${projectId}`,
    );

    rerender(
      <MemoryRouter>
        <InboxSourceContext
          item={{ ...projectItem, sourceDeletedAt: "2026-08-29T12:00:00Z" }}
        />
      </MemoryRouter>,
    );
    expect(screen.getByRole("status")).toHaveTextContent("来源项目已删除");
    expect(screen.getByText("官网升级")).toBeTruthy();
    expect(screen.queryByRole("link", { name: /查看来源项目/ })).toBeNull();
  });

  it("shows a safe backup-create maintenance snapshot and opens data settings", () => {
    const maintenanceItem: InboxItem = {
      ...sourceItem,
      title: "本地备份需要处理",
      summary:
        "无法创建已验证的本地备份；现有数据没有被修改。请检查本地存储后重试。",
      sourceEntityType: "system_maintenance",
      sourceEntityId: "backup:create",
      sourceEventKey:
        "system:backup:create:018f0000-0000-7000-8000-000000000818",
      dueAt: null,
      payloadJson: {
        component: "backup",
        operation: "create",
        failure_code: "backup_create_failed",
        occurred_at: "2026-08-28T12:00:00.000000000Z",
        message:
          "无法创建已验证的本地备份；现有数据没有被修改。请检查本地存储后重试。",
      },
    };
    render(
      <MemoryRouter>
        <InboxSourceContext item={maintenanceItem} />
      </MemoryRouter>,
    );

    expect(screen.getByText("系统维护")).toBeTruthy();
    expect(screen.getByText("本地备份创建失败")).toBeTruthy();
    expect(screen.getByText("本地备份")).toBeTruthy();
    expect(screen.getByText("创建")).toBeTruthy();
    expect(
      screen.getByText(
        "无法创建已验证的本地备份；现有数据没有被修改。请检查本地存储后重试。",
      ),
    ).toBeTruthy();
    expect(screen.queryByText(/C:\\/)).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "打开数据与备份" }));
    expect(useUiStore.getState()).toMatchObject({
      settingsOpen: true,
      settingsModule: "data",
    });
  });

  it("shows a safe backup-verify maintenance snapshot", () => {
    const maintenanceItem: InboxItem = {
      ...sourceItem,
      title: "本地备份校验需要处理",
      summary:
        "无法完成已发布备份的完整性校验。现有工作区数据没有被修改。请稍后重试。",
      sourceEntityType: "system_maintenance",
      sourceEntityId: "backup:verify",
      sourceEventKey:
        "system:backup:verify:018f0000-0000-7000-8000-000000000819",
      dueAt: null,
      payloadJson: {
        component: "backup",
        operation: "verify",
        failure_code: "backup_verify_failed",
        occurred_at: "2026-08-28T12:00:00.000000000Z",
        message:
          "无法完成已发布备份的完整性校验。现有工作区数据没有被修改。请稍后重试。",
      },
    };
    render(
      <MemoryRouter>
        <InboxSourceContext item={maintenanceItem} />
      </MemoryRouter>,
    );

    expect(screen.getByText("本地备份校验失败")).toBeTruthy();
    expect(screen.getByText("校验")).toBeTruthy();
    expect(
      screen.getByText(
        "无法完成已发布备份的完整性校验。现有工作区数据没有被修改。请稍后重试。",
      ),
    ).toBeTruthy();
    expect(screen.queryByText(/backup-id/i)).toBeNull();
  });

  it("shows a database migration incident with the data recovery entry", () => {
    const message =
      "上次启动未能完成受保护的数据库迁移。已有数据未被新版本继续使用；请检查回滚备份和应用日志。";
    render(
      <MemoryRouter>
        <InboxSourceContext
          item={{
            ...sourceItem,
            title: "本地数据库迁移需要处理",
            summary: message,
            sourceEntityType: "system_maintenance",
            sourceEntityId: "database:migration",
            sourceEventKey:
              "system:database:migration:018f0000-0000-7000-8000-000000000820",
            dueAt: null,
            payloadJson: {
              component: "database",
              operation: "migration",
              failure_code: "database_migration_failed",
              occurred_at: "2026-08-28T12:00:00.000000000Z",
              message,
            },
          }}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText("本地数据库迁移失败")).toBeTruthy();
    expect(screen.getByText("本地数据库")).toBeTruthy();
    expect(screen.getByText("迁移")).toBeTruthy();
    expect(screen.getByRole("button", { name: "打开数据与备份" })).toBeTruthy();
  });

  it("shows a runtime database incident with the data recovery entry", () => {
    const message =
      "运行中的本地数据库操作失败。请检查可用磁盘空间和应用日志，并在继续重要写入前创建或校验备份。";
    render(
      <MemoryRouter>
        <InboxSourceContext
          item={{
            ...sourceItem,
            title: "本地数据库运行需要处理",
            summary: message,
            sourceEntityType: "system_maintenance",
            sourceEntityId: "database:runtime",
            sourceEventKey:
              "system:database:runtime:018f0000-0000-7000-8000-000000000822",
            dueAt: null,
            payloadJson: {
              component: "database",
              operation: "runtime",
              failure_code: "database_runtime_failed",
              occurred_at: "2026-08-28T12:00:00.000000000Z",
              message,
            },
          }}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText("本地数据库运行失败")).toBeTruthy();
    expect(screen.getByText("运行")).toBeTruthy();
    expect(screen.getByRole("button", { name: "打开数据与备份" })).toBeTruthy();
  });

  it("shows a low-space incident with the data recovery entry", () => {
    const message =
      "本地数据或备份所在磁盘的可用空间已低于 1 GiB。请释放空间，并在继续重要写入前创建或校验备份。";
    render(
      <MemoryRouter>
        <InboxSourceContext
          item={{
            ...sourceItem,
            title: "本地存储空间不足",
            summary: message,
            sourceEntityType: "system_maintenance",
            sourceEntityId: "storage:low_space",
            sourceEventKey:
              "system:storage:low_space:018f0000-0000-7000-8000-000000000823",
            dueAt: null,
            payloadJson: {
              component: "storage",
              operation: "low_space",
              failure_code: "storage_low_space",
              occurred_at: "2026-08-28T12:00:00.000000000Z",
              message,
            },
          }}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText("本地存储空间不足")).toBeTruthy();
    expect(screen.getByText("容量检查")).toBeTruthy();
    expect(screen.getByRole("button", { name: "打开数据与备份" })).toBeTruthy();
  });

  it("shows a Sidecar startup incident without inventing an unavailable log action", () => {
    const message =
      "上次本地服务启动未能进入就绪状态。请检查应用日志后重新启动。";
    render(
      <MemoryRouter>
        <InboxSourceContext
          item={{
            ...sourceItem,
            title: "本地服务启动需要处理",
            summary: message,
            sourceEntityType: "system_maintenance",
            sourceEntityId: "sidecar:startup",
            sourceEventKey:
              "system:sidecar:startup:018f0000-0000-7000-8000-000000000821",
            dueAt: null,
            payloadJson: {
              component: "sidecar",
              operation: "startup",
              failure_code: "sidecar_startup_failed",
              occurred_at: "2026-08-28T12:00:00.000000000Z",
              message,
            },
          }}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText("本地服务启动失败")).toBeTruthy();
    expect(screen.getByText("本地服务")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "打开数据与备份" })).toBeNull();
  });
});
