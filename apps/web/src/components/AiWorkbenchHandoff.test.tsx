import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  AiIssueHandoffCard,
  AiWorkbenchHandoffButton,
  AiWorkbenchHandoffCard,
} from "./AiWorkbenchHandoff";
import {
  useAiWorkbenchHandoff,
  workbenchHandoffDetails,
  type AiWorkbenchRecordType,
} from "../store/aiWorkbenchHandoff";
import type { AiWorkspaceScope } from "../types/models";
import { projectPortfolioHandoff } from "../lib/aiIssueHandoff";

const id = "33333333-3333-4333-8333-333333333333";
const records: [AiWorkbenchRecordType, string][] = [
  ["task", `/tasks/${id}`],
  ["project", `/projects/${id}`],
  ["client", `/clients/${id}`],
  ["inbox_item", `/inbox/${id}`],
  ["reminder", `/inbox?reminder=${id}`],
  ["content_item", `/content-calendar?item=${id}`],
  ["roadmap_milestone", `/roadmap?milestone=${id}`],
];

const handoffGuides: Record<AiWorkbenchRecordType, string> = {
  task: "workspace_guide(topic=tasks_projects)",
  project: "workspace_guide(topic=tasks_projects)",
  client: "workspace_guide(topic=people_clients)",
  inbox_item: "workspace_guide(topic=inbox)",
  reminder: "workspace_guide(topic=reminders)",
  content_item: "workspace_guide(topic=roadmap_content)",
  roadmap_milestone: "workspace_guide(topic=roadmap_content)",
};

afterEach(() => {
  cleanup();
  useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
});

