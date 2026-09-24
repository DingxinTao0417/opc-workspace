import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useWorkspacePanels } from "../store/workspacePanels";
import { AgentWorkspace } from "./AgentWorkspace";

vi.mock("./TaskArtifactWorkspacePreview", () => ({
  TaskArtifactWorkspacePreview: ({
    panel,
  }: {
    panel: { artifactId: string };
  }) => <output data-testid="artifact-preview">{panel.artifactId}</output>,
}));

afterEach(() => {
  cleanup();
  useWorkspacePanels.setState({ tabs: [], activeId: "", splitId: null });
});

describe("agent workspace artifact tab", () => {
  it("renders the exact artifact preview as a closable tab", () => {
    const id = "018f0000-0000-7000-8000-000000000041";
    useWorkspacePanels.setState({
      tabs: [
        {
          id: `task-artifact:${id}`,
          kind: "artifact",
          title: "任务产出",
          artifactId: id,
          artifactTaskId: "018f0000-0000-7000-8000-000000000042",
          artifactSubmissionId: "018f0000-0000-7000-8000-000000000043",
        },
      ],
      activeId: `task-artifact:${id}`,
    });
    render(
      <MemoryRouter>
        <AgentWorkspace />
      </MemoryRouter>,
    );
    expect(screen.getByRole("tab", { name: "任务产出" })).toBeVisible();
    expect(screen.getByTestId("artifact-preview")).toHaveTextContent(id);
    expect(screen.getByRole("button", { name: "关闭 任务产出" })).toBeVisible();
  });
});
