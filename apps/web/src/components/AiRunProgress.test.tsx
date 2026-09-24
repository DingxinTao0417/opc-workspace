import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AiRunProgress } from "./AiRunProgress";
import type { AiRunProgress as Step } from "../types/models";

const step: Step = {
  sequence: 2,
  kind: "tool_call",
  tool_name: "workspace_propose",
  status: "running",
  started_at: "2026-09-18T10:00:00Z",
  duration_ms: 0,
};
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});
it("shows current tool and elapsed time without suggesting approval", () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date(step.started_at));
  const view = render(<AiRunProgress steps={[step]} live />);
  expect(screen.getByRole("status")).toHaveTextContent(
    "准备操作建议（仍需确认） · 进行中",
  );
  act(() => {
    vi.advanceTimersByTime(2000);
  });
  expect(view.container.querySelector("summary")).toHaveTextContent("2 秒");
  view.rerender(<AiRunProgress steps={[step]} />);
  expect(screen.getByRole("status")).toHaveTextContent("结果未确认");
  expect(view.container.querySelector("summary")).not.toHaveTextContent("秒");
  expect(vi.getTimerCount()).toBe(0);
});
it("reports automatic retries of the model turn", () => {
  const retried: Step = {
    sequence: 2,
    kind: "model_turn",
    turn_index: 1,
    status: "succeeded",
    started_at: "2026-09-18T10:00:00Z",
    completed_at: "2026-09-18T10:00:03Z",
    duration_ms: 3000,
    retry_count: 2,
    retry_reason: "upstream_503",
  };
  const view = render(
    <AiRunProgress steps={[{ ...retried, status: "running" }]} live />,
  );
  expect(screen.getByRole("status")).toHaveTextContent(
    "模型推理 · 第 1 轮 · 已自动重试 2 次",
  );
  expect(view.container).toHaveTextContent(
    "模型推理 · 第 1 轮 · 已自动重试 2 次",
  );
  view.rerender(
    <AiRunProgress
      steps={[{ ...retried, sequence: 3, retry_count: 0, retry_reason: "" }]}
    />,
  );
  expect(view.container).not.toHaveTextContent("重试");
});

it("settles cancelled steps and clears the timer on unmount", () => {
  vi.useFakeTimers();
  const view = render(<AiRunProgress steps={[step]} live />);
  expect(vi.getTimerCount()).toBe(1);
  view.rerender(
    <AiRunProgress
      steps={[{ ...step, status: "cancelled", completed_at: step.started_at }]}
    />,
  );
  expect(view.container).toHaveTextContent("已停止");
  expect(vi.getTimerCount()).toBe(0);
  view.rerender(<AiRunProgress steps={[step]} live />);
  view.unmount();
  expect(vi.getTimerCount()).toBe(0);
});
it("does not invent progress for older generations", () => {
  const view = render(<AiRunProgress />);
  expect(view.container).toBeEmptyDOMElement();
});

it.each(["model_turn", "self_check"] as const)(
  "discloses %s history adjustment without claiming compaction or completion",
  (kind) => {
    const adjusted: Step = {
      sequence: 2,
      kind,
      turn_index: 2,
      status: "succeeded",
      started_at: step.started_at,
      completed_at: step.started_at,
      duration_ms: 0,
      trimmed_history_turns: 4,
    };
    const view = render(<AiRunProgress steps={[adjusted]} live />);
    expect(view.container).toHaveTextContent("已调整 4 轮较早对话");
    expect(view.container).toHaveTextContent("当前消息与已授权上下文保留");
    expect(view.container).toHaveTextContent("较早约束可能不完整");
    expect(view.container).toHaveTextContent("本地聊天记录未删除");
    view.rerender(
      <AiRunProgress
        steps={[{ ...adjusted, trimmed_history_turns: undefined }]}
      />,
    );
    expect(view.container).not.toHaveTextContent("已调整");
    expect(view.container).not.toHaveTextContent("本地聊天记录未删除");
  },
);

it.each(["model_turn", "self_check"] as const)(
  "discloses %s older read-result compaction without claiming a summary",
  (kind) => {
    const compacted: Step = {
      sequence: 2,
      kind,
      turn_index: 2,
      status: "succeeded",
      started_at: step.started_at,
      completed_at: step.started_at,
      duration_ms: 0,
      compacted_tool_results: 2,
      trimmed_history_turns: 1,
    };
    const view = render(<AiRunProgress steps={[compacted]} live />);
    expect(view.container).toHaveTextContent("已收起 2 条较早只读结果");
    expect(view.container).toHaveTextContent("并非摘要");
    expect(view.container).toHaveTextContent("必须按当前权限重新查询");
    expect(view.container).toHaveTextContent("最新工具结果与操作回执保留");
    expect(view.container).toHaveTextContent("较早约束可能不完整");
  },
);

it("labels submission reading as a query, not an approval", () => {
  render(
    <AiRunProgress
      steps={[{ ...step, tool_name: "workspace_task_submissions" }]}
      live
    />,
  );
  expect(screen.getByRole("status")).toHaveTextContent("查询提交与验收记录");
  expect(screen.getByRole("status")).not.toHaveTextContent("已验收");
});

it.each([
  ["workspace_today", "汇总今日工作台"],
  ["workspace_tasks", "按条件筛选任务"],
  ["workspace_projects", "查询项目组合"],
  ["workspace_guide", "读取工作台规则"],
  ["workspace_request_access", "请求下条消息的工作台权限（尚未授权）"],
  ["workspace_plan", "整理多步计划（不自动执行）"],
  ["workspace_task_views", "查询任务保存视图"],
  ["workspace_project_outputs", "查询项目产出与跟进"],
  ["workspace_agent_project_files", "核对跨任务已验收文件"],
  ["workspace_read_artifact_file", "校验并读取产出文件"],
  ["workspace_content_items", "查询内容排期与准备任务"],
  ["workspace_roadmap_milestones", "查询路线图里程碑"],
  ["workspace_inbox_source", "核对收件箱来源"],
  ["knowledge_library", "查询知识库元数据"],
] as const)(
  "labels %s instead of showing a generic tool call",
  (tool, text) => {
    render(<AiRunProgress steps={[{ ...step, tool_name: tool }]} live />);

    expect(screen.getByRole("status")).toHaveTextContent(text);
    expect(screen.getByRole("status")).not.toHaveTextContent("调用工具");
  },
);
