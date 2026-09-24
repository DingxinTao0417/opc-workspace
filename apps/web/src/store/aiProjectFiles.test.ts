import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { workspaceApi, type WorkspaceFileSnapshot } from "../api/workspace";
import { useAiProjectFiles } from "./aiProjectFiles";

vi.mock("../api/workspace", () => ({
  workspaceApi: {
    fileSnapshot: vi.fn(),
    validateFileSnapshot: vi.fn(),
    releaseFileSnapshot: vi.fn(async () => {}),
  },
}));
const root = {
  id: "local-only-root",
  name: "project",
  path: "D:/private-project",
};
const session = "018f0000-0000-7000-8000-000000000101";
const provider = { id: "018f0000-0000-7000-8000-000000000102", version: 2 };
const exact: WorkspaceFileSnapshot = {
  id: "018f0000-0000-7000-8000-000000000103",
  path: "src/a.txt",
  content: "原文\r\n",
  size: 8,
  sha256: "a".repeat(64),
  expiresInSeconds: 600,
};
beforeEach(() => {
  useAiProjectFiles.getState().clear();
  vi.clearAllMocks();
  vi.mocked(workspaceApi.fileSnapshot).mockResolvedValue({ ...exact });
  vi.mocked(workspaceApi.validateFileSnapshot).mockResolvedValue(undefined);
});
afterEach(() => {
  useAiProjectFiles.getState().clear();
  vi.useRealTimers();
});
const stage = () => useAiProjectFiles.getState().stage(root, exact.path);

