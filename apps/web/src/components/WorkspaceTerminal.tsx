import { useEffect, useRef, useState } from "react";
import { TerminalSquare } from "lucide-react";
import { workspaceApi, workspaceError } from "../api/workspace";
import { terminalOutputHandoff } from "../lib/aiIssueHandoff";
import {
  useWorkspacePanels,
  type WorkspacePanel,
} from "../store/workspacePanels";
import { AiIssueHandoffButton } from "./AiWorkbenchHandoff";
import "@xterm/xterm/css/xterm.css";

const terminalSnapshotLimit = 16 * 1024;

function utf8Tail(value: string, limit: number) {
  const bytes = new TextEncoder().encode(value);
  if (bytes.byteLength <= limit) return { value, truncated: false };
  let start = bytes.byteLength - limit;
  while (start < bytes.byteLength && (bytes[start] & 0xc0) === 0x80) start += 1;
  return {
    value: new TextDecoder().decode(bytes.slice(start)),
    truncated: true,
  };
}

export function WorkspaceTerminal({ panel }: { panel: WorkspacePanel }) {
  const currentRoot = useWorkspacePanels((s) => s.root);
  const root = panel.root ?? currentRoot;
  const update = useWorkspacePanels((s) => s.update);
  const container = useRef<HTMLDivElement>(null);
  const [error, setError] = useState("");
  const [starting, setStarting] = useState(false);
  const [ended, setEnded] = useState(false);
  const [terminalOutput, setTerminalOutput] = useState({
    terminalId: "",
    content: "",
    capturedTruncated: false,
    nativeBufferDropped: false,
  });
  const start = async () => {
    if (!root || starting) return;
    setStarting(true);
    setError("");
    try {
      const terminalId = await workspaceApi.terminalStart(root.id);
      // Closing a tab while native spawn is in flight must not orphan a shell.
      if (
        !useWorkspacePanels.getState().tabs.some((tab) => tab.id === panel.id)
      ) {
        await workspaceApi.terminalClose(terminalId);
        return;
      }
      update(panel.id, { terminalId, root, title: `终端 · ${root.name}` });
    } catch (reason) {
      setError(workspaceError(reason));
    } finally {
      setStarting(false);
    }
  };
  useEffect(() => {
    if (!panel.terminalId || !container.current) return;
    const id = panel.terminalId;
    setTerminalOutput({
      terminalId: id,
      content: "",
      capturedTruncated: false,
      nativeBufferDropped: false,
    });
    let disposed = false;
    let cleanup: (() => void) | undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;
    void (async () => {
      const [{ Terminal }, { FitAddon }] = await Promise.all([
        import("@xterm/xterm"),
        import("@xterm/addon-fit"),
      ]);
      if (disposed || !container.current) return;
      const term = new Terminal({
        fontSize: 13,
        fontFamily: 'Consolas, "Cascadia Mono", monospace',
        scrollback: 3000,
        cursorBlink: true,
        allowProposedApi: false,
        theme: { background: "#191a1c", foreground: "#e5e5e5" },
      });
      const fit = new FitAddon();
      term.loadAddon(fit);
      term.open(container.current);
      let writes = Promise.resolve();
      let queuedBytes = 0;
      const input = term.onData((data) => {
        const bytes = new TextEncoder().encode(data).length;
        if (bytes > 16 * 1024 || queuedBytes + bytes > 64 * 1024) {
          setError("终端输入过大或仍在处理，请等待后分段输入。");
          return;
        }
        queuedBytes += bytes;
        writes = writes
          .then(() => workspaceApi.terminalWrite(id, data))
          .catch((reason) => {
            if (!disposed) setError(workspaceError(reason));
          })
          .finally(() => {
            queuedBytes -= bytes;
          });
      });
      const resize = () => {
        if (
          disposed ||
          !container.current?.clientWidth ||
          !container.current.clientHeight
        )
          return;
        fit.fit();
        void workspaceApi
          .terminalResize(
            id,
            Math.max(10, Math.min(400, term.cols)),
            Math.max(2, Math.min(200, term.rows)),
          )
          .catch((reason) => {
            if (!disposed) setError(workspaceError(reason));
          });
      };
      const observer = new ResizeObserver(resize);
      observer.observe(container.current);
      resize();
      let offset = 0;
      const appendSnapshotOutput = (value: string, nativeDropped = false) => {
        setTerminalOutput((previous) => {
          const bounded = utf8Tail(
            previous.content + value,
            terminalSnapshotLimit,
          );
          return {
            terminalId: id,
            content: bounded.value,
            capturedTruncated: previous.capturedTruncated || bounded.truncated,
            nativeBufferDropped: previous.nativeBufferDropped || nativeDropped,
          };
        });
      };
      const poll = async () => {
        try {
          const output = await workspaceApi.terminalRead(id, offset);
          if (disposed) return;
          offset = output.nextOffset;
          if (output.dropped) {
            const notice = "\r\n[较早的输出已超出 1 MiB 缓冲范围]\r\n";
            term.write(notice);
            appendSnapshotOutput(notice, true);
          }
          if (output.data) {
            const bytes = Uint8Array.from(atob(output.data), (char) =>
              char.charCodeAt(0),
            );
            appendSnapshotOutput(new TextDecoder().decode(bytes));
            await new Promise<void>((resolve) => term.write(bytes, resolve));
          }
          if (disposed) return;
          if (output.ended && !output.data) {
            setEnded(true);
            term.options.disableStdin = true;
            return;
          }
          timer = setTimeout(() => void poll(), output.data ? 30 : 200);
        } catch (reason) {
          if (!disposed) setError(workspaceError(reason));
        }
      };
      cleanup = () => {
        observer.disconnect();
        input.dispose();
        term.dispose();
      };
      void poll();
    })().catch((reason) => {
      if (!disposed) setError(workspaceError(reason));
    });
    return () => {
      disposed = true;
      if (timer) clearTimeout(timer);
      cleanup?.();
    };
  }, [panel.terminalId]);
  return (
    <div className="ws-terminal">
      {panel.terminalId ? (
        <>
          <div className="ws-toolbar">
            <span>PowerShell · {root?.name}</span>
            <AiIssueHandoffButton
              content={
                terminalOutput.terminalId === panel.terminalId
                  ? terminalOutputHandoff(terminalOutput)
                  : null
              }
              label="交给智能体分析"
            />
            <small>{ended ? "已退出" : "手动输入 · AI 不可控制终端"}</small>
          </div>
          <div className="ws-terminal-surface" ref={container} />
        </>
      ) : (
        <div className="ws-empty">
          <TerminalSquare size={34} />
          <h3>手动终端</h3>
          <p>
            以当前用户权限运行 PowerShell，可访问所选目录之外的文件与网络。AI
            不能输入命令，输出不会自动发送给模型。
          </p>
          <p>关闭标签会终止该终端及其子进程；收起面板不会停止。</p>
          <button
            className="ws-primary"
            disabled={!root || starting}
            onClick={() => void start()}
          >
            {starting
              ? "正在启动…"
              : root
                ? `启动终端 · ${root.name}`
                : "请先选择工作目录"}
          </button>
        </div>
      )}
      {error && (
        <p className="ws-error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
