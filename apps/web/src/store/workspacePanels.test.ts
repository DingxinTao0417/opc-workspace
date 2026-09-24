import { beforeEach, describe, expect, it } from "vitest";
import { useWorkspacePanels } from "./workspacePanels";

beforeEach(() =>
  useWorkspacePanels.setState({
    tabs: [],
    activeId: "",
    splitId: null,
    splitRatio: 0.5,
    handledLayoutRequest: 0,
    browserRevision: -1,
    root: null,
    maximized: false,
  }),
);
describe("workspace tabs", () => {
  it("bounds the split ratio and restores invalid values to an even split", () => {
    const { setSplitRatio } = useWorkspacePanels.getState();

    setSplitRatio(0.1);
    expect(useWorkspacePanels.getState().splitRatio).toBe(0.25);
    setSplitRatio(0.9);
    expect(useWorkspacePanels.getState().splitRatio).toBe(0.75);
    setSplitRatio(Number.NaN);
    expect(useWorkspacePanels.getState().splitRatio).toBe(0.5);
  });

  it("starts each new split pair evenly", () => {
    const panels = useWorkspacePanels.getState();
    panels.open({ kind: "files", title: "文件" }, "files");
    panels.open({ kind: "review", title: "审查" }, "review");
    panels.setSplitRatio(0.7);
    panels.setSplitWith("files");
    expect(useWorkspacePanels.getState().splitRatio).toBe(0.5);
  });

  it("bounds ordinary tabs while still allowing selection of an existing file", () => {
    const s = useWorkspacePanels.getState();
    for (let i = 0; i < 24; i++)
      s.open({ kind: "file", title: `${i}.ts` }, `${i}`);
    expect(() => s.open({ kind: "review", title: "review" })).toThrow("24");
    expect(() => s.open({ kind: "file", title: "0.ts" }, "0")).not.toThrow();
    expect(useWorkspacePanels.getState().tabs).toHaveLength(24);
  });
  it("deduplicates file tabs and preserves drafts across selection", () => {
    const s = useWorkspacePanels.getState();
    s.open({ kind: "file", title: "a.ts", path: "a.ts" }, "file:a");
    s.open({ kind: "chat", title: "chat", draft: "unsent" }, "chat");
    s.open({ kind: "file", title: "a.ts", path: "a.ts" }, "file:a");
    expect(useWorkspacePanels.getState().tabs).toHaveLength(2);
    expect(useWorkspacePanels.getState().tabs[1].draft).toBe("unsent");
  });
  it("keeps close selection valid and clears the split on last tab", () => {
    const s = useWorkspacePanels.getState();
    s.open({ kind: "files", title: "files" }, "a");
    s.open({ kind: "review", title: "review" }, "b");
    s.split();
    expect(useWorkspacePanels.getState().splitId).toBe("a");
    s.close("b");
    expect(useWorkspacePanels.getState().activeId).toBe("a");
    expect(useWorkspacePanels.getState().splitId).toBeNull();
  });
  it("rejects stale native snapshots and limits split to one Chromium surface", () => {
    const s = useWorkspacePanels.getState();
    const tabs = ["one", "two"].map((id) => ({
      id,
      url: "about:blank",
      title: id,
      loading: false,
      canGoBack: false,
      canGoForward: false,
      error: null,
    }));
    s.syncBrowser({ tabs, activeTabId: "one", revision: 2 });
    s.select("browser:one");
    s.split();
    expect(useWorkspacePanels.getState().splitId).toBeNull();
    s.open({ kind: "files", title: "files" }, "file");
    s.split();
    expect(useWorkspacePanels.getState().splitId).toBe("browser:one");
    s.select("browser:two");
    expect(useWorkspacePanels.getState().splitId).toBeNull();
    s.syncBrowser({ tabs: [], activeTabId: null, revision: 1 });
    expect(useWorkspacePanels.getState().tabs).toHaveLength(3);
  });
  it("does not leave the same tab in both panes after closing its neighbor", () => {
    const s = useWorkspacePanels.getState();
    s.open({ kind: "files", title: "files" }, "a");
    s.open({ kind: "review", title: "review" }, "b");
    s.open({ kind: "chat", title: "chat" }, "c");
    s.select("a");
    s.split();
    expect(useWorkspacePanels.getState().splitId).toBe("b");
    s.close("a");
    expect(useWorkspacePanels.getState().activeId).toBe("b");
    expect(useWorkspacePanels.getState().splitId).toBeNull();
  });
  it("sets an exact split pair and rejects stale, identical, or dual-browser panes", () => {
    const s = useWorkspacePanels.getState();
    s.open({ kind: "files", title: "文件" }, "files");
    s.open({ kind: "review", title: "变更审查" }, "review");
    s.select("files");
    expect(s.setSplitWith("review")).toBe(true);
    expect(useWorkspacePanels.getState()).toMatchObject({
      activeId: "files",
      splitId: "review",
    });
    expect(s.setSplitWith("files")).toBe(false);
    expect(s.setSplitWith("missing")).toBe(false);

    s.syncBrowser({
      tabs: [
        {
          id: "one",
          url: "about:blank",
          title: "一个",
          loading: false,
          canGoBack: false,
          canGoForward: false,
          error: null,
        },
        {
          id: "two",
          url: "about:blank",
          title: "两个",
          loading: false,
          canGoBack: false,
          canGoForward: false,
          error: null,
        },
      ],
      activeTabId: "one",
      revision: 1,
    });
    s.select("browser:one");
    expect(s.setSplitWith("browser:two")).toBe(false);
  });
});
