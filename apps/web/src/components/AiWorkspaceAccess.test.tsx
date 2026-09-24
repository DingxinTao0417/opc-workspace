import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AiProvider } from "../types/models";
import {
  AiWorkspaceAccess,
  AiWorkspaceGrantSummary,
} from "./AiWorkspaceAccess";

afterEach(cleanup);
describe("independent output file reading", () => {
  const provider = {
    id: "provider",
    name: "远程模型",
    model: "test-model",
    kind: "remote",
    version: 7,
  } as AiProvider;

  it("shows only the confirmed next-message scopes beside the composer", () => {
    const view = render(<AiWorkspaceGrantSummary />);
    expect(screen.queryByLabelText("本条工作台授权范围")).toBeNull();
    view.rerender(
      <AiWorkspaceGrantSummary
        value={{
          provider_version: 7,
          scopes: ["work", "outputs", "output_files"],
        }}
      />,
    );
    expect(screen.getByLabelText("本条工作台授权范围")).toHaveTextContent(
      "下一条消息已选 3 项：工作事项、执行产出、产出文件正文",
    );
    expect(screen.getByLabelText("本条工作台授权范围")).toHaveTextContent(
      "发送后不继承，操作仍需逐项确认",
    );
    view.rerender(<AiWorkspaceGrantSummary />);
    expect(screen.queryByLabelText("本条工作台授权范围")).toBeNull();
  });

  it("groups every capability and keeps quick selections uncommitted until confirmation", () => {
    const change = vi.fn();
    render(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    expect(screen.getAllByRole("checkbox")).toHaveLength(16);
    expect(screen.getByRole("group", { name: "常用查询与定位" })).toBeVisible();
    expect(screen.getByRole("group", { name: "操作建议与执行" })).toBeVisible();
    expect(screen.getByRole("group", { name: "敏感内容与文件" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "查询并定位" }));
    expect(change).not.toHaveBeenCalled();
    expect(screen.getByRole("checkbox", { name: /^工作事项/ })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /^工作区导航/ })).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: /^操作建议/ }),
    ).not.toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "准备操作建议" }));
    expect(screen.getByRole("checkbox", { name: /^操作建议/ })).toBeChecked();
    for (const name of [/^财务查询/, /^产出文件正文/, /停止 Agent 执行/]) {
      expect(screen.getByRole("checkbox", { name })).not.toBeChecked();
    }
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(change).toHaveBeenCalledWith({
      provider_version: 7,
      scopes: ["actions", "workspace_ui", "work"],
    });
  });

  it("keeps scope labels concise and expands full disclosure without granting access", () => {
    const change = vi.fn();
    render(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    const work = screen.getByRole("checkbox", {
      name: "工作事项",
    });
    expect(work).toHaveAccessibleDescription(/含项目笔记正文/);
    const disclosure = screen
      .getByText("工作事项：完整范围与风险")
      .closest("details")!;
    expect(disclosure).not.toHaveAttribute("open");
    fireEvent.click(work);
    expect(disclosure).toHaveAttribute("open");
    expect(
      screen.getByText(/任务、项目、收件箱、本地提醒、路线图、内容日历的名称/),
    ).toBeVisible();
    expect(change).not.toHaveBeenCalled();
    fireEvent.click(work);
    expect(disclosure).not.toHaveAttribute("open");
    expect(
      screen.getByRole("button", { name: "仅授权下一条消息" }),
    ).toBeDisabled();
  });

  it("replaces sensitive draft scopes without approving them and disables operation shortcuts in ephemeral chat", () => {
    const change = vi.fn();
    render(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        persist={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    fireEvent.click(screen.getByRole("checkbox", { name: /^产出文件正文/ }));
    expect(
      screen.getByRole("checkbox", { name: /^产出文件正文/ }),
    ).toBeChecked();
    expect(screen.getByRole("button", { name: "准备操作建议" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "只查工作事项" }));
    expect(
      screen.getByRole("checkbox", { name: /^产出文件正文/ }),
    ).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: /^工作事项/ })).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(change).toHaveBeenCalledWith({
      provider_version: 7,
      scopes: ["work"],
    });
  });

  it("requires an explicit choice even when a handoff recommends file reading", () => {
    const change = vi.fn();
    const view = render(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        persist={false}
        openRequest={0}
        recommendedScopes={["work", "outputs", "output_files"]}
        onChange={change}
      />,
    );
    view.rerender(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        persist={false}
        openRequest={1}
        recommendedScopes={["work", "outputs", "output_files"]}
        onChange={change}
      />,
    );
    const files = screen.getByRole("checkbox", {
      name: /^产出文件正文（独立只读授权）/,
    });
    expect(files).toBeEnabled();
    expect(files).not.toBeChecked();
    expect(screen.getByText(/智能体建议的产出文件正文/)).toBeVisible();
    expect(screen.getByRole("checkbox", { name: /^执行产出/ })).toBeChecked();
    expect(
      screen.getByText(/所读取的完整分页正文会发送给当前模型/),
    ).toBeVisible();
    fireEvent.click(files);
    expect(
      screen.queryByText(/文件权限不会随建议自动授予/),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("checkbox", { name: /^操作建议/ }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: /停止 Agent 执行（独立确认）/ }),
    ).not.toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(change).toHaveBeenCalledWith({
      provider_version: 7,
      scopes: ["work", "outputs", "output_files"],
    });
    view.rerender(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        persist={false}
        value={{
          provider_version: 7,
          scopes: ["work", "outputs", "output_files"],
        }}
        onChange={change}
      />,
    );
    expect(
      screen.getByRole("button", { name: "本次可读产出文件" }),
    ).toBeVisible();
  });

  it.each([/^工作事项/, /^执行产出/])(
    "revokes file reading when a read dependency is removed: %s",
    (name) => {
      render(
        <AiWorkspaceAccess
          provider={provider}
          disabled={false}
          onChange={vi.fn()}
        />,
      );
      fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
      const files = screen.getByRole("checkbox", {
        name: /^产出文件正文（独立只读授权）/,
      });
      fireEvent.click(files);
      expect(screen.getByRole("checkbox", { name: /^工作事项/ })).toBeChecked();
      expect(screen.getByRole("checkbox", { name: /^执行产出/ })).toBeChecked();
      fireEvent.click(screen.getByRole("checkbox", { name }));
      expect(files).not.toBeChecked();
    },
  );

  it("keeps file reading independent from action and execution approval", () => {
    render(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        onChange={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    const files = screen.getByRole("checkbox", {
      name: /^产出文件正文（独立只读授权）/,
    });
    fireEvent.click(
      screen.getByRole("checkbox", { name: /Agent 受控文件（再次确认）/ }),
    );
    expect(files).not.toBeChecked();
    fireEvent.click(files);
    fireEvent.click(screen.getByRole("checkbox", { name: /^操作建议/ }));
    expect(files).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: /Agent 受控文件（再次确认）/ }),
    ).not.toBeChecked();
  });
});

