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
});