describe("one-message exact file approval", () => {
  it("does not grant by selecting, sends only exact disclosed fields and revalidates before use", async () => {
    await stage();
    await expect(
      useAiProjectFiles.getState().prepare(session, provider),
    ).rejects.toThrow("确认");
    expect(workspaceApi.validateFileSnapshot).not.toHaveBeenCalled();
    useAiProjectFiles.getState().approve(session, provider);
    const payload = await useAiProjectFiles
      .getState()
      .prepare(session, provider);
    expect(payload).toMatchObject({
      provider_id: provider.id,
      provider_version: 2,
      session_id: session,
      confirmed: true,
      files: [
        { path: exact.path, content: exact.content, sha256: exact.sha256 },
      ],
    });
    expect(workspaceApi.validateFileSnapshot).toHaveBeenCalledWith(
      root.id,
      exact.id,
    );
    expect(JSON.stringify(payload)).not.toContain(root.id);
    expect(JSON.stringify(payload)).not.toContain(root.path);
    expect(JSON.stringify(payload)).not.toContain(exact.id);
  });

  it.each(["session", "provider", "version"])(
    "rejects changed %s without reading",
    async (change) => {
      await stage();
      useAiProjectFiles.getState().approve(session, provider);
      await expect(
        useAiProjectFiles
          .getState()
          .prepare(change === "session" ? provider.id : session, {
            ...provider,
            ...(change === "provider" ? { id: session } : {}),
            ...(change === "version" ? { version: 3 } : {}),
          }),
      ).rejects.toThrow("授权");
      expect(workspaceApi.validateFileSnapshot).not.toHaveBeenCalled();
    },
  );

  it("rejects revoked or replaced approval during native revalidation", async () => {
    await stage();
    useAiProjectFiles.getState().approve(session, provider);
    let resolve!: () => void;
    vi.mocked(workspaceApi.validateFileSnapshot).mockReturnValueOnce(
      new Promise<void>((r) => {
        resolve = r;
      }),
    );
    const sending = useAiProjectFiles.getState().prepare(session, provider);
    useAiProjectFiles.getState().revoke();
    // Even same identities cannot revive the in-flight approval instance.
    useAiProjectFiles.getState().approve(session, provider);
    resolve();
    await expect(sending).rejects.toThrow("已变化");
    expect(useAiProjectFiles.getState().approval).not.toBeNull();
  });

  it("clears approval when native content has drifted", async () => {
    await stage();
    useAiProjectFiles.getState().approve(session, provider);
    vi.mocked(workspaceApi.validateFileSnapshot).mockRejectedValueOnce(
      new Error("文件已变化"),
    );
    await expect(
      useAiProjectFiles.getState().prepare(session, provider),
    ).rejects.toThrow("已变化");
    expect(useAiProjectFiles.getState().approval).toBeNull();
  });

  it("bundles up to four exact files from one root and validates each before sending", async () => {
    vi.mocked(workspaceApi.fileSnapshot)
      .mockResolvedValueOnce({ ...exact })
      .mockResolvedValueOnce({
        ...exact,
        id: "018f0000-0000-7000-8000-000000000104",
        path: "src/b.txt",
        content: "第二份",
        size: new TextEncoder().encode("第二份").byteLength,
        sha256: "b".repeat(64),
        expiresInSeconds: 300,
      });
    await useAiProjectFiles.getState().stage(root, "src/a.txt");
    await useAiProjectFiles.getState().stage(root, "src/b.txt");
    const selection = useAiProjectFiles.getState().selection!;
    expect(selection.files.map(({ snapshot }) => snapshot.path)).toEqual([
      "src/a.txt",
      "src/b.txt",
    ]);
    expect(selection.totalBytes).toBe(
      8 + new TextEncoder().encode("第二份").byteLength,
    );
    useAiProjectFiles.getState().approve(session, provider);
    const payload = await useAiProjectFiles
      .getState()
      .prepare(session, provider);
    expect(payload?.files).toEqual([
      { path: "src/a.txt", content: exact.content, sha256: exact.sha256 },
      { path: "src/b.txt", content: "第二份", sha256: "b".repeat(64) },
    ]);
    expect(workspaceApi.validateFileSnapshot).toHaveBeenNthCalledWith(
      1,
      root.id,
      exact.id,
    );
    expect(workspaceApi.validateFileSnapshot).toHaveBeenNthCalledWith(
      2,
      root.id,
      "018f0000-0000-7000-8000-000000000104",
    );
    expect(Date.parse(payload!.expires_at)).toBe(selection.expiresAt);
    expect(JSON.stringify(payload)).not.toContain(root.id);
    expect(JSON.stringify(payload)).not.toContain(root.path);
    expect(JSON.stringify(payload)).not.toContain(exact.id);
  });

  it("keeps the prior bundle but revokes consent when a same-root or byte cap is exceeded", async () => {
    const firstContent = "a".repeat(16 * 1024);
    const oversizedTotalContent = "b".repeat(16 * 1024 + 1);
    vi.mocked(workspaceApi.fileSnapshot)
      .mockResolvedValueOnce({
        ...exact,
        path: "large-a.txt",
        content: firstContent,
        size: firstContent.length,
      })
      .mockResolvedValueOnce({
        ...exact,
        id: "018f0000-0000-7000-8000-000000000104",
        path: "large-b.txt",
        content: oversizedTotalContent,
        size: oversizedTotalContent.length,
      });
    await useAiProjectFiles.getState().stage(root, "large-a.txt");
    useAiProjectFiles.getState().approve(session, provider);
    await useAiProjectFiles.getState().stage(root, "large-b.txt");
    expect(useAiProjectFiles.getState().selection?.files).toHaveLength(1);
    expect(
      useAiProjectFiles.getState().selection?.files[0]?.snapshot.path,
    ).toBe("large-a.txt");
    expect(useAiProjectFiles.getState().approval).toBeNull();
    expect(useAiProjectFiles.getState().error).toContain("32 KiB");
    expect(workspaceApi.releaseFileSnapshot).toHaveBeenCalledWith(
      root.id,
      "018f0000-0000-7000-8000-000000000104",
    );
  });

  it("replaces a reselected path and removes only the requested file", async () => {
    await stage();
    useAiProjectFiles.getState().approve(session, provider);
    vi.mocked(workspaceApi.fileSnapshot).mockResolvedValueOnce({
      ...exact,
      id: "018f0000-0000-7000-8000-000000000104",
      content: "updated",
      size: 7,
    });
    await useAiProjectFiles.getState().stage(root, exact.path);
    expect(useAiProjectFiles.getState().selection?.files).toHaveLength(1);
    expect(
      useAiProjectFiles.getState().selection?.files[0]?.snapshot.content,
    ).toBe("updated");
    expect(useAiProjectFiles.getState().approval).toBeNull();
    expect(workspaceApi.releaseFileSnapshot).toHaveBeenCalledWith(
      root.id,
      exact.id,
    );
    vi.mocked(workspaceApi.fileSnapshot).mockResolvedValueOnce({
      ...exact,
      id: "018f0000-0000-7000-8000-000000000105",
      path: "b.txt",
    });
    await useAiProjectFiles.getState().stage(root, "b.txt");
    useAiProjectFiles.getState().approve(session, provider);
    useAiProjectFiles.getState().remove("b.txt");
    expect(useAiProjectFiles.getState().selection?.files).toHaveLength(1);
    expect(useAiProjectFiles.getState().approval).toBeNull();
    expect(workspaceApi.releaseFileSnapshot).toHaveBeenCalledWith(
      root.id,
      "018f0000-0000-7000-8000-000000000105",
    );
  });

  it("rejects files from another root and never captures more than four", async () => {
    await stage();
    await useAiProjectFiles
      .getState()
      .stage({ ...root, id: "other-root" }, "other.txt");
    expect(useAiProjectFiles.getState().selection?.files).toHaveLength(1);
    expect(useAiProjectFiles.getState().error).toContain("同一个");
    expect(workspaceApi.fileSnapshot).toHaveBeenCalledTimes(1);
    useAiProjectFiles.getState().clear();
    vi.mocked(workspaceApi.fileSnapshot).mockImplementation(
      async (_rootId, path) => ({
        ...exact,
        id:
          "018f0000-0000-7000-8000-" +
          String(path.charCodeAt(0)).padStart(12, "0"),
        path,
      }),
    );
    for (const path of ["a.txt", "b.txt", "c.txt", "d.txt"])
      await useAiProjectFiles.getState().stage(root, path);
    await useAiProjectFiles.getState().stage(root, "e.txt");
    expect(useAiProjectFiles.getState().selection?.files).toHaveLength(4);
    expect(useAiProjectFiles.getState().error).toContain("最多选择 4");
    expect(workspaceApi.fileSnapshot).toHaveBeenCalledTimes(5);
  });

  it("expires before read and rejects oversized complete files without truncation", async () => {
    vi.useFakeTimers();
    await stage();
    useAiProjectFiles.getState().approve(session, provider);
    vi.advanceTimersByTime(600_001);
    await expect(
      useAiProjectFiles.getState().prepare(session, provider),
    ).rejects.toThrow("授权");
    vi.mocked(workspaceApi.fileSnapshot).mockResolvedValueOnce({
      ...exact,
      content: "x".repeat(32 * 1024 + 1),
      size: 32 * 1024 + 1,
    });
    await stage();
    expect(useAiProjectFiles.getState().selection).toBeNull();
    expect(useAiProjectFiles.getState().error).toContain("32 KiB");
    await expect(
      useAiProjectFiles.getState().prepare(session, provider),
    ).rejects.toThrow("32 KiB");
  });

  it("releases a late capture after removal and never stages it", async () => {
    let resolve!: (s: WorkspaceFileSnapshot) => void;
    vi.mocked(workspaceApi.fileSnapshot).mockReturnValueOnce(
      new Promise((r) => {
        resolve = r;
      }),
    );
    const loading = stage();
    useAiProjectFiles.getState().clear();
    resolve(exact);
    await loading;
    expect(useAiProjectFiles.getState().selection).toBeNull();
    expect(useAiProjectFiles.getState().loading).toBe(false);
    expect(workspaceApi.releaseFileSnapshot).toHaveBeenCalledWith(
      root.id,
      exact.id,
    );
  });
});
