import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import type { Tag } from "../types/models";
import { TagManagerModal } from "./TagManagerModal";

const apiMocks = vi.hoisted(() => ({
  getAllTags: vi.fn(),
  createTag: vi.fn(),
  updateTag: vi.fn(),
  deleteTag: vi.fn(),
}));

vi.mock("../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../api/client")>("../api/client");
  return { ...actual, ...apiMocks };
});

const tag: Tag = {
  id: "018f0000-0000-7000-8000-000000001951",
  name: "验收",
  color: "#6E7BF2",
  version: 2,
  createdAt: "2026-09-20T08:00:00Z",
};

function renderModal() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const onClose = vi.fn();
  const view = render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <TagManagerModal open onClose={onClose} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { ...view, onClose };
}

describe("TagManagerModal", () => {
  beforeEach(() => {
    useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
    apiMocks.getAllTags.mockResolvedValue([tag]);
    apiMocks.createTag.mockResolvedValue(tag);
    apiMocks.updateTag.mockResolvedValue(tag);
    apiMocks.deleteTag.mockResolvedValue({ deleted: true });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
  });

  it("hands a saved tag to the agent without editing or deleting it", async () => {
    const { onClose } = renderModal();

    fireEvent.click(await screen.findByRole("button", { name: "交给智能体" }));

    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "任务标签",
      route: "/tasks",
      scopes: ["work", "actions"],
    });
    expect(pending?.prompt).toContain("workspace_task_options");
    expect(pending?.prompt).toContain(`tag_id=${tag.id}`);
    expect(pending?.prompt).toContain("tag.update");
    expect(pending?.prompt).toContain("tag.delete");
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(apiMocks.updateTag).not.toHaveBeenCalled();
    expect(apiMocks.deleteTag).not.toHaveBeenCalled();
  });

  it("disables tag handoff while local tag edits are unsaved", async () => {
    renderModal();

    const handoff = await screen.findByRole("button", {
      name: "交给智能体",
    });
    expect(handoff).toBeEnabled();
    fireEvent.change(screen.getByLabelText("标签名称：验收"), {
      target: { value: "验收中" },
    });

    await waitFor(() => expect(handoff).toBeDisabled());
    fireEvent.click(handoff);
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  });
});
