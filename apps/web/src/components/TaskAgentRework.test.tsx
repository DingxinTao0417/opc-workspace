import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TaskAgentRework } from "./TaskAgentRework";
import type { AiProvider, Task } from "../types/models";
const mocks = vi.hoisted(() => ({ source: vi.fn(), preview: vi.fn() }));
vi.mock("../api/taskSubmissionLocation", () => ({
  getTaskSubmissionLocation: mocks.source,
}));
vi.mock("../api/agentRunRework", () => ({
  getAgentRunReworkPreview: mocks.preview,
}));
const submissionId = "018f0000-0000-7000-8000-000000000951";
const task = {
  id: "018f0000-0000-7000-8000-000000000950",
  version: 7,
  currentSubmissionId: submissionId,
  reviewPolicy: "manual",
  status: "in_progress",
} as Task;
const provider = {
  id: "provider",
  name: "原模型",
  kind: "remote",
  version: 2,
  configVersion: 3,
} as AiProvider;
const context = {
  submission_id: submissionId,
  sequence: 1,
  review_reason: "原始退回意见",
  reviewed_at: "2026-09-21T12:00:00Z",
  artifacts: [],
};
beforeEach(() => {
  vi.resetAllMocks();
  mocks.source.mockResolvedValue({
    task: { ...task, status: "in_progress" },
    submission: {
      id: submissionId,
      sequence: 1,
      status: "changes_requested",
      reviewReason: "原始退回意见",
      artifacts: [],
    },
  });
  mocks.preview.mockResolvedValue(context);
});
afterEach(cleanup);
function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const onChange = vi.fn();
  const ui = (current = provider, currentTask = task) => (
    <QueryClientProvider client={client}>
      <TaskAgentRework
        task={currentTask}
        provider={current}
        disabled={false}
        hasAssignee
        onChange={onChange}
      />
    </QueryClientProvider>
  );
  return { ...render(ui()), onChange, ui, client };
}
async function preview() {
  fireEvent.change(
    await screen.findByRole("combobox", { name: "选择返工批次" }),
    { target: { value: submissionId } },
  );
  fireEvent.click(screen.getByRole("button", { name: "预览完整返工上下文" }));
  await screen.findByRole("checkbox", { name: "同意发送返工意见与旧产出" });
}
it.each(["version", "protocol", "model"])(
  "supports reason-only context and clears consent through Provider %s ABA",
  async (field) => {
    const view = mount();
    await preview();
    expect(
      screen.getByText(/本次仅携带返工意见，未携带任何旧稿/),
    ).toBeVisible();
    expect(screen.getByText(/会离开本机，发送后无法撤回/)).toBeVisible();
    fireEvent.click(
      screen.getByRole("checkbox", { name: "同意发送返工意见与旧产出" }),
    );
    await waitFor(() =>
      expect(view.onChange).toHaveBeenLastCalledWith(
        expect.objectContaining({ context }),
      ),
    );
    view.rerender(
      view.ui({
        ...provider,
        [field]:
          field === "version"
            ? 3
            : field === "protocol"
              ? "anthropic_messages"
              : "changed-model",
      }),
    );
    await waitFor(() => expect(view.onChange).toHaveBeenLastCalledWith(null));
    view.rerender(view.ui());
    expect(
      screen.getByRole("checkbox", { name: "同意发送返工意见与旧产出" }),
    ).not.toBeChecked();
  },
);
it("revokes approved context when source is deselected or closed", async () => {
  const view = mount();
  await preview();
  fireEvent.click(
    screen.getByRole("checkbox", { name: "同意发送返工意见与旧产出" }),
  );
  fireEvent.change(screen.getByRole("combobox", { name: "选择返工批次" }), {
    target: { value: "" },
  });
  await waitFor(() => expect(view.onChange).toHaveBeenLastCalledWith(null));
  expect(screen.queryByLabelText("返工上下文完整预览")).not.toBeInTheDocument();
  view.unmount();
  expect(view.onChange).toHaveBeenLastCalledWith(null);
});
it("does not expose approval on failed or late preview after selection changes", async () => {
  let resolve!: (value: typeof context) => void;
  mocks.preview.mockReturnValue(
    new Promise((done) => {
      resolve = done;
    }),
  );
  const view = mount();
  fireEvent.change(
    await screen.findByRole("combobox", { name: "选择返工批次" }),
    { target: { value: submissionId } },
  );
  fireEvent.click(screen.getByRole("button", { name: "预览完整返工上下文" }));
  await waitFor(() => expect(mocks.preview).toHaveBeenCalledOnce());
  fireEvent.change(screen.getByRole("combobox", { name: "选择返工批次" }), {
    target: { value: "" },
  });
  resolve(context);
  await waitFor(() => expect(view.onChange).toHaveBeenLastCalledWith(null));
  expect(
    screen.queryByRole("checkbox", { name: "同意发送返工意见与旧产出" }),
  ).not.toBeInTheDocument();
});
it("clears approval during source refetch and Task version ABA", async () => {
  const view = mount();
  await preview();
  fireEvent.click(
    screen.getByRole("checkbox", { name: "同意发送返工意见与旧产出" }),
  );
  await waitFor(() =>
    expect(view.onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ context }),
    ),
  );
  const latestSource = await mocks.source();
  let resolve!: (value: unknown) => void;
  mocks.source.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  void view.client.invalidateQueries({ queryKey: ["agent-rework-source"] });
  await waitFor(() => expect(view.onChange).toHaveBeenLastCalledWith(null));
  await act(async () => {
    resolve(latestSource);
  });
  mocks.source.mockResolvedValue(latestSource);
  expect(
    screen.getByRole("checkbox", { name: "同意发送返工意见与旧产出" }),
  ).not.toBeChecked();
  view.rerender(view.ui(provider, { ...task, version: 8 }));
  view.rerender(view.ui());
  expect(view.onChange).toHaveBeenLastCalledWith(null);
  expect(
    await screen.findByRole("checkbox", { name: "同意发送返工意见与旧产出" }),
  ).not.toBeChecked();
});
it.each(["todo", "none"])(
  "rejects a source outside manual in_progress: %s",
  async (kind) => {
    if (kind === "todo")
      mocks.source.mockResolvedValue({
        task: { ...task, status: "todo" },
        submission: {
          id: submissionId,
          status: "changes_requested",
          reviewReason: "原因",
        },
      });
    const view = mount();
    if (kind === "none")
      view.rerender(view.ui(provider, { ...task, reviewPolicy: "none" }));
    await screen.findByText(/当前没有与任务版本匹配的已退回批次/);
    expect(
      screen.queryByRole("combobox", { name: "选择返工批次" }),
    ).not.toBeInTheDocument();
    expect(mocks.preview).not.toHaveBeenCalled();
  },
);
