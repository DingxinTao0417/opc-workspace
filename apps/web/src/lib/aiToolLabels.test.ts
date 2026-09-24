import { describe, expect, it } from "vitest";
import { aiToolLabel } from "./aiToolLabels";

describe("aiToolLabel", () => {
  it.each([
    ["workspace_open_panel", "请求打开右侧工作区标签（仅本地布局）"],
    ["workspace_request_record_navigation", "请求定位工作台记录（待你确认）"],
    ["workspace_request_browser_navigation", "请求打开网页标签（待你确认）"],
    ["workspace_request_browser_action", "请求操作当前网页标签（待你确认）"],
    ["workspace_delegate_agent", "准备子智能体委派（仍需确认）"],
    ["workspace_agent_followup", "准备子智能体续交办（仍需确认）"],
    ["workspace_agent_children", "查看子智能体状态与回复"],
  ])("labels %s", (name, label) => {
    expect(aiToolLabel(name)).toBe(label);
  });

  it("never invents a label for an unknown or missing tool", () => {
    expect(aiToolLabel("private-name")).toBeNull();
    expect(aiToolLabel(null)).toBeNull();
    expect(aiToolLabel(undefined)).toBeNull();
  });
});
