import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getKnowledgeSources } from "../api/client";
import type {
  AiProvider,
  AiWorkspaceGrant,
  KnowledgeSource,
} from "../types/models";
import { AiKnowledgeAccess } from "./AiKnowledgeAccess";
import { AiWorkspaceAccess } from "./AiWorkspaceAccess";

vi.mock("../api/client", () => ({ getKnowledgeSources: vi.fn() }));
const provider = {
  id: "provider",
  name: "Remote model",
  kind: "remote",
  model: "test",
  version: 2,
} as AiProvider;
const source = (id: string, name: string, version = 2): KnowledgeSource => ({
  id,
  name,
  version,
  title: name,
  sourceType: "markdown",
  status: "ready",
  chunkCount: 4,
  deletedAt: null,
  deleteReason: null,
  importMode: "managed_copy",
  mimeType: "text/markdown",
  sizeBytes: 100,
  contentSha256: "hash",
  lastIndexedAt: null,
  createdAt: "",
  updatedAt: "",
  documentId: "doc",
  documentVersion: 1,
  latestJob: null,
});
const first = source("018f0000-0000-7000-8000-000000006101", "产品手册.md");
const second = source("018f0000-0000-7000-8000-000000006102", "验收要求.md");
function mount(element: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={client}>{element}</QueryClientProvider>,
  );
  return { ...view, client };
}
beforeEach(() => {
  vi.mocked(getKnowledgeSources).mockReset();
  vi.mocked(getKnowledgeSources).mockResolvedValue({
    items: [first],
    meta: { page: 1, pageSize: 10, total: 1 },
  });
});
afterEach(cleanup);
describe("per-message knowledge consent", () => {
  it("requires selected sources, discloses external transfer and grants no business scope", async () => {
    const change = vi.fn();
    mount(
      <AiWorkspaceAccess
        provider={provider}
        disabled={false}
        onChange={change}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    expect(getKnowledgeSources).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("checkbox", { name: /^知识检索与引用/ }));
    expect(
      screen.getByRole("button", { name: "仅授权下一条消息" }),
    ).toBeDisabled();
    expect(
      screen.getByText(/查询结果会发送给以上远程供应商/),
    ).toBeInTheDocument();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /产品手册.md/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(change).toHaveBeenCalledWith({
      provider_version: 2,
      scopes: ["knowledge"],
      knowledge_sources: [{ source_id: first.id, expected_source_version: 2 }],
    });
    // Unaccepted parent value means no sticky permission when reopened.
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    expect(
      screen.getByRole("checkbox", { name: /^知识检索与引用/ }),
    ).not.toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "不授权" }));
    expect(change).toHaveBeenLastCalledWith(undefined);
  });
  it("keeps explicit selections across source pages and name filters", async () => {
    vi.mocked(getKnowledgeSources).mockImplementation(async (input) => ({
      items: input?.page === 2 || input?.search ? [second] : [first],
      meta: { page: input?.page ?? 1, pageSize: 10, total: 11 },
    }));
    const change = vi.fn();
    function Picker() {
      const [value, setValue] = useState<
        NonNullable<AiWorkspaceGrant["knowledge_sources"]>
      >([]);
      return (
        <AiKnowledgeAccess
          value={value}
          disabled={false}
          onChange={(next) => {
            setValue(next);
            change(next);
          }}
        />
      );
    }
    mount(<Picker />);
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /产品手册.md/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "下一页来源" }));
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /验收要求.md/ }),
    );
    expect(change).toHaveBeenLastCalledWith([
      { source_id: first.id, expected_source_version: 2 },
      { source_id: second.id, expected_source_version: 2 },
    ]);
    fireEvent.change(screen.getByRole("searchbox"), {
      target: { value: "验收" },
    });
    await waitFor(() =>
      expect(getKnowledgeSources).toHaveBeenLastCalledWith({
        page: 1,
        pageSize: 10,
        search: "验收",
      }),
    );
    expect(
      within(screen.getByLabelText("已授权知识来源")).getByText(/产品手册/),
    ).toBeInTheDocument();
    fireEvent.click(
      within(screen.getByLabelText("已授权知识来源")).getAllByRole("button", {
        name: "移除来源",
      })[0],
    );
    expect(change).toHaveBeenLastCalledWith([
      { source_id: second.id, expected_source_version: 2 },
    ]);
  });
  it("does not silently renew a changed source version and excludes unindexed sources", async () => {
    vi.mocked(getKnowledgeSources).mockResolvedValue({
      items: [
        { ...first, version: 3 },
        { ...second, status: "failed", chunkCount: 0 },
      ],
      meta: { page: 1, pageSize: 10, total: 2 },
    });
    const change = vi.fn();
    mount(
      <AiKnowledgeAccess
        value={[{ source_id: first.id, expected_source_version: 2 }]}
        onChange={change}
        disabled={false}
      />,
    );
    expect(
      await screen.findByText(/版本已变，请取消后重新勾选/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("checkbox", { name: /验收要求.md/ }),
    ).toBeDisabled();
    expect(change).not.toHaveBeenCalled();
  });
  it("shows retry and empty states without auto-authorizing anything", async () => {
    vi.mocked(getKnowledgeSources).mockRejectedValueOnce(new Error("storage"));
    vi.mocked(getKnowledgeSources).mockResolvedValue({
      items: [],
      meta: { page: 1, pageSize: 10, total: 0 },
    });
    const change = vi.fn();
    mount(<AiKnowledgeAccess value={[]} onChange={change} disabled={false} />);
    fireEvent.click(
      await screen.findByRole("button", { name: "重试来源列表" }),
    );
    expect(await screen.findByText(/没有可匹配的知识来源/)).toBeInTheDocument();
    expect(change).not.toHaveBeenCalled();
  });
});
