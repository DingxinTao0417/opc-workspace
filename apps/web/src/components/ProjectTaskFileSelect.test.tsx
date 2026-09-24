import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ProjectTaskFileSelect } from "./ProjectTaskFileSelect";
import { projectFile } from "../test/agentProjectFileFixture";
import type { AgentRunFileCandidate } from "../types/models";
const api = vi.hoisted(() => ({ list: vi.fn() }));
vi.mock("../api/agentProjectFiles", () => ({ getAgentProjectFiles: api.list }));
const taskId = "018f0000-0000-7000-8000-000000006201";
beforeEach(() => {
  vi.resetAllMocks();
  api.list.mockResolvedValue({
    items: [projectFile],
    total: 1,
    offset: 0,
    nextOffset: null,
  });
});
afterEach(cleanup);
function mount(
  otherCount = 0,
  otherBytes = 0,
  projectId: string | null = projectFile.sourceTask.project_id,
) {
  function Selection() {
    const [selected, setSelected] = useState<AgentRunFileCandidate[]>([]);
    return (
      <ProjectTaskFileSelect
        taskId={taskId}
        projectId={projectId}
        selected={selected}
        onChange={setSelected}
        otherCount={otherCount}
        otherBytes={otherBytes}
      />
    );
  }
  return render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: { queries: { retry: false, gcTime: 0 } },
        })
      }
    >
      <Selection />
    </QueryClientProvider>,
  );
}
it("only loads after explicit opening and preserves selection across real pages", async () => {
  const page = Array.from({ length: 20 }, (_, i) => ({
    ...projectFile,
    id: `018f0000-0000-7000-8000-${String(6300 + i).padStart(12, "0")}`,
    name: `page-${i}.md`,
  }));
  api.list.mockImplementation((_task, offset) =>
    Promise.resolve(
      offset === 0
        ? { items: page, offset, total: 21, nextOffset: 20 }
        : { items: [projectFile], offset, total: 21, nextOffset: null },
    ),
  );
  mount();
  expect(api.list).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "选择跨任务已验收文件" }));
  fireEvent.click(
    await screen.findByRole("checkbox", { name: "选择跨任务文件 page-0.md" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "下一页文件" }));
  fireEvent.click(
    await screen.findByRole("checkbox", { name: "选择跨任务文件 accepted.md" }),
  );
  expect(
    screen.getByRole("button", { name: "移除跨任务文件 page-0.md" }),
  ).toBeVisible();
  expect(
    screen.getByRole("button", { name: "移除跨任务文件 accepted.md" }),
  ).toBeVisible();
  // The initial empty search must not schedule a late reset back to page 1.
  await new Promise((resolve) => setTimeout(resolve, 300));
  expect(
    screen.getByRole("checkbox", { name: "选择跨任务文件 accepted.md" }),
  ).toBeChecked();
  expect(screen.getByRole("button", { name: "下一页文件" })).toBeDisabled();
  expect(api.list).toHaveBeenCalledWith(
    taskId,
    20,
    "",
    expect.any(AbortSignal),
  );
  fireEvent.change(
    screen.getByRole("textbox", { name: "搜索来源任务或文件" }),
    { target: { value: "精确来源" } },
  );
  await waitFor(() =>
    expect(api.list).toHaveBeenCalledWith(
      taskId,
      0,
      "精确来源",
      expect.any(AbortSignal),
    ),
  );
  expect(
    screen.getByRole("button", { name: "移除跨任务文件 accepted.md" }),
  ).toBeVisible();
});
it.each([
  [4, 0],
  [0, 131000],
])("shares existing count/byte budgets %s %s", async (count, bytes) => {
  mount(count, bytes);
  fireEvent.click(screen.getByRole("button", { name: "选择跨任务已验收文件" }));
  expect(
    await screen.findByRole("checkbox", { name: "选择跨任务文件 accepted.md" }),
  ).toBeDisabled();
});
it("keeps lookup errors visible and retries without selecting anything", async () => {
  api.list.mockRejectedValueOnce(new Error("unavailable"));
  mount();
  fireEvent.click(screen.getByRole("button", { name: "选择跨任务已验收文件" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("读取失败");
  fireEvent.click(screen.getByRole("button", { name: "重读跨任务文件" }));
  expect(
    await screen.findByRole("checkbox", { name: "选择跨任务文件 accepted.md" }),
  ).not.toBeChecked();
});
it("does not query without a project and hides wrong-project candidates", async () => {
  const view = mount(0, 0, null);
  expect(api.list).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("button", { name: "选择跨任务已验收文件" }),
  ).toBeNull();
  view.unmount();
  api.list.mockResolvedValue({
    items: [
      {
        ...projectFile,
        sourceTask: { ...projectFile.sourceTask, project_id: taskId },
      },
    ],
    offset: 0,
    total: 1,
    nextOffset: null,
  });
  mount();
  fireEvent.click(screen.getByRole("button", { name: "选择跨任务已验收文件" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("来源不一致");
  expect(screen.queryByRole("checkbox")).toBeNull();
});
