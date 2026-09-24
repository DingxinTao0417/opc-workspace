import {
  Fragment,
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent as ReactPointerEvent,
} from "react";
import {
  Bot,
  Columns2,
  FileText,
  FolderOpen,
  GitCompareArrows,
  Globe,
  LayoutDashboard,
  Maximize2,
  MessageSquare,
  Minimize2,
  PanelRightClose,
  Plus,
  TerminalSquare,
  X,
} from "lucide-react";
import { isDesktopRuntime } from "../api/desktop";
import { nativeBrowser } from "../api/browser";
import { workspaceApi, workspaceError } from "../api/workspace";
import { useUiStore, type RightPanelRequestMode } from "../store/ui";
import { useAiChatStore } from "../store/aiChat";
import {
  useWorkspacePanels,
  type WorkspacePanel,
  type WorkspacePanelKind,
} from "../store/workspacePanels";
import { EmbeddedBrowser } from "./EmbeddedBrowser";
import { RightFloatingCard } from "./RightFloatingCard";
import { AgentsTab, ManagedFilesTab } from "./WorkspaceResources";
import {
  WorkspaceFiles,
  WorkspaceFilePreview,
  WorkspaceReview,
} from "./WorkspaceFiles";
import { WorkspaceTerminal } from "./WorkspaceTerminal";
import { WorkspaceChat } from "./WorkspaceChat";
import { Modal } from "./Modal";
import { TaskArtifactWorkspacePreview } from "./TaskArtifactWorkspacePreview";
import { AiProjectFileReviewPreview } from "./AiProjectFileReview";
import "../workspace-panel.css";

const tools = [
  { kind: "review", title: "变更审查", icon: GitCompareArrows },
  { kind: "terminal", title: "终端", icon: TerminalSquare },
  { kind: "browser", title: "浏览器", icon: Globe },
  { kind: "files", title: "文件", icon: FolderOpen },
  { kind: "chat", title: "侧边聊天", icon: MessageSquare },
  { kind: "agents", title: "Agent 执行", icon: Bot },
  { kind: "managed", title: "任务产出与附件", icon: FileText },
  { kind: "overview", title: "环境信息", icon: LayoutDashboard },
] as const;
function panelIcon(kind: WorkspacePanelKind) {
  return tools.find((tool) => tool.kind === kind)?.icon ?? FileText;
}

function PanelContent({ panel }: { panel: WorkspacePanel }) {
  switch (panel.kind) {
    case "browser":
      return <EmbeddedBrowser hideTabs />;
    case "files":
      return <WorkspaceFiles panel={panel} />;
    case "file":
      return <WorkspaceFilePreview panel={panel} />;
    case "artifact":
      return <TaskArtifactWorkspacePreview panel={panel} />;
    case "ai-file-edit":
      return <AiProjectFileReviewPreview proposalId={panel.proposalId ?? ""} />;
    case "review":
      return <WorkspaceReview panel={panel} />;
    case "terminal":
      return <WorkspaceTerminal panel={panel} />;
    case "chat":
      return <WorkspaceChat panel={panel} />;
    case "agents":
      return (
        <div className="ws-resource">
          <AgentsTab />
        </div>
      );
    case "managed":
      return (
        <div className="ws-resource">
          <ManagedFilesTab />
        </div>
      );
    case "overview":
      return (
        <div className="ws-resource">
          <RightFloatingCard inline />
        </div>
      );
    default:
      return null;
  }
}

