import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it } from "vitest";
import { parseAiActionProposal } from "../api/aiWorkspaceActions";
import { AiAutomationRetryDetails } from "./AiAutomationRetryDetails";

afterEach(cleanup);
it("distinguishes retrying notification delivery from rerunning the Agent", () => {
  const preview = {
    rule_id: "00000000-0000-5000-8000-000000000105",
    run_id: "00000000-0000-4000-8000-000000000003",
    rule_version: 1,
    current_rule_version: 1,
    rule_enabled: false,
    attempt: 1,
    next_attempt: 2,
    trigger_type: "event",
    action_type: "inbox_item",
    error_code: "ACTION_WRITE_FAILED",
    scheduled_for: null,
    permissions: ["创建本地诊断事项"],
    config: { priority: "P1" },
    action: {
      action_type: "inbox_item",
      agent_run_id: "00000000-0000-4000-8000-000000000004",
      task_id: "00000000-0000-4000-8000-000000000005",
      attempt: 7,
      error_code: "AGENT_MODEL_FAILED",
      failed_at: "2026-09-21T12:00:00.000000000Z",
      priority: "P1",
    },
  };
  render(
    <MemoryRouter>
      <AiAutomationRetryDetails
        proposal={parseAiActionProposal({
          id: "00000000-0000-4000-8000-000000000001",
          generation_id: "00000000-0000-4000-8000-000000000002",
          fingerprint: "a".repeat(64),
          action: {
            action: "automation.retry",
            automation_run_id: preview.run_id,
            expected_version: 1,
            changes: {},
          },
          preview: {
            label: "重试失败诊断通知",
            before: {},
            after: {},
            automation_retry: preview,
          },
          status: "pending",
          can_confirm: true,
          result_id: null,
          result_version: null,
          created_at: "2026-09-21T12:00:00Z",
          decided_at: null,
        })}
      />
    </MemoryRouter>,
  );
  expect(
    screen.getByText(
      "这里只重试失败诊断通知，不重跑 Agent、不调用模型、不恢复产出登记。",
    ),
  ).toBeVisible();
  expect(screen.getByText("Agent 执行次数")).toBeVisible();
  expect(screen.getByText("7")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: /执行|重试|恢复/ }),
  ).not.toBeInTheDocument();
});