describe("independent per-message finance consent", () => {
  it("allows UI-only workspace navigation in a non-persistent chat without adding a data scope", () => {
    const change = vi.fn();
    const view = render(
      <AiWorkspaceAccess
        provider={
          {
            name: "本地模型",
            model: "test",
            kind: "local",
            version: 7,
          } as AiProvider
        }
        disabled={false}
        persist={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    const navigation = screen.getByRole("checkbox", {
      name: /^工作区导航（不读取内容）/,
    });
    expect(navigation).toBeEnabled();
    fireEvent.click(navigation);
    expect(screen.getByText(/不会查询或发送任何工作台内容/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(change).toHaveBeenCalledWith({
      provider_version: 7,
      scopes: ["workspace_ui"],
    });
    view.rerender(
      <AiWorkspaceAccess
        provider={
          {
            name: "本地模型",
            model: "test",
            kind: "local",
            version: 7,
          } as AiProvider
        }
        disabled={false}
        persist={false}
        value={{ provider_version: 7, scopes: ["workspace_ui"] }}
        onChange={change}
      />,
    );
    expect(
      screen.getByRole("button", { name: "本次可开工作区" }),
    ).toBeVisible();
  });

  it("allows a browser-navigation request in a non-persistent chat and labels its limited capability", () => {
    const change = vi.fn();
    const view = render(
      <AiWorkspaceAccess
        provider={
          {
            name: "本地模型",
            model: "test",
            kind: "local",
            version: 7,
          } as AiProvider
        }
        disabled={false}
        persist={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /^浏览器标签操作（逐次确认）/,
      }),
    );
    expect(screen.getByText(/由你逐次确认后才操作/)).toBeVisible();
    expect(
      screen.getByText(
        /网页内容、浏览器状态、操作结果和本机路径不会发送给模型/,
      ),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(change).toHaveBeenCalledWith({
      provider_version: 7,
      scopes: ["workspace_browser"],
    });
    view.rerender(
      <AiWorkspaceAccess
        provider={
          {
            name: "本地模型",
            model: "test",
            kind: "local",
            version: 7,
          } as AiProvider
        }
        disabled={false}
        persist={false}
        value={{ provider_version: 7, scopes: ["workspace_browser"] }}
        onChange={change}
      />,
    );
    expect(
      screen.getByRole("button", { name: "本次可请求浏览器操作" }),
    ).toBeVisible();
  });

  it("discloses that record navigation still needs a local confirmation when UI and work access are combined", () => {
    const change = vi.fn();
    render(
      <AiWorkspaceAccess
        provider={
          {
            name: "本地模型",
            model: "test",
            kind: "local",
            version: 7,
          } as AiProvider
        }
        disabled={false}
        value={{ provider_version: 7, scopes: ["work", "workspace_ui"] }}
        onChange={change}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "本次可查工作台" }));

    expect(
      screen.getByText(/请求你确认后在本地打开一条已获授权的记录/),
    ).toBeVisible();
    expect(
      screen.getByText(
        /仅在已授予范围内核对收件箱来源的身份、版本、状态和本地详情地址/,
      ),
    ).toBeVisible();
    expect(
      screen.getByText(
        /未授予对应范围时只提示所需权限，不披露受保护来源的 ID、版本或地址/,
      ),
    ).toBeVisible();
    for (const name of [/^执行产出/, /^客户资料/, /^财务查询/]) {
      expect(screen.getByRole("checkbox", { name })).not.toBeChecked();
    }
    expect(change).not.toHaveBeenCalled();
  });

  it("discloses server-verified Agent Run navigation only when outputs are included", () => {
    const change = vi.fn();
    render(
      <AiWorkspaceAccess
        provider={
          {
            name: "本地模型",
            model: "test",
            kind: "local",
            version: 7,
          } as AiProvider
        }
        disabled={false}
        value={{
          provider_version: 7,
          scopes: ["work", "outputs", "workspace_ui"],
        }}
        onChange={change}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "本次可查工作台" }));

    expect(
      screen.getByText(
        /记录、Agent 执行或任务提交批次；后两者的所属任务均由服务端核对/,
      ),
    ).toBeVisible();
    expect(
      screen.getByText(
        /也可按项目汇总任务产出、关联跟进事项及必需任务进度；不读取项目附件正文/,
      ),
    ).toBeVisible();
  });

  it("opens an explicit plan-continuation consent with work and actions preselected", () => {
    const change = vi.fn();
    const provider = {
      id: "provider",
      name: "远程模型",
      model: "test-model",
      kind: "remote",
      version: 7,
    } as AiProvider;
    const view = render(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        onChange={change}
        openRequest={0}
        recommendedScopes={["work", "actions"]}
      />,
    );

    view.rerender(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        onChange={change}
        openRequest={1}
        recommendedScopes={["work", "actions"]}
      />,
    );

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: /^工作事项/ })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /^操作建议/ })).toBeChecked();
    expect(change).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(change).toHaveBeenCalledWith({
      provider_version: 7,
      scopes: ["actions", "work"],
    });
  });

  it("requires work, outputs and actions for one-message Agent execution and disables it in ephemeral chats", () => {
    const change = vi.fn();
    const provider = {
      id: "provider",
      name: "远程模型",
      model: "test-model",
      kind: "remote",
      version: 7,
    } as AiProvider;
    const view = render(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    const execution = screen.getByRole("checkbox", {
      name: /停止 Agent 执行（独立确认）/,
    });
    fireEvent.click(execution);
    expect(screen.getByRole("checkbox", { name: /^工作事项/ })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /^执行产出/ })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /^操作建议/ })).toBeChecked();
    expect(screen.getAllByText(/远程 Provider/).length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("checkbox", { name: /^执行产出/ }));
    expect(execution).not.toBeChecked();
    fireEvent.click(execution);
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(change).toHaveBeenCalledWith({
      provider_version: 7,
      scopes: ["actions", "agent_execution", "work", "outputs"],
    });

    view.unmount();
    render(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        persist={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    expect(
      screen.getByRole("checkbox", {
        name: /停止 Agent 执行（独立确认）/,
      }),
    ).toBeDisabled();
    expect(screen.getByText(/操作建议与多步计划暂不可用/)).toBeVisible();
    expect(screen.getByText(/Agent 执行暂不可用/)).toBeVisible();
  });
  it("keeps controlled Agent files one-message-only and coupled to every execution dependency", () => {
    const change = vi.fn();
    const provider = {
      id: "provider",
      name: "远程模型",
      model: "test-model",
      kind: "remote",
      version: 7,
    } as AiProvider;
    const view = render(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    const files = screen.getByRole("checkbox", {
      name: /Agent 受控文件（再次确认）/,
    });
    fireEvent.click(files);
    for (const name of [
      /^工作事项/,
      /^执行产出/,
      /^操作建议/,
      /停止 Agent 执行（独立确认）/,
    ]) {
      expect(screen.getByRole("checkbox", { name })).toBeChecked();
    }
    expect(screen.getByText(/不得到真实文件 ID、路径或正文/)).toBeVisible();

    fireEvent.click(
      screen.getByRole("checkbox", { name: /停止 Agent 执行（独立确认）/ }),
    );
    expect(files).not.toBeChecked();
    fireEvent.click(files);
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(change).toHaveBeenCalledWith({
      provider_version: 7,
      scopes: ["actions", "agent_execution", "agent_files", "work", "outputs"],
    });

    view.unmount();
    render(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        persist={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    expect(
      screen.getByRole("checkbox", {
        name: /Agent 受控文件（再次确认）/,
      }),
    ).toBeDisabled();
    expect(screen.getByText(/受控文件暂不可用/)).toBeVisible();
  });
  it("requires a separate project task file scope and removes it with every dependency", () => {
    const change = vi.fn();
    render(
      <AiWorkspaceAccess
        provider={
          {
            id: "provider",
            name: "远程模型",
            model: "test",
            kind: "remote",
            version: 7,
          } as AiProvider
        }
        disabled={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    const projectFiles = screen.getByRole("checkbox", {
      name: /Agent 跨任务已验收文件/,
    });
    expect(projectFiles).not.toBeChecked();
    fireEvent.click(projectFiles);
    for (const name of [
      /^工作事项/,
      /^执行产出/,
      /^操作建议/,
      /停止 Agent 执行（独立确认）/,
      /Agent 受控文件（再次确认）/,
    ]) {
      const dependency = screen.getByRole("checkbox", { name });
      expect(dependency).toBeChecked();
      fireEvent.click(dependency);
      expect(projectFiles).not.toBeChecked();
      fireEvent.click(projectFiles);
    }
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(change.mock.calls[0][0].scopes).toContain("agent_project_files");
  });
  it("never preselects the new project file scope from a recommendation and disables it in temporary chats", () => {
    const provider = {
      id: "provider",
      name: "远程模型",
      model: "test",
      kind: "remote",
      version: 7,
    } as AiProvider;
    const view = render(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        onChange={vi.fn()}
        openRequest={0}
        recommendedScopes={["agent_project_files"]}
      />,
    );
    view.rerender(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        onChange={vi.fn()}
        openRequest={1}
        recommendedScopes={["agent_project_files"]}
      />,
    );
    expect(
      screen.getByRole("checkbox", { name: /Agent 跨任务已验收文件/ }),
    ).not.toBeChecked();
    for (const name of [
      /^工作事项/,
      /^执行产出/,
      /^操作建议/,
      /停止 Agent 执行（独立确认）/,
      /Agent 受控文件（再次确认）/,
    ]) {
      expect(screen.getByRole("checkbox", { name })).not.toBeChecked();
    }
    expect(screen.getByRole("note")).toHaveTextContent(
      "Agent 跨任务已验收文件",
    );
    expect(
      screen.getByRole("button", { name: "仅授权下一条消息" }),
    ).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "不授权" }));
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    expect(
      screen.queryByText(/文件权限不会随建议自动授予/),
    ).not.toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /Agent 跨任务已验收文件/ }),
    );
    expect(screen.queryByRole("note")).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "仅授权下一条消息" }),
    ).toBeEnabled();
    view.unmount();
    render(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        persist={false}
        onChange={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    expect(
      screen.getByRole("checkbox", { name: /Agent 跨任务已验收文件/ }),
    ).toBeDisabled();
  });
  it("requires independent export consent and drops it with finance", () => {
    const change = vi.fn();
    const view = render(
      <AiWorkspaceAccess
        provider={
          {
            name: "模型",
            model: "test",
            kind: "local",
            version: 7,
          } as AiProvider
        }
        disabled={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    const exports = screen.getByRole("checkbox", {
        name: /财务导出（独立确认）/,
      }),
      finance = screen.getByRole("checkbox", { name: /财务查询（只读）/ });
    fireEvent.click(exports);
    expect(finance).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: /财务操作（独立确认）/ }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: /发票操作（独立确认）/ }),
    ).not.toBeChecked();
    fireEvent.click(finance);
    expect(exports).not.toBeChecked();
    fireEvent.click(exports);
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(change).toHaveBeenCalledWith({
      provider_version: 7,
      scopes: ["finance_exports", "finance"],
    });
    view.unmount();
    render(
      <AiWorkspaceAccess
        provider={
          {
            name: "模型",
            model: "test",
            kind: "local",
            version: 7,
          } as AiProvider
        }
        disabled={false}
        persist={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    expect(
      screen.getByRole("checkbox", { name: /财务导出（独立确认）/ }),
    ).toBeDisabled();
  });
  it("keeps knowledge library management independent from content search and unavailable in ephemeral chats", () => {
    const change = vi.fn();
    const view = render(
      <AiWorkspaceAccess
        provider={
          {
            name: "模型",
            model: "test",
            kind: "remote",
            version: 7,
          } as AiProvider
        }
        disabled={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    const management = screen.getByRole("checkbox", {
      name: /^知识库管理（逐项确认）/,
    });
    expect(
      screen.getByRole("checkbox", { name: /^知识检索与引用/ }),
    ).not.toBeChecked();
    fireEvent.click(management);
    expect(
      screen.getByRole("checkbox", { name: /^知识检索与引用/ }),
    ).not.toBeChecked();
    expect(
      screen.getByText(/存为新的 \.txt\/\.md\/\.markdown 知识来源/),
    ).toBeVisible();
    expect(screen.getByText(/不会扫描本地文件或读取路径/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(change).toHaveBeenCalledWith({
      provider_version: 7,
      scopes: ["knowledge_actions"],
    });
    view.rerender(
      <AiWorkspaceAccess
        provider={
          {
            name: "模型",
            model: "test",
            kind: "remote",
            version: 7,
          } as AiProvider
        }
        value={{ provider_version: 7, scopes: ["knowledge_actions"] }}
        disabled={false}
        onChange={change}
      />,
    );
    expect(
      screen.getByRole("button", { name: "本次可提议操作" }),
    ).toBeVisible();
    view.unmount();
    render(
      <AiWorkspaceAccess
        provider={
          {
            name: "模型",
            model: "test",
            kind: "remote",
            version: 7,
          } as AiProvider
        }
        disabled={false}
        persist={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    expect(
      screen.getByRole("checkbox", { name: /^知识库管理（逐项确认）/ }),
    ).toBeDisabled();
  });
  it("keeps invoice consent independent of ledger and other writes", () => {
    const change = vi.fn();
    render(
      <AiWorkspaceAccess
        provider={
          {
            name: "模型",
            model: "test",
            kind: "local",
            version: 7,
          } as AiProvider
        }
        disabled={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    const invoice = screen.getByRole("checkbox", {
      name: /发票操作（独立确认）/,
    });
    const finance = screen.getByRole("checkbox", { name: /财务查询（只读）/ });
    fireEvent.click(invoice);
    expect(finance).toBeChecked();
    expect(screen.getByText(/CSV 导出需另选“财务导出”/)).toBeVisible();
    expect(
      screen.getByRole("checkbox", { name: /财务操作（独立确认）/ }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: /^操作建议/ }),
    ).not.toBeChecked();
    fireEvent.click(finance);
    expect(invoice).not.toBeChecked();
    fireEvent.click(invoice);
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(change).toHaveBeenCalledWith({
      provider_version: 7,
      scopes: ["invoice_actions", "finance"],
    });
  });
  it("makes financial write consent independent and removes dependent consent on deselection", () => {
    const change = vi.fn();
    render(
      <AiWorkspaceAccess
        provider={
          {
            name: "模型",
            model: "test",
            kind: "local",
            version: 7,
          } as AiProvider
        }
        disabled={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    const finance = screen.getByRole("checkbox", { name: /财务查询（只读）/ });
    const actions = screen.getByRole("checkbox", {
      name: /财务操作（独立确认）/,
    });
    fireEvent.click(actions);
    expect(finance).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: /^工作事项/ }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: /^操作建议/ }),
    ).not.toBeChecked();
    fireEvent.click(finance);
    expect(actions).not.toBeChecked();
    fireEvent.click(actions);
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(change).toHaveBeenCalledWith({
      provider_version: 7,
      scopes: ["finance_actions", "finance"],
    });
  });
  it.each(["remote", "local"] as const)(
    "allows explicit finance-only reads for a nonpersistent %s conversation",
    (kind) => {
      const change = vi.fn();
      render(
        <AiWorkspaceAccess
          provider={
            {
              id: "test",
              name: "测试供应商",
              model: "test",
              kind,
              version: 7,
            } as AiProvider
          }
          disabled={false}
          persist={false}
          onChange={change}
        />,
      );
      fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
      expect(
        screen.getByRole("checkbox", { name: /财务查询（只读）/ }),
      ).not.toBeChecked();
      expect(
        screen.getByRole("checkbox", { name: /操作建议（逐项确认）/ }),
      ).toBeDisabled();
      expect(
        screen.getByRole("checkbox", { name: /财务操作（独立确认）/ }),
      ).toBeDisabled();
      expect(
        screen.getByRole("checkbox", { name: /发票操作（独立确认）/ }),
      ).toBeDisabled();
      expect(
        screen.getByText(
          kind === "remote"
            ? /查询结果会发送给以上远程供应商/
            : /查询结果将发送到所选本机模型端点/,
        ),
      ).toBeVisible();
      expect(
        screen.getByRole("checkbox", { name: "财务查询（只读）" }),
      ).toHaveAccessibleDescription(/另选操作建议也不会开放财务写入/);
      fireEvent.click(
        screen.getByRole("checkbox", { name: /财务查询（只读）/ }),
      );
      expect(
        screen.getByRole("checkbox", { name: /^工作事项/ }),
      ).not.toBeChecked();
      expect(
        screen.getByRole("checkbox", { name: /^客户资料/ }),
      ).not.toBeChecked();
      fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
      expect(change).toHaveBeenCalledWith({
        provider_version: 7,
        scopes: ["finance"],
      });
      fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
      expect(
        screen.getByRole("checkbox", { name: /财务查询（只读）/ }),
      ).not.toBeChecked();
      fireEvent.click(screen.getByRole("button", { name: "不授权" }));
      expect(change).toHaveBeenLastCalledWith(undefined);
    },
  );
});