export function AgentWorkspace() {
  const state = useWorkspacePanels();
  const compatibilityTab = useUiStore((s) => s.rightPanelTab);
  const compatibilityRequestMode = useUiStore((s) => s.rightPanelRequestMode);
  const compatibilityRequest = useUiStore((s) => s.rightPanelRequest);
  const workspacePanelsRequest = useUiStore((s) => s.workspacePanelsRequest);
  const collapse = useUiStore((s) => s.toggleRightOverviewCollapsed);
  const [menu, setMenu] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [confirmClose, setConfirmClose] = useState<WorkspacePanel | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const plusRef = useRef<HTMLButtonElement>(null);
  const panesRef = useRef<HTMLDivElement>(null);
  const splitDrag = useRef<{
    pointerId: number;
    startX: number;
    startRatio: number;
    usableWidth: number;
  } | null>(null);
  const operation = useRef(false);
  const [resizingSplit, setResizingSplit] = useState(false);
  const active = state.tabs.find((tab) => tab.id === state.activeId);
  const secondary = state.tabs.find((tab) => tab.id === state.splitId);
  const splitStyle = {
    "--ws-left-share": `${state.splitRatio * 100}%`,
  } as CSSProperties;

  const handleSplitResizePointerDown = (
    event: ReactPointerEvent<HTMLDivElement>,
  ) => {
    if (event.button !== 0 || !secondary) return;
    const width = panesRef.current?.getBoundingClientRect().width ?? 0;
    const usableWidth = width - 12;
    if (usableWidth <= 0) return;
    event.preventDefault();
    splitDrag.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startRatio: state.splitRatio,
      usableWidth,
    };
    setResizingSplit(true);
    try {
      event.currentTarget.setPointerCapture(event.pointerId);
    } catch {
      // Pointer capture is optional in embedded WebViews and jsdom.
    }
  };

  const handleSplitResizePointerMove = (
    event: ReactPointerEvent<HTMLDivElement>,
  ) => {
    const drag = splitDrag.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    state.setSplitRatio(
      drag.startRatio + (event.clientX - drag.startX) / drag.usableWidth,
    );
  };

  const handleSplitResizePointerEnd = (
    event: ReactPointerEvent<HTMLDivElement>,
  ) => {
    if (splitDrag.current?.pointerId !== event.pointerId) return;
    splitDrag.current = null;
    setResizingSplit(false);
    try {
      if (event.currentTarget.hasPointerCapture(event.pointerId))
        event.currentTarget.releasePointerCapture(event.pointerId);
    } catch {
      // Releasing capture must not interrupt closing or switching the panel.
    }
  };

  const handleSplitResizeKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (!secondary) return;
    const step = event.shiftKey ? 0.1 : 0.025;
    const current = useWorkspacePanels.getState().splitRatio;
    const next =
      event.key === "ArrowLeft"
        ? current - step
        : event.key === "ArrowRight"
          ? current + step
          : event.key === "Home"
            ? 0.25
            : event.key === "End"
              ? 0.75
              : null;
    if (next === null) return;
    event.preventDefault();
    state.setSplitRatio(next);
  };

  const open = async (
    kind: WorkspacePanelKind,
    mode: RightPanelRequestMode = "open",
  ): Promise<string | null> => {
    if (operation.current) return null;
    operation.current = true;
    setMenu(false);
    setError("");
    setBusy(true);
    try {
      const tool = tools.find((item) => item.kind === kind);
      const panels = useWorkspacePanels.getState();
      const existing =
        mode === "activate"
          ? panels.tabs.find((tab) => tab.kind === kind)
          : undefined;
      if (existing) {
        panels.select(existing.id);
        return existing.id;
      }
      if (kind === "browser" && isDesktopRuntime()) {
        if (mode === "activate") {
          const snapshot = await nativeBrowser.snapshot();
          useWorkspacePanels.getState().syncBrowser(snapshot);
          const browserId = snapshot.activeTabId ?? snapshot.tabs[0]?.id;
          if (browserId) {
            const id = `browser:${browserId}`;
            useWorkspacePanels.getState().select(id);
            return id;
          }
        }
        const snapshot = await nativeBrowser.createTab();
        useWorkspacePanels.getState().syncBrowser(snapshot);
        if (snapshot.activeTabId) {
          const id = `browser:${snapshot.activeTabId}`;
          useWorkspacePanels.getState().select(id);
          return id;
        }
        return null;
      } else {
        const latestPanels = useWorkspacePanels.getState();
        return latestPanels.open({
          kind,
          title: tool?.title ?? "工作区",
          ...(latestPanels.root &&
          ["files", "review", "terminal"].includes(kind)
            ? { root: latestPanels.root }
            : {}),
        });
      }
    } catch (reason) {
      setError(workspaceError(reason));
      return null;
    } finally {
      operation.current = false;
      setBusy(false);
    }
  };
  useEffect(() => {
    const requestKey = `${compatibilityTab}:${compatibilityRequestMode}:${compatibilityRequest}`;
    if (
      compatibilityTab !== "summary" &&
      useWorkspacePanels.getState().handledRequest !== requestKey
    ) {
      useWorkspacePanels.setState({ handledRequest: requestKey });
      void open(compatibilityTab, compatibilityRequestMode);
    }
    // Compatibility requests from the floating status card/command entry points.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [compatibilityTab, compatibilityRequestMode, compatibilityRequest]);
  useEffect(() => {
    const request = workspacePanelsRequest;
    if (!request) return;
    const current = useWorkspacePanels.getState();
    if (current.handledLayoutRequest >= request.id) return;
    useWorkspacePanels.setState({ handledLayoutRequest: request.id });
    void (async () => {
      const [primary, companion] = request.panels;
      const primaryId = await open(primary, "activate");
      if (!primaryId) return;
      const companionId = await open(companion, "activate");
      if (!companionId || primaryId === companionId) return;
      const latest = useWorkspacePanels.getState();
      latest.select(primaryId);
      if (!latest.setSplitWith(companionId)) {
        setError("无法按请求并排显示这两个工作区标签。");
      } else if (request.splitRatio !== undefined) {
        latest.setSplitRatio(request.splitRatio);
      }
    })();
    // Requests are immutable and individually consumed; re-mounts compare the
    // persistent handledLayoutRequest marker to avoid replaying old UI actions.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspacePanelsRequest?.id]);
  useEffect(() => {
    if (!isDesktopRuntime()) return;
    let disposed = false;
    let unsubscribe: (() => void) | undefined;
    void (async () => {
      unsubscribe = await nativeBrowser.subscribe((snapshot) => {
        if (!disposed) useWorkspacePanels.getState().syncBrowser(snapshot);
      });
      if (disposed) {
        unsubscribe();
        return;
      }
      const snapshot = await nativeBrowser.snapshot();
      if (!disposed) useWorkspacePanels.getState().syncBrowser(snapshot);
    })().catch((reason) => {
      if (!disposed) setError(workspaceError(reason));
    });
    return () => {
      disposed = true;
      unsubscribe?.();
    };
  }, []);
  // Only the browser displayed in one of the panes can own the native surface.
  const browserId = active?.browserId ?? secondary?.browserId;
  useEffect(() => {
    if (browserId)
      void nativeBrowser
        .activateTab(browserId)
        .catch((reason) => setError(workspaceError(reason)));
  }, [browserId]);
  useEffect(() => {
    if (!menu) return;
    menuRef.current?.querySelector<HTMLButtonElement>("button")?.focus();
    const outside = (event: PointerEvent) => {
      if (
        !menuRef.current?.contains(event.target as Node) &&
        !plusRef.current?.contains(event.target as Node)
      )
        setMenu(false);
    };
    document.addEventListener("pointerdown", outside);
    return () => document.removeEventListener("pointerdown", outside);
  }, [menu]);

  const close = async (panel: WorkspacePanel, confirmed = false) => {
    const chat = useAiChatStore.getState();
    if (
      panel.kind === "chat" &&
      chat.streamOwner === `side:${panel.id}` &&
      chat.streaming
    ) {
      setError("侧边聊天仍在生成，请先停止生成再关闭标签。");
      return;
    }
    if (panel.terminalId && !confirmed) {
      setConfirmClose(panel);
      return;
    }
    setError("");
    try {
      if (panel.terminalId) await workspaceApi.terminalClose(panel.terminalId);
      if (panel.browserId)
        state.syncBrowser(await nativeBrowser.closeTab(panel.browserId));
      state.close(panel.id);
      setConfirmClose(null);
    } catch (reason) {
      setError(workspaceError(reason));
    }
  };
  const chooseRoot = async () => {
    setBusy(true);
    setError("");
    try {
      const root = await workspaceApi.chooseRoot();
      if (root) state.setRoot(root);
    } catch (reason) {
      setError(workspaceError(reason));
    } finally {
      setBusy(false);
    }
  };
  return (
    <aside
      className="right-sidebar agent-tool-workspace"
      id="right-overview"
      aria-label="智能体工作区"
      onKeyDown={(event) => {
        if (event.key === "Escape" && menu) {
          setMenu(false);
          plusRef.current?.focus();
        }
      }}
    >
      <div className="ws-tabbar">
        <div className="ws-tabs" role="tablist" aria-label="工作区标签页">
          {state.tabs.map((panel, index) => {
            const Icon = panelIcon(panel.kind);
            return (
              <div
                className={`ws-tab${panel.id === active?.id ? " is-active" : ""}`}
                key={panel.id}
              >
                <button
                  id={`ws-tab-${panel.id}`}
                  role="tab"
                  aria-selected={panel.id === active?.id}
                  aria-controls={`ws-pane-${panel.id}`}
                  tabIndex={panel.id === active?.id ? 0 : -1}
                  title={panel.title}
                  onClick={() => state.select(panel.id)}
                  onKeyDown={(event) => {
                    if (
                      ["ArrowLeft", "ArrowRight", "Home", "End"].includes(
                        event.key,
                      )
                    ) {
                      event.preventDefault();
                      const next =
                        event.key === "Home"
                          ? 0
                          : event.key === "End"
                            ? state.tabs.length - 1
                            : (index +
                                (event.key === "ArrowRight" ? 1 : -1) +
                                state.tabs.length) %
                              state.tabs.length;
                      state.select(state.tabs[next].id);
                      document
                        .getElementById(`ws-tab-${state.tabs[next].id}`)
                        ?.focus();
                    } else if (event.key === "Delete") {
                      event.preventDefault();
                      void close(panel);
                    }
                  }}
                >
                  <Icon size={14} />
                  <span>{panel.title}</span>
                </button>
                <button
                  aria-label={`关闭 ${panel.title}`}
                  className="ws-tab-close"
                  onClick={() => void close(panel)}
                >
                  <X size={12} />
                </button>
              </div>
            );
          })}
        </div>
        <button
          className="ws-icon"
          ref={plusRef}
          aria-label="新建工作区标签"
          aria-expanded={menu}
          aria-haspopup="menu"
          disabled={busy}
          onClick={() => setMenu((value) => !value)}
        >
          <Plus size={17} />
        </button>
        <div className="ws-tab-actions">
          <button
            className="ws-icon"
            aria-label={state.splitId ? "取消分栏" : "分栏显示"}
            title="将另一个标签并排显示"
            aria-pressed={!!state.splitId}
            disabled={state.tabs.length < 2}
            onClick={state.split}
          >
            <Columns2 size={15} />
          </button>
          <button
            className="ws-icon"
            aria-label={state.maximized ? "还原工作区" : "放大工作区"}
            onClick={state.toggleMaximized}
          >
            {state.maximized ? (
              <Minimize2 size={15} />
            ) : (
              <Maximize2 size={15} />
            )}
          </button>
          <button
            className="ws-icon"
            aria-label="收起右侧工作区"
            onClick={collapse}
          >
            <PanelRightClose size={15} />
          </button>
        </div>
      </div>
      {menu && (
        <div
          className="ws-new-menu"
          ref={menuRef}
          role="menu"
          aria-label="新建标签类型"
          data-overlay-root="true"
          onKeyDown={(event) => {
            if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
              event.preventDefault();
              const items = Array.from(
                menuRef.current!.querySelectorAll("button"),
              );
              const index = items.indexOf(
                document.activeElement as HTMLButtonElement,
              );
              const next =
                event.key === "Home"
                  ? 0
                  : event.key === "End"
                    ? items.length - 1
                    : (index +
                        (event.key === "ArrowDown" ? 1 : -1) +
                        items.length) %
                      items.length;
              items[next]?.focus();
            }
          }}
        >
          {tools.map((tool) => (
            <button
              role="menuitem"
              key={tool.kind}
              onClick={() => void open(tool.kind)}
            >
              <tool.icon size={16} />
              <span>{tool.title}</span>
            </button>
          ))}
        </div>
      )}
      <div className="ws-rootbar">
        <button
          title={state.root?.path ?? "授权读取本地目录"}
          disabled={busy || !isDesktopRuntime()}
          onClick={() => void chooseRoot()}
        >
          <FolderOpen size={14} />
          <span>{state.root?.name ?? "选择工作目录"}</span>
        </button>
        <span>本地 · {isDesktopRuntime() ? "桌面工作区" : "网页预览"}</span>
      </div>
      {error && (
        <div className="ws-error" role="alert">
          {error}
          <button aria-label="关闭工作区错误提示" onClick={() => setError("")}>
            <X size={13} />
          </button>
        </div>
      )}
      <div
        className={`ws-panes${secondary ? " is-split" : ""}${resizingSplit ? " is-resizing" : ""}`}
        ref={panesRef}
        style={splitStyle}
      >
        {active ? (
          [
            active,
            ...(secondary && secondary.id !== active.id ? [secondary] : []),
          ].map((panel, index) => (
            <Fragment key={panel.id}>
              {index === 1 && secondary && (
                <div
                  aria-controls={`ws-pane-${active.id} ws-pane-${secondary.id}`}
                  aria-label="调整工作区分栏比例"
                  aria-orientation="vertical"
                  aria-valuemax={75}
                  aria-valuemin={25}
                  aria-valuenow={Math.round(state.splitRatio * 100)}
                  aria-valuetext={`主面板 ${Math.round(state.splitRatio * 100)}%，次面板 ${100 - Math.round(state.splitRatio * 100)}%`}
                  className="ws-split-resizer"
                  data-dragging={resizingSplit ? "true" : undefined}
                  onDoubleClick={() => state.setSplitRatio(0.5)}
                  onKeyDown={handleSplitResizeKeyDown}
                  onPointerCancel={handleSplitResizePointerEnd}
                  onPointerDown={handleSplitResizePointerDown}
                  onPointerMove={handleSplitResizePointerMove}
                  onPointerUp={handleSplitResizePointerEnd}
                  role="separator"
                  tabIndex={0}
                  title="拖动调整宽度，双击恢复均分"
                />
              )}
              <section
                id={`ws-pane-${panel.id}`}
                role="tabpanel"
                aria-labelledby={`ws-tab-${panel.id}`}
                className="ws-pane"
              >
                {secondary && (
                  <div className="ws-pane-title">{panel.title}</div>
                )}
                <PanelContent panel={panel} />
              </section>
            </Fragment>
          ))
        ) : (
          <div className="ws-empty ws-welcome">
            <div className="ws-welcome-symbol">
              <Columns2 size={26} />
            </div>
            <h2>你的工作区</h2>
            <p>对话留在左侧，文件、网页与工具在这里打开。</p>
            <div className="ws-launchers">
              {tools.slice(0, 6).map((tool) => (
                <button
                  key={tool.kind}
                  onClick={() => void open(tool.kind)}
                  disabled={busy}
                >
                  <tool.icon size={20} />
                  <span>{tool.title}</span>
                </button>
              ))}
            </div>
            <small>使用 + 新建标签，也可以将两个不同的工具分栏显示。</small>
          </div>
        )}
      </div>
      {busy && (
        <div className="ws-busy" role="status" data-overlay-root="true">
          正在打开…
        </div>
      )}
      {confirmClose && (
        <Modal
          open
          onClose={() => setConfirmClose(null)}
          title="关闭终端确认"
          footer={
            <>
              <button
                className="button button-secondary"
                onClick={() => setConfirmClose(null)}
              >
                保留终端
              </button>
              <button
                className="button button-danger"
                onClick={() => void close(confirmClose, true)}
              >
                终止并关闭
              </button>
            </>
          }
        >
          <p>终端及其运行中的子进程将被终止，尚未保存的进程状态无法恢复。</p>
        </Modal>
      )}
    </aside>
  );
}
