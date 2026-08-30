import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { InboxItem } from "../types/models";
import { InboxTaskOrchestrationModal } from "./InboxTaskOrchestrationModal";

const sourceProjectId = "018f0000-0000-7000-8000-000000000901";

const item: InboxItem = {
  id: "inbox-1",
  kind: "manual",
  title: "审核并拆分发布事项",
  summary: "",
  sourceEntityType: "manual",
  sourceEntityId: null,
  sourceEventKey: null,
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
  payloadJson: {},
  version: 4,
  createdAt: "2026-08-28T10:00:00Z",
  updatedAt: "2026-08-28T10:00:00Z",
  availableActions: ["edit", "read", "snooze", "resolve", "dismiss"],
};

function invoiceDueItem(projectId: string | null): InboxItem {
  const invoiceId = "018f0000-0000-7000-8000-000000000826";
  return {
    ...item,
    id: `inbox-invoice-${projectId ?? "unassigned"}`,
    kind: "event",
    title: "发票临期：INV-2026-0829",
    sourceEntityType: "invoice_due",
    sourceEntityId: invoiceId,
    sourceEventKey: `invoice:${invoiceId}:due_soon:2026-09-01`,
    dueAt: "2026-08-29T09:00:00+08:00",
    payloadJson: {
      invoice_id: invoiceId,
      invoice_number: "INV-2026-0829",
      client_id: "018f0000-0000-7000-8000-000000000827",
      client_name: "星河设计事务所",
      project_id: projectId,
      project_name: projectId ? "来源项目" : null,
      amount_minor: 128045,
      currency: "CNY",
      due_date: "2026-09-01",
      due_state: "due_soon",
      occurrence_date: "2026-08-29",
      invoice_version: 4,
      projected_at: "2026-08-29T08:00:00.123456789Z",
      lead_days: 3,
    },
  };
}

const mocks = vi.hoisted(() => ({
  actors: vi.fn(),
  split: {
    error: null as unknown,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  },
}));

vi.mock("./ProjectSelect", () => ({
  ProjectSelect: ({
    ariaLabel,
    disabled,
    emptyLabel,
    onChange,
    value,
  }: {
    ariaLabel: string;
    disabled?: boolean;
    emptyLabel: string;
    onChange: (value: string) => void;
    value: string;
  }) => (
    <select
      aria-label={ariaLabel}
      disabled={disabled}
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      <option value="">{emptyLabel}</option>
      <option value="project-1">官网发布</option>
      <option value={sourceProjectId}>来源项目</option>
    </select>
  ),
}));

vi.mock("../api/hooks", () => ({
  useAssignmentActorOptionsQuery: mocks.actors,
  useSplitInboxItem: () => mocks.split,
}));

describe("InboxTaskOrchestrationModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.split.error = null;
    mocks.split.isPending = false;
    mocks.actors.mockReturnValue({
      data: [
        {
          id: "owner-1",
          type: "owner",
          displayName: "我",
          status: "active",
          isBuiltin: true,
          version: 1,
        },
        {
          id: "person-1",
          type: "person",
          displayName: "外部协作者",
          status: "active",
          isBuiltin: false,
          version: 1,
        },
      ],
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
  });

  afterEach(() => {
    cleanup();
    mocks.split.error = null;
  });

  it("submits ordered parent-child Tasks with assignees and auto policy", async () => {
    const onClose = vi.fn();
    render(
      <InboxTaskOrchestrationModal
        expectedVersion={4}
        item={item}
        onClose={onClose}
        open
      />,
    );

    await waitFor(() =>
      expect((screen.getByLabelText("负责人") as HTMLSelectElement).value).toBe(
        "owner-1",
      ),
    );
    expect(
      screen.getAllByRole("option", {
        name: "外部协作者（仅本地责任记录）",
      }),
    ).not.toHaveLength(0);
    fireEvent.change(screen.getByLabelText("任务名称"), {
      target: { value: "准备发布资料" },
    });
    fireEvent.click(screen.getByRole("button", { name: "添加任务" }));
    const names = screen.getAllByLabelText("任务名称");
    fireEvent.change(names[1], { target: { value: "正式发布" } });
    const assignees = screen.getAllByLabelText("负责人");
    fireEvent.change(assignees[1], { target: { value: "person-1" } });
    const parents = screen.getAllByLabelText("父任务");
    fireEvent.change(parents[1], { target: { value: "task-1" } });
    fireEvent.change(screen.getByLabelText("第 2 个任务项目"), {
      target: { value: "project-1" },
    });
    const reviews = screen.getAllByLabelText("验收");
    fireEvent.change(reviews[1], { target: { value: "manual" } });
    const criteria = screen.getAllByLabelText("完成条件");
    fireEvent.change(criteria[1], { target: { value: "客户确认已上线" } });

    fireEvent.click(screen.getByRole("button", { name: "创建并开始跟踪" }));

    expect(mocks.split.mutate).toHaveBeenCalledTimes(1);
    expect(mocks.split.mutate.mock.calls[0][0]).toMatchObject({
      inboxItemId: "inbox-1",
      expectedVersion: 4,
      resolutionPolicy: "all_required_tasks_done",
      tasks: [
        {
          key: "task-1",
          parentKey: null,
          title: "准备发布资料",
          assigneeActorId: "owner-1",
          isRequired: true,
        },
        {
          key: "task-2",
          parentKey: "task-1",
          title: "正式发布",
          assigneeActorId: "person-1",
          projectId: "project-1",
          reviewPolicy: "manual",
          completionCriteria: "客户确认已上线",
          isRequired: true,
        },
      ],
    });
  });

  it("inherits a trusted source Project for every draft and still allows clearing it", async () => {
    const followupItem: InboxItem = {
      ...item,
      id: "inbox-followup",
      kind: "event",
      sourceEntityType: "task_artifact",
      sourceEntityId: "018f0000-0000-7000-8000-000000000902",
      sourceEventKey:
        "task-artifact:018f0000-0000-7000-8000-000000000902:followup",
      payloadJson: { project_id: sourceProjectId },
    };
    render(
      <InboxTaskOrchestrationModal
        expectedVersion={4}
        item={followupItem}
        onClose={vi.fn()}
        open
      />,
    );

    await waitFor(() =>
      expect(
        (screen.getByLabelText("第 1 个任务项目") as HTMLSelectElement).value,
      ).toBe(sourceProjectId),
    );
    expect(screen.getByText(/已从来源带入项目/)).toBeVisible();
    fireEvent.change(screen.getByLabelText("任务名称"), {
      target: { value: "整理交付清单" },
    });
    fireEvent.click(screen.getByRole("button", { name: "添加任务" }));
    const projectInputs = screen.getAllByLabelText(/第 \d+ 个任务项目/);
    expect((projectInputs[1] as HTMLSelectElement).value).toBe(sourceProjectId);
    fireEvent.change(projectInputs[0], { target: { value: "" } });
    fireEvent.change(screen.getAllByLabelText("任务名称")[1], {
      target: { value: "客户确认交付" },
    });
    fireEvent.change(screen.getAllByLabelText("完成条件")[1], {
      target: { value: "客户书面确认" },
    });

    fireEvent.click(screen.getByRole("button", { name: "创建并开始跟踪" }));

    expect(mocks.split.mutate.mock.calls[0][0].tasks).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ title: "整理交付清单", projectId: null }),
        expect.objectContaining({
          title: "客户确认交付",
          projectId: sourceProjectId,
          completionCriteria: "客户书面确认",
        }),
      ]),
    );
  });

  it("inherits an Automation snapshot Project for existing and added drafts while allowing a clear", async () => {
    const automationItem: InboxItem = {
      ...item,
      id: "inbox-automation",
      kind: "event",
      sourceEntityType: "automation",
      sourceEntityId: "018f0000-0000-7000-8000-000000000826",
      sourceEventKey:
        "automation:event:00000000-0000-5000-8000-000000000101:018f0000-0000-7000-8000-000000000825",
      payloadJson: {
        automation_rule_id: "00000000-0000-5000-8000-000000000101",
        automation_run_id: "018f0000-0000-7000-8000-000000000826",
        preset_key: "project-completed-inbox",
        project_id: sourceProjectId,
        project_name: "来源项目",
      },
    };
    render(
      <InboxTaskOrchestrationModal
        expectedVersion={4}
        item={automationItem}
        onClose={vi.fn()}
        open
      />,
    );

    const firstProject = screen.getByLabelText(
      "第 1 个任务项目",
    ) as HTMLSelectElement;
    await waitFor(() => expect(firstProject.value).toBe(sourceProjectId));
    expect(screen.getByText(/已从来源带入项目/)).toBeVisible();

    fireEvent.change(screen.getByLabelText("任务名称"), {
      target: { value: "核对开票条件" },
    });
    fireEvent.click(screen.getByRole("button", { name: "添加任务" }));
    const projectInputs = screen.getAllByLabelText(/第 \d+ 个任务项目/);
    expect((projectInputs[1] as HTMLSelectElement).value).toBe(sourceProjectId);

    fireEvent.change(projectInputs[0], { target: { value: "" } });
    fireEvent.change(screen.getAllByLabelText("任务名称")[1], {
      target: { value: "准备发票资料" },
    });
    fireEvent.change(screen.getAllByLabelText("完成条件")[1], {
      target: { value: "资料已核对" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建并开始跟踪" }));

    expect(mocks.split.mutate.mock.calls[0][0].tasks).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ title: "核对开票条件", projectId: null }),
        expect.objectContaining({
          title: "准备发票资料",
          projectId: sourceProjectId,
        }),
      ]),
    );
  });

  it("inherits a canonical Invoice snapshot Project and lets each draft clear or change it", async () => {
    render(
      <InboxTaskOrchestrationModal
        expectedVersion={4}
        item={invoiceDueItem(sourceProjectId)}
        onClose={vi.fn()}
        open
      />,
    );

    const firstProject = screen.getByLabelText(
      "第 1 个任务项目",
    ) as HTMLSelectElement;
    await waitFor(() => expect(firstProject.value).toBe(sourceProjectId));
    expect(screen.getByText(/已从来源带入项目/)).toBeVisible();

    fireEvent.change(screen.getByLabelText("任务名称"), {
      target: { value: "核对临期发票" },
    });
    fireEvent.click(screen.getByRole("button", { name: "添加任务" }));
    const projectInputs = screen.getAllByLabelText(/第 \d+ 个任务项目/);
    expect((projectInputs[1] as HTMLSelectElement).value).toBe(sourceProjectId);

    fireEvent.change(projectInputs[0], { target: { value: "" } });
    fireEvent.change(projectInputs[1], { target: { value: "project-1" } });
    fireEvent.change(screen.getAllByLabelText("任务名称")[1], {
      target: { value: "准备跟进资料" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建并开始跟踪" }));

    expect(mocks.split.mutate.mock.calls[0][0].tasks).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ title: "核对临期发票", projectId: null }),
        expect.objectContaining({
          title: "准备跟进资料",
          projectId: "project-1",
        }),
      ]),
    );
  });

  it("keeps every Invoice draft unassigned when its snapshot has no Project", async () => {
    render(
      <InboxTaskOrchestrationModal
        expectedVersion={4}
        item={invoiceDueItem(null)}
        onClose={vi.fn()}
        open
      />,
    );

    await waitFor(() =>
      expect((screen.getByLabelText("负责人") as HTMLSelectElement).value).toBe(
        "owner-1",
      ),
    );
    expect(
      (screen.getByLabelText("第 1 个任务项目") as HTMLSelectElement).value,
    ).toBe("");
    expect(screen.queryByText(/已从来源带入项目/)).toBeNull();

    fireEvent.change(screen.getByLabelText("任务名称"), {
      target: { value: "核对无项目发票" },
    });
    fireEvent.click(screen.getByRole("button", { name: "添加任务" }));
    const projectInputs = screen.getAllByLabelText(/第 \d+ 个任务项目/);
    expect((projectInputs[1] as HTMLSelectElement).value).toBe("");
    fireEvent.change(screen.getAllByLabelText("任务名称")[1], {
      target: { value: "整理发票资料" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建并开始跟踪" }));

    expect(mocks.split.mutate.mock.calls[0][0].tasks).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          title: "核对无项目发票",
          projectId: null,
        }),
        expect.objectContaining({ title: "整理发票资料", projectId: null }),
      ]),
    );
  });

  it("does not trust a non-canonical Invoice snapshot Project id", () => {
    render(
      <InboxTaskOrchestrationModal
        expectedVersion={4}
        item={invoiceDueItem("project-1")}
        onClose={vi.fn()}
        open
      />,
    );

    expect(
      (screen.getByLabelText("第 1 个任务项目") as HTMLSelectElement).value,
    ).toBe("");
    expect(screen.queryByText(/已从来源带入项目/)).toBeNull();
  });

  it("keeps the draft visible when the transaction fails", async () => {
    mocks.split.error = new Error("failed");
    const { rerender } = render(
      <InboxTaskOrchestrationModal
        expectedVersion={4}
        item={item}
        onClose={vi.fn()}
        open
      />,
    );
    await waitFor(() =>
      expect((screen.getByLabelText("负责人") as HTMLSelectElement).value).toBe(
        "owner-1",
      ),
    );
    fireEvent.change(screen.getByLabelText("任务名称"), {
      target: { value: "保留的拆分草稿" },
    });
    rerender(
      <InboxTaskOrchestrationModal
        expectedVersion={4}
        item={item}
        onClose={vi.fn()}
        open
      />,
    );
    expect(screen.getByDisplayValue("保留的拆分草稿")).toBeInTheDocument();
    mocks.split.error = null;
  });

  it("rejects an automatic policy with no required task", async () => {
    render(
      <InboxTaskOrchestrationModal
        expectedVersion={4}
        item={item}
        onClose={vi.fn()}
        open
      />,
    );
    await waitFor(() =>
      expect((screen.getByLabelText("负责人") as HTMLSelectElement).value).toBe(
        "owner-1",
      ),
    );
    fireEvent.change(screen.getByLabelText("任务名称"), {
      target: { value: "仅作参考任务" },
    });
    fireEvent.click(screen.getByRole("checkbox", { name: /必需任务/ }));
    fireEvent.click(screen.getByRole("button", { name: "创建并开始跟踪" }));

    expect(screen.getByRole("alert")).toHaveTextContent(
      "自动解决策略至少需要一项必需任务",
    );
    expect(mocks.split.mutate).not.toHaveBeenCalled();
  });

  it("closes after a successful split even when the follow-up refresh fails", async () => {
    const onClose = vi.fn();
    const onCreated = vi.fn().mockRejectedValue(new Error("refresh failed"));
    mocks.split.mutate.mockImplementationOnce(
      (_input, options: { onSuccess: () => void }) => options.onSuccess(),
    );
    render(
      <InboxTaskOrchestrationModal
        expectedVersion={4}
        item={item}
        onClose={onClose}
        onCreated={onCreated}
        open
      />,
    );
    await waitFor(() =>
      expect((screen.getByLabelText("负责人") as HTMLSelectElement).value).toBe(
        "owner-1",
      ),
    );
    fireEvent.change(screen.getByLabelText("任务名称"), {
      target: { value: "已经成功创建的任务" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建并开始跟踪" }));

    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(1));
    expect(mocks.split.mutate).toHaveBeenCalledTimes(1);
  });
});