describe("workbench to agent handoff", () => {
  it.each(records)(
    "hands off only the exact %s identity and waits for the human",
    (type, route) => {
      const prepare = vi.fn();
      const close = vi.fn();
      render(
        <MemoryRouter initialEntries={["/record"]}>
          <Routes>
            <Route
              path="/record"
              element={
                <AiWorkbenchHandoffButton
                  recordType={type}
                  recordId={id}
                  onNavigate={close}
                />
              }
            />
            <Route
              path="/ai"
              element={
                <AiWorkbenchHandoffCard disabled={false} onPrepare={prepare} />
              }
            />
          </Routes>
        </MemoryRouter>,
      );
      expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));
      expect(close).toHaveBeenCalledOnce();
      const pending = useAiWorkbenchHandoff.getState().pending!;
      expect(Object.keys(pending).sort()).toEqual(["id", "requestId", "type"]);
      expect(pending).toMatchObject({ type, id });
      expect(screen.getByRole("link")).toHaveAttribute("href", route);
      expect(prepare).not.toHaveBeenCalled();
      const detail = workbenchHandoffDetails(type, id)!;
      expect(detail.prompt).toContain(`type=${type}、id=${id}`);
      expect(detail.prompt).toContain(route);
      expect(detail.prompt).toContain(handoffGuides[type]);
      expect(detail.prompt).toContain("待我确认的");
      expect(detail.scopes).toEqual(
        type === "client"
          ? ["work", "clients", "actions"]
          : ["work", "actions"],
      );
      fireEvent.click(
        screen.getByRole("button", { name: "带入问题并选择权限" }),
      );
      expect(prepare).toHaveBeenCalledWith(pending);
      expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    },
  );

  it("keeps each handoff's domain boundary in the staged prompt", () => {
    expect(workbenchHandoffDetails("task", id)?.prompt).toContain(
      "任务提交、Agent Run 成功和人工验收不是同一件事",
    );
    expect(workbenchHandoffDetails("project", id)?.prompt).toContain(
      "不要修改任务、客户或财务事实",
    );
    expect(workbenchHandoffDetails("client", id)?.prompt).toContain(
      "不得发送外部消息、访问外部系统",
    );
    expect(workbenchHandoffDetails("inbox_item", id)?.prompt).toContain(
      "拆分不会自动运行 Agent，强制解决不是普通解决的自动降级",
    );
    expect(workbenchHandoffDetails("reminder", id)?.prompt).toContain(
      "已触发或已取消的提醒不能继续修改或取消",
    );
    expect(workbenchHandoffDetails("content_item", id)?.prompt).toContain(
      "不能访问、验证或声称已发布到外部平台",
    );
    expect(workbenchHandoffDetails("roadmap_milestone", id)?.prompt).toContain(
      "不要修改 Project/Task 状态、自动验收任务",
    );
  });

  it("rejects unknown types, non-canonical identities and stale dismissals", () => {
    const store = useAiWorkbenchHandoff.getState();
    expect(store.stage("task", id)).toBe(true);
    const first = useAiWorkbenchHandoff.getState().pending!;
    for (const bad of ["", "../secret", `${id}?scope=finance`, "not-a-uuid"]) {
      expect(store.stage("task", bad)).toBe(false);
      expect(useAiWorkbenchHandoff.getState().pending).toEqual(first);
    }
    expect(store.stage("terminal" as AiWorkbenchRecordType, id)).toBe(false);
    expect(store.stage("project", id)).toBe(true);
    store.dismiss(first.requestId);
    expect(useAiWorkbenchHandoff.getState().pending?.type).toBe("project");
  });

  it("carries a queue issue with its own prompt, route and scopes", () => {
    const prepare = vi.fn();
    const content = {
      label: "知识库索引失败",
      route: `/knowledge?source=${id}&job=${id}`,
      prompt: "请读取该来源与索引任务的真实状态，并给出下一步建议。",
      scopes: ["knowledge_actions" as const],
    };
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <Routes>
          <Route
            path="/ai"
            element={
              <AiIssueHandoffCard disabled={false} onPrepare={prepare} />
            }
          />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.queryByText(/让智能体处理/)).toBeNull();
    act(() => {
      expect(useAiWorkbenchHandoff.getState().stageIssue(content)).toBe(true);
    });
    expect(screen.getByText("让智能体处理：知识库索引失败")).toBeVisible();
    expect(screen.getByRole("link")).toHaveAttribute(
      "href",
      `/knowledge?source=${id}&job=${id}`,
    );
    fireEvent.click(screen.getByRole("button", { name: "带入问题并选择权限" }));
    expect(prepare).toHaveBeenCalledWith(
      expect.objectContaining({
        label: "知识库索引失败",
        prompt: content.prompt,
        scopes: ["knowledge_actions"],
      }),
    );
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  });

  it("carries a non-workspace issue without opening permission copy", () => {
    const prepare = vi.fn();
    const content = {
      label: "供应商配置待处理",
      route: "/ai?settings=ai",
      routeLabel: "打开 AI 助手设置",
      prompt: "请帮我排查这个供应商配置问题。",
      scopes: [],
    };
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <Routes>
          <Route
            path="/ai"
            element={
              <AiIssueHandoffCard disabled={false} onPrepare={prepare} />
            }
          />
        </Routes>
      </MemoryRouter>,
    );
    act(() => {
      expect(useAiWorkbenchHandoff.getState().stageIssue(content)).toBe(true);
    });
    expect(screen.getByText("让智能体处理：供应商配置待处理")).toBeVisible();
    expect(
      screen.getByRole("link", { name: "打开 AI 助手设置" }),
    ).toHaveAttribute("href", "/ai?settings=ai");
    expect(screen.getByText(/不会打开工作台权限/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "带入问题" }));
    expect(prepare).toHaveBeenCalledWith(
      expect.objectContaining({
        label: "供应商配置待处理",
        scopes: [],
      }),
    );
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  });

  it("makes an attached Git snapshot visible and waits for a later send", () => {
    const prepare = vi.fn();
    const content = {
      label: "Git 变更审查",
      route: "/ai?workspace=review",
      routeLabel: "查看 Git 变更审查",
      prompt: "请审查明确附带的 Git 变更快照。",
      scopes: [] as AiWorkspaceScope[],
      attachment: "git_review" as const,
    };
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiIssueHandoffCard disabled={false} onPrepare={prepare} />
      </MemoryRouter>,
    );
    act(() => {
      expect(useAiWorkbenchHandoff.getState().stageIssue(content)).toBe(true);
    });
    expect(screen.getByText(/不含本机路径/)).toBeVisible();
    expect(
      screen.getByText(/不授予 Git、文件、终端、浏览器或网络操作能力/),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "查看 Git 变更审查" }),
    ).toHaveAttribute("href", "/ai?workspace=review");
    fireEvent.click(screen.getByRole("button", { name: "带入 Git 变更" }));
    expect(prepare).toHaveBeenCalledWith(expect.objectContaining(content));
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  });

  it("makes an attached text preview visible and waits for a later send", () => {
    const prepare = vi.fn();
    const content = {
      label: "本地文本预览",
      route: "/ai?workspace=files",
      routeLabel: "查看本地文件预览",
      prompt: "请分析明确附带的文本预览。",
      scopes: [] as AiWorkspaceScope[],
      attachment: "file_preview" as const,
    };
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiIssueHandoffCard disabled={false} onPrepare={prepare} />
      </MemoryRouter>,
    );
    act(() => {
      expect(useAiWorkbenchHandoff.getState().stageIssue(content)).toBe(true);
    });
    expect(screen.getByText(/不含本机路径、目录授权或文件 ID/)).toBeVisible();
    expect(
      screen.getByText(/不授予文件、Git、终端、浏览器或网络操作能力/),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "查看本地文件预览" }),
    ).toHaveAttribute("href", "/ai?workspace=files");
    fireEvent.click(screen.getByRole("button", { name: "带入文本预览" }));
    expect(prepare).toHaveBeenCalledWith(expect.objectContaining(content));
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  });

  it("makes an attached terminal-output snapshot visible without granting terminal control", () => {
    const prepare = vi.fn();
    const content = {
      label: "终端输出",
      route: "/ai?workspace=terminal",
      routeLabel: "查看手动终端",
      prompt: "请分析明确附带的近期终端输出。",
      scopes: [] as AiWorkspaceScope[],
      attachment: "terminal_output" as const,
    };
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiIssueHandoffCard disabled={false} onPrepare={prepare} />
      </MemoryRouter>,
    );
    act(() => {
      expect(useAiWorkbenchHandoff.getState().stageIssue(content)).toBe(true);
    });
    expect(
      screen.getByText(/不含终端 ID、工作目录、路径或命令输入/),
    ).toBeVisible();
    expect(
      screen.getByText(/不授予终端、文件、Git、浏览器或网络操作能力/),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "查看手动终端" })).toHaveAttribute(
      "href",
      "/ai?workspace=terminal",
    );
    fireEvent.click(screen.getByRole("button", { name: "带入终端输出" }));
    expect(prepare).toHaveBeenCalledWith(expect.objectContaining(content));
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  });

  it("shows a sanitized browser address and waits for a later human send", () => {
    const prepare = vi.fn();
    const content = {
      label: "当前网页地址",
      route: "/ai?workspace=browser",
      routeLabel: "查看内嵌浏览器",
      prompt: "请分析明确附带的网页地址。",
      scopes: [] as AiWorkspaceScope[],
      attachment: "browser_address" as const,
      preview: "https://example.com/docs",
    };
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiIssueHandoffCard disabled={false} onPrepare={prepare} />
      </MemoryRouter>,
    );
    act(() => {
      expect(useAiWorkbenchHandoff.getState().stageIssue(content)).toBe(true);
    });
    expect(
      screen.getByText(/查询参数和片段已移除，含账号密码的地址不会交接/),
    ).toBeVisible();
    expect(
      screen.getByText(
        (_text, element) =>
          element?.classList.contains("ai-workbench-handoff-preview") ?? false,
      ),
    ).toBeVisible();
    expect(
      screen.getByText(/不授予浏览器、网络、文件、Git 或终端操作能力/),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "查看内嵌浏览器" }),
    ).toHaveAttribute("href", "/ai?workspace=browser");
    fireEvent.click(screen.getByRole("button", { name: "带入网页地址" }));
    expect(prepare).toHaveBeenCalledWith(expect.objectContaining(content));
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  });

  it("shows the explicitly captured page text preview and requires a separate manual send", () => {
    const prepare = vi.fn();
    const content = {
      label: "当前网页内容快照",
      route: "/ai?workspace=browser",
      routeLabel: "查看内嵌浏览器",
      prompt: "请分析明确附带的可见网页文本。",
      scopes: [] as AiWorkspaceScope[],
      attachment: "browser_page_text" as const,
      preview:
        "User-reviewed visible webpage text.\nDocs — https://example.com/",
      previewTruncated: true,
    };
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiIssueHandoffCard disabled={false} onPrepare={prepare} />
      </MemoryRouter>,
    );
    act(() => {
      expect(useAiWorkbenchHandoff.getState().stageIssue(content)).toBe(true);
    });
    expect(
      screen.getByText(
        (_text, element) =>
          element?.classList.contains("ai-workbench-handoff-preview") ?? false,
      ),
    ).toBeVisible();
    expect(
      screen.getByText(/你刚才明确从当前内嵌浏览器标签读取/),
    ).toBeVisible();
    expect(screen.getByText(/最多 20 条可见 HTTP\(S\) 链接/)).toBeVisible();
    expect(screen.getByText(/不可信网页数据/)).toBeVisible();
    expect(screen.getByText(/只有你随后手动发送/)).toBeVisible();
    expect(
      screen.getByText("网页正文或可见链接目录已达到安全上限。"),
    ).toBeVisible();
    expect(prepare).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "带入网页文本" }));
    expect(prepare).toHaveBeenCalledWith(expect.objectContaining(content));
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  });

  it("explains that a browser action receipt has no page access or new browser grant", () => {
    const prepare = vi.fn();
    const content = {
      label: "浏览器操作结果",
      route: "/ai?workspace=browser",
      prompt: "本机浏览器命令无错误返回，网页状态仍未知。",
      scopes: [] as AiWorkspaceScope[],
      attachment: "browser_action_result" as const,
    };
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiIssueHandoffCard disabled={false} onPrepare={prepare} />
      </MemoryRouter>,
    );
    act(() => {
      expect(useAiWorkbenchHandoff.getState().stageIssue(content)).toBe(true);
    });
    expect(screen.getByText(/不证明页面加载完毕/)).toBeVisible();
    expect(screen.getByText(/不授予新的浏览器或网络操作能力/)).toBeVisible();
    expect(prepare).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "带入操作结果" }));
    expect(prepare).toHaveBeenCalledWith(expect.objectContaining(content));
  });

  it("rejects incomplete queue issues and keeps one pending intent at a time", () => {
    const store = useAiWorkbenchHandoff.getState();
    const valid = {
      label: "自动化运行失败",
      route: "/settings/automation?run=" + id,
      prompt: "请读取运行元数据并给出建议",
      scopes: ["work", "actions"] as AiWorkspaceScope[],
    };
    for (const bad of [
      { ...valid, label: "  " },
      { ...valid, prompt: "" },
      { ...valid, route: "settings/automation" },
      { ...valid, route: "https://example.com" },
      { ...valid, route: "//example.com/settings/automation" },
      { ...valid, route: "/\\example.com/settings/automation" },
      { ...valid, route: "/settings/automation\n?run=" + id },
      { ...valid, route: "/settings/automation?run=%0a" },
      { ...valid, route: "/settings/automation?run=%5c" },
      { ...valid, preview: "\u0000" },
      { ...valid, preview: "a".repeat(2 * 1024 + 1) },
    ]) {
      expect(store.stageIssue(bad as typeof valid)).toBe(false);
      expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
    }
    expect(store.stageIssue(valid)).toBe(true);
    expect(useAiWorkbenchHandoff.getState().pendingIssue?.route).toBe(
      valid.route,
    );
    const first = useAiWorkbenchHandoff.getState().pendingIssue!;
    expect(store.stage("task", id)).toBe(true);
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
    store.dismissIssue(first.requestId);
    expect(useAiWorkbenchHandoff.getState().pending?.type).toBe("task");
  });

  it("accepts a canonical project search containing a literal backslash without opening a general route bypass", () => {
    const store = useAiWorkbenchHandoff.getState();
    const handoff = projectPortfolioHandoff({
      query: "path\\segment",
      status: "",
      clientId: "",
      page: 1,
    });
    expect(handoff?.route).toBe("/projects?q=path%5Csegment");
    expect(store.stageIssue(handoff!)).toBe(true);
    expect(useAiWorkbenchHandoff.getState().pendingIssue?.route).toBe(
      handoff?.route,
    );
    expect(
      store.stageIssue({
        ...handoff!,
        route: "/projects?q=path%5Csegment&extra=1",
      }),
    ).toBe(false);
    expect(
      store.stageIssue({ ...handoff!, route: "/projects%5Cescape?q=path" }),
    ).toBe(false);
  });

  it("does not leave a busy or edited record", () => {
    const close = vi.fn();
    render(
      <MemoryRouter>
        <AiWorkbenchHandoffButton
          recordType="task"
          recordId={id}
          disabled
          onNavigate={close}
        />
      </MemoryRouter>,
    );
    const button = screen.getByRole("button", { name: "交给智能体" });
    expect(button).toBeDisabled();
    fireEvent.click(button);
    expect(close).not.toHaveBeenCalled();
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
  });
});
