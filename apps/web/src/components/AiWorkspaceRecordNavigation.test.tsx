import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import { useUiStore } from "../store/ui";
import { useSettingsStore } from "../store/settings";
import { useWorkspacePanels } from "../store/workspacePanels";
import { AiWorkspaceRecordNavigation } from "./AiWorkspaceRecordNavigation";

function LocationProbe() {
  const location = useLocation();
  return (
    <output data-testid="location">{`${location.pathname}${location.search}`}</output>
  );
}

afterEach(() => {
  cleanup();
  useUiStore.setState({
    workspaceRecordNavigationRequest: null,
    rightOverviewCollapsed: false,
    rightPanelTab: "overview",
    rightPanelRequestMode: "activate",
    rightPanelRequest: 0,
  });
  useWorkspacePanels.setState({
    tabs: [],
    activeId: "",
    splitId: null,
    handledRequest: "",
  });
  useSettingsStore.setState({ showRightOverview: true, preview: null });
});

describe("AI workspace record navigation", () => {
  it("requires confirmation before opening a saved view with a return path", () => {
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiWorkspaceRecordNavigation sessionId="018f0000-0000-7000-8000-000000000002" />
        <LocationProbe />
      </MemoryRouter>,
    );
    act(() =>
      useUiStore
        .getState()
        .requestWorkspaceRecordNavigation(
          "task_saved_view",
          "018f0000-0000-7000-8000-000000000001",
        ),
    );
    expect(screen.getByTestId("location")).toHaveTextContent("/ai");
    expect(
      screen.getByText(/重新读取并应用当前保存的筛选条件/),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "打开任务保存视图" }));
    expect(screen.getByTestId("location")).toHaveTextContent(
      "/tasks?task_view=018f0000-0000-7000-8000-000000000001&return_session=018f0000-0000-7000-8000-000000000002",
    );
  });
  it("keeps the chat in place until the user confirms and preserves its return session", () => {
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiWorkspaceRecordNavigation sessionId="018f0000-0000-7000-8000-000000000002" />
        <LocationProbe />
      </MemoryRouter>,
    );
    act(() =>
      useUiStore
        .getState()
        .requestWorkspaceRecordNavigation(
          "task",
          "018f0000-0000-7000-8000-000000000001",
        ),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("智能体建议打开任务");
    expect(screen.getByTestId("location")).toHaveTextContent("/ai");
    fireEvent.click(screen.getByRole("button", { name: "打开任务" }));
    expect(screen.getByTestId("location")).toHaveTextContent(
      "/tasks/018f0000-0000-7000-8000-000000000001?return_session=018f0000-0000-7000-8000-000000000002",
    );
    expect(useUiStore.getState().workspaceRecordNavigationRequest).toBeNull();
  });

  it("lets the user stay in chat without navigating", () => {
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiWorkspaceRecordNavigation sessionId={null} />
        <LocationProbe />
      </MemoryRouter>,
    );
    act(() =>
      useUiStore
        .getState()
        .requestWorkspaceRecordNavigation(
          "client",
          "018f0000-0000-7000-8000-000000000003",
        ),
    );
    fireEvent.click(screen.getByRole("button", { name: "留在对话" }));
    expect(screen.getByTestId("location")).toHaveTextContent("/ai");
    expect(useUiStore.getState().workspaceRecordNavigationRequest).toBeNull();
  });

  it("opens the exact Agent Run using only the Sidecar-derived Task identity", () => {
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiWorkspaceRecordNavigation sessionId={null} />
        <LocationProbe />
      </MemoryRouter>,
    );
    act(() =>
      useUiStore
        .getState()
        .requestWorkspaceRecordNavigation(
          "agent_run",
          "018f0000-0000-7000-8000-000000000011",
          "018f0000-0000-7000-8000-000000000012",
        ),
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "智能体建议打开Agent 执行",
    );
    fireEvent.click(screen.getByRole("button", { name: "打开Agent 执行" }));
    expect(screen.getByTestId("location")).toHaveTextContent(
      "/tasks/018f0000-0000-7000-8000-000000000012?agent_run=018f0000-0000-7000-8000-000000000011",
    );
  });

  it("opens the exact Task Submission using only the Sidecar-derived Task identity", () => {
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiWorkspaceRecordNavigation sessionId="018f0000-0000-7000-8000-000000000013" />
        <LocationProbe />
      </MemoryRouter>,
    );
    act(() =>
      useUiStore
        .getState()
        .requestWorkspaceRecordNavigation(
          "task_submission",
          "018f0000-0000-7000-8000-000000000014",
          "018f0000-0000-7000-8000-000000000015",
        ),
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "智能体建议打开提交批次",
    );
    fireEvent.click(screen.getByRole("button", { name: "打开提交批次" }));
    expect(screen.getByTestId("location")).toHaveTextContent(
      "/tasks/018f0000-0000-7000-8000-000000000015/submissions/018f0000-0000-7000-8000-000000000014?return_session=018f0000-0000-7000-8000-000000000013",
    );
  });

  it("opens the exact Task Artifact only after confirmation", () => {
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiWorkspaceRecordNavigation sessionId="018f0000-0000-7000-8000-000000000044" />
        <LocationProbe />
      </MemoryRouter>,
    );
    act(() =>
      useUiStore
        .getState()
        .requestWorkspaceRecordNavigation(
          "task_artifact",
          "018f0000-0000-7000-8000-000000000041",
          "018f0000-0000-7000-8000-000000000042",
          undefined,
          "018f0000-0000-7000-8000-000000000043",
        ),
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "智能体建议打开任务产出",
    );
    expect(screen.getByTestId("location")).toHaveTextContent("/ai");
    fireEvent.click(screen.getByRole("button", { name: "打开完整详情" }));
    expect(screen.getByTestId("location")).toHaveTextContent(
      "/tasks/018f0000-0000-7000-8000-000000000042/submissions/018f0000-0000-7000-8000-000000000043?artifact=018f0000-0000-7000-8000-000000000041&return_session=018f0000-0000-7000-8000-000000000044",
    );
  });

  it("previews the exact artifact in a right-side tab without leaving chat", () => {
    useSettingsStore.setState({ showRightOverview: true, preview: null });
    useUiStore.setState({
      rightOverviewCollapsed: true,
      rightPanelTab: "browser",
      rightPanelRequestMode: "activate",
      rightPanelRequest: 7,
    });
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiWorkspaceRecordNavigation sessionId="018f0000-0000-7000-8000-000000000044" />
        <LocationProbe />
      </MemoryRouter>,
    );
    const request = () =>
      useUiStore
        .getState()
        .requestWorkspaceRecordNavigation(
          "task_artifact",
          "018f0000-0000-7000-8000-000000000041",
          "018f0000-0000-7000-8000-000000000042",
          undefined,
          "018f0000-0000-7000-8000-000000000043",
        );
    act(request);
    fireEvent.click(screen.getByRole("button", { name: "右侧预览" }));
    expect(screen.getByTestId("location")).toHaveTextContent("/ai");
    expect(useUiStore.getState().rightOverviewCollapsed).toBe(false);
    expect(useUiStore.getState().workspaceRecordNavigationRequest).toBeNull();
    expect(useWorkspacePanels.getState().tabs).toEqual([
      expect.objectContaining({
        id: "task-artifact:018f0000-0000-7000-8000-000000000041",
        kind: "artifact",
        artifactId: "018f0000-0000-7000-8000-000000000041",
        artifactTaskId: "018f0000-0000-7000-8000-000000000042",
        artifactSubmissionId: "018f0000-0000-7000-8000-000000000043",
        returnSession: "018f0000-0000-7000-8000-000000000044",
      }),
    ]);
    expect(useWorkspacePanels.getState().handledRequest).toBe(
      "browser:activate:7",
    );
    act(request);
    fireEvent.click(screen.getByRole("button", { name: "右侧预览" }));
    expect(useWorkspacePanels.getState().tabs).toHaveLength(1);
  });

  it("keeps confirmation available when the right-side tab limit is reached", () => {
    useSettingsStore.setState({ showRightOverview: true, preview: null });
    useWorkspacePanels.setState({
      tabs: Array.from({ length: 24 }, (_, index) => ({
        id: `existing-${index}`,
        kind: "overview" as const,
        title: `现有标签 ${index}`,
      })),
      activeId: "existing-0",
    });
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiWorkspaceRecordNavigation sessionId={null} />
        <LocationProbe />
      </MemoryRouter>,
    );
    act(() =>
      useUiStore
        .getState()
        .requestWorkspaceRecordNavigation(
          "task_artifact",
          "018f0000-0000-7000-8000-000000000041",
          "018f0000-0000-7000-8000-000000000042",
          undefined,
          "018f0000-0000-7000-8000-000000000043",
        ),
    );
    fireEvent.click(screen.getByRole("button", { name: "右侧预览" }));
    expect(screen.getByText(/最多打开 24 个工具标签/)).toBeVisible();
    expect(
      useUiStore.getState().workspaceRecordNavigationRequest,
    ).not.toBeNull();
    expect(screen.getByTestId("location")).toHaveTextContent("/ai");
  });

  it("keeps full detail available when the right-side workspace is disabled", () => {
    useSettingsStore.setState({ showRightOverview: false, preview: null });
    render(
      <MemoryRouter initialEntries={["/ai"]}>
        <AiWorkspaceRecordNavigation sessionId={null} />
        <LocationProbe />
      </MemoryRouter>,
    );
    act(() =>
      useUiStore
        .getState()
        .requestWorkspaceRecordNavigation(
          "task_artifact",
          "018f0000-0000-7000-8000-000000000041",
          "018f0000-0000-7000-8000-000000000042",
          undefined,
          "018f0000-0000-7000-8000-000000000043",
        ),
    );
    expect(screen.queryByRole("button", { name: "右侧预览" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "打开完整详情" }));
    expect(screen.getByTestId("location")).toHaveTextContent(
      "/tasks/018f0000-0000-7000-8000-000000000042/submissions/018f0000-0000-7000-8000-000000000043?artifact=018f0000-0000-7000-8000-000000000041",
    );
  });

  it.each([
    [
      "project_note",
      "项目笔记",
      "/projects/018f0000-0000-7000-8000-000000000032?note=018f0000-0000-7000-8000-000000000031&return_session=018f0000-0000-7000-8000-000000000033",
    ],
    [
      "client_activity",
      "客户活动",
      "/clients/018f0000-0000-7000-8000-000000000032?activity=018f0000-0000-7000-8000-000000000031&return_session=018f0000-0000-7000-8000-000000000033",
    ],
    [
      "client_followup",
      "客户回访",
      "/clients/018f0000-0000-7000-8000-000000000032?followup=018f0000-0000-7000-8000-000000000031&return_session=018f0000-0000-7000-8000-000000000033",
    ],
  ] as const)(
    "opens exact %s with the Sidecar-derived parent only after confirmation",
    (recordType, label, expectedRoute) => {
      render(
        <MemoryRouter initialEntries={["/ai"]}>
          <AiWorkspaceRecordNavigation sessionId="018f0000-0000-7000-8000-000000000033" />
          <LocationProbe />
        </MemoryRouter>,
      );
      act(() =>
        useUiStore
          .getState()
          .requestWorkspaceRecordNavigation(
            recordType,
            "018f0000-0000-7000-8000-000000000031",
            undefined,
            "018f0000-0000-7000-8000-000000000032",
          ),
      );
      expect(screen.getByTestId("location")).toHaveTextContent("/ai");
      fireEvent.click(screen.getByRole("button", { name: `打开${label}` }));
      expect(screen.getByTestId("location")).toHaveTextContent(expectedRoute);
    },
  );

  it.each([
    ["financial_entry", "收支记录", "/income/"],
    ["invoice", "发票", "/invoices/"],
  ] as const)(
    "opens a %s only after confirmation",
    (recordType, label, prefix) => {
      render(
        <MemoryRouter initialEntries={["/ai"]}>
          <AiWorkspaceRecordNavigation sessionId={null} />
          <LocationProbe />
        </MemoryRouter>,
      );
      act(() =>
        useUiStore
          .getState()
          .requestWorkspaceRecordNavigation(
            recordType,
            "018f0000-0000-7000-8000-000000000041",
          ),
      );
      expect(screen.getByTestId("location")).toHaveTextContent("/ai");
      fireEvent.click(screen.getByRole("button", { name: `打开${label}` }));
      expect(screen.getByTestId("location")).toHaveTextContent(
        `${prefix}018f0000-0000-7000-8000-000000000041`,
      );
    },
  );

  it("does not let an old dismissal close a newer navigation request", () => {
    const store = useUiStore.getState();
    store.requestWorkspaceRecordNavigation(
      "task",
      "018f0000-0000-7000-8000-000000000051",
    );
    const oldId = useUiStore.getState().workspaceRecordNavigationRequest!.id;
    store.dismissWorkspaceRecordNavigation(oldId);
    store.requestWorkspaceRecordNavigation(
      "task",
      "018f0000-0000-7000-8000-000000000052",
    );
    const newId = useUiStore.getState().workspaceRecordNavigationRequest!.id;
    expect(newId).not.toBe(oldId);
    store.dismissWorkspaceRecordNavigation(oldId);
    expect(
      useUiStore.getState().workspaceRecordNavigationRequest?.recordId,
    ).toBe("018f0000-0000-7000-8000-000000000052");
  });

  it("does not let an old browser dismissal close a newer browser request", () => {
    const store = useUiStore.getState();
    store.requestBrowserNavigation("https://example.com/first");
    const oldId = useUiStore.getState().browserNavigationRequest!.id;
    store.dismissBrowserNavigation(oldId);
    store.requestBrowserNavigation("https://example.com/second");
    const newId = useUiStore.getState().browserNavigationRequest!.id;
    expect(newId).not.toBe(oldId);
    store.dismissBrowserNavigation(oldId);
    expect(useUiStore.getState().browserNavigationRequest?.url).toBe(
      "https://example.com/second",
    );
    store.dismissBrowserNavigation(newId);
  });
});
