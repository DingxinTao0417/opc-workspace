import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AiProjectFileContext } from "./AiProjectFileContext";
import { useAiProjectFiles } from "../store/aiProjectFiles";
import { workspaceApi } from "../api/workspace";
import type { AiProvider } from "../types/models";
vi.mock("../api/workspace", () => ({
  workspaceApi: {
    fileSnapshot: vi.fn(),
    releaseFileSnapshot: vi.fn(async () => {}),
    validateFileSnapshot: vi.fn(),
  },
}));
const session = "018f0000-0000-7000-8000-000000000101";
const provider = {
  id: "018f0000-0000-7000-8000-000000000102",
  version: 2,
  name: "所选供应商",
  model: "selected-model",
  kind: "remote",
} as AiProvider;
afterEach(() => {
  cleanup();
  useAiProjectFiles.getState().clear();
  vi.clearAllMocks();
});
it("discloses provider, complete content and history caveat before granting; changing model revokes", async () => {
  // The actual byte length is validated, not inferred from display characters.
  vi.mocked(workspaceApi.fileSnapshot).mockResolvedValueOnce({
    id: "018f0000-0000-7000-8000-000000000103",
    path: "file.txt",
    content: "<script>data only</script>",
    size: new TextEncoder().encode("<script>data only</script>").length,
    sha256: "a".repeat(64),
    expiresInSeconds: 600,
  });
  await useAiProjectFiles
    .getState()
    .stage({ id: "root", name: "project", path: "D:/private" }, "file.txt");
  const view = render(
    <AiProjectFileContext
      sessionId={session}
      provider={provider}
      disabled={false}
    />,
  );
  expect(screen.getByText(/远程供应商.*所选供应商/)).toHaveTextContent(
    "模型回复可能引用文件内容",
  );
  expect(screen.getByText("<script>data only</script>")).toBeInTheDocument();
  expect(view.container.querySelector("script")).toBeNull();
  expect(useAiProjectFiles.getState().approval).toBeNull();
  fireEvent.click(
    screen.getByRole("button", {
      name: "仅授权下一条消息使用这些文件",
    }),
  );
  expect(useAiProjectFiles.getState().approval?.providerId).toBe(provider.id);
  view.rerender(
    <AiProjectFileContext
      sessionId={session}
      provider={{ ...provider, version: 3 }}
      disabled={false}
    />,
  );
  expect(useAiProjectFiles.getState().approval).toBeNull();
  view.unmount();
  expect(useAiProjectFiles.getState().selection).toBeNull();
  expect(workspaceApi.releaseFileSnapshot).toHaveBeenCalledWith(
    "root",
    "018f0000-0000-7000-8000-000000000103",
  );
});

it("previews every selected file and revokes the bundle grant when one is removed", async () => {
  vi.mocked(workspaceApi.fileSnapshot)
    .mockResolvedValueOnce({
      id: "018f0000-0000-7000-8000-000000000103",
      path: "src/a.ts",
      content: "const a = 1;",
      size: 12,
      sha256: "a".repeat(64),
      expiresInSeconds: 600,
    })
    .mockResolvedValueOnce({
      id: "018f0000-0000-7000-8000-000000000104",
      path: "src/b.ts",
      content: "const b = 2;",
      size: 12,
      sha256: "b".repeat(64),
      expiresInSeconds: 600,
    });
  const root = { id: "root", name: "project", path: "D:/private" };
  await useAiProjectFiles.getState().stage(root, "src/a.ts");
  await useAiProjectFiles.getState().stage(root, "src/b.ts");
  render(
    <AiProjectFileContext
      sessionId={session}
      provider={provider}
      disabled={false}
    />,
  );
  expect(screen.getByText("项目文件 · 本条消息授权 2/4")).toBeInTheDocument();
  expect(screen.getByText("const a = 1;")).toBeInTheDocument();
  expect(screen.getByText("const b = 2;")).toBeInTheDocument();
  expect(screen.getByText(/将把以上 2 个相对路径/)).toBeInTheDocument();
  useAiProjectFiles.getState().approve(session, provider);
  fireEvent.click(screen.getByRole("button", { name: "移除 src/b.ts" }));
  expect(useAiProjectFiles.getState().selection?.files).toHaveLength(1);
  expect(useAiProjectFiles.getState().approval).toBeNull();
  expect(workspaceApi.releaseFileSnapshot).toHaveBeenCalledWith(
    root.id,
    "018f0000-0000-7000-8000-000000000104",
  );
});
