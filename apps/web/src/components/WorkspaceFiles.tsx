import { useQuery } from "@tanstack/react-query";
import { ArrowUp, FileText, Folder, RefreshCw } from "lucide-react";
import { useState } from "react";
import { workspaceApi, workspaceError } from "../api/workspace";
import { filePreviewHandoff, gitReviewHandoff } from "../lib/aiIssueHandoff";
import {
  useWorkspacePanels,
  type WorkspacePanel,
} from "../store/workspacePanels";
import { AiIssueHandoffButton } from "./AiWorkbenchHandoff";
import { useAiProjectFiles } from "../store/aiProjectFiles";
import { revealAgentChat } from "../lib/revealAgentChat";
import { useWorkspaceFileChanges } from "../store/workspaceFileChanges";
import { WorkspaceFileRecovery } from "./WorkspaceFileRecovery";
import { useAgentRunFileApply } from "../store/agentRunFileApply";
import { AgentRunFileApplyReview } from "./AgentRunFileApplyReview";

export function WorkspaceFiles({ panel }: { panel: WorkspacePanel }) {
  const fileRevision = useWorkspaceFileChanges((s) => s.revision);
  const [recoveryOpen, setRecoveryOpen] = useState(false);
  const currentRoot = useWorkspacePanels((s) => s.root);
  const root = panel.root ?? currentRoot;
  const open = useWorkspacePanels((s) => s.open);
  const [path, setPath] = useState("");
  const [filter, setFilter] = useState("");
  const [openError, setOpenError] = useState("");
  const query = useQuery({
    queryKey: ["workspace-files", root?.id, path, fileRevision],
    queryFn: () => workspaceApi.list(root!.id, path),
    enabled: !!root,
    retry: false,
  });
  if (!root)
    return (
      <div className="ws-empty">
        <Folder size={32} />
        <h3>选择工作目录</h3>
        <p>
          点击上方文件夹按钮，授权读取目录内的文件。内容不会自动进入 AI 上下文。
        </p>
      </div>
    );
  return (
    <div className="ws-files">
      <div className="ws-toolbar">
        <button
          aria-label="返回上级目录"
          disabled={!path}
          onClick={() => setPath(path.split("/").slice(0, -1).join("/"))}
        >
          <ArrowUp size={15} />
        </button>
        <span title={`${root.path}/${path}`}>
          {root.name}
          {path ? ` / ${path}` : ""}
        </span>
        <button aria-label="刷新文件" onClick={() => void query.refetch()}>
          <RefreshCw size={15} />
        </button>
        <button type="button" onClick={() => setRecoveryOpen((v) => !v)}>
          文件恢复
        </button>
      </div>
      {recoveryOpen && <WorkspaceFileRecovery key={root.id} root={root} />}
      <input
        className="ws-search"
        aria-label="筛选当前目录"
        placeholder="筛选当前目录…"
        value={filter}
        onChange={(event) => setFilter(event.target.value)}
      />
      {openError && (
        <p className="ws-error" role="alert">
          {openError}
        </p>
      )}
      {query.isPending ? (
        <p className="ws-hint">正在读取…</p>
      ) : query.isError ? (
        <p className="ws-error" role="alert">
          {workspaceError(query.error)}
        </p>
      ) : (
        <div className="ws-file-list">
          {query.data.entries
            .filter((file) =>
              file.name.toLowerCase().includes(filter.toLowerCase()),
            )
            .map((file) => (
              <button
                key={file.path}
                onClick={() => {
                  if (file.directory) {
                    setPath(file.path);
                    setFilter("");
                  } else {
                    setOpenError("");
                    try {
                      open(
                        {
                          kind: "file",
                          title: file.name,
                          root,
                          path: file.path,
                        },
                        `file:${root.id}:${file.path}`,
                      );
                    } catch (reason) {
                      setOpenError(workspaceError(reason));
                    }
                  }
                }}
              >
                {file.directory ? <Folder size={16} /> : <FileText size={16} />}
                <span>{file.name}</span>
                <small>
                  {file.directory
                    ? "文件夹"
                    : `${Math.ceil(file.size / 1024)} KB`}
                </small>
              </button>
            ))}
          {!query.data.entries.length && (
            <p className="ws-hint">这个目录是空的</p>
          )}
          {query.data.truncated && (
            <p className="ws-hint">目录较大，仅显示前 1,000 项。</p>
          )}
        </div>
      )}
    </div>
  );
}

export function WorkspaceFilePreview({ panel }: { panel: WorkspacePanel }) {
  const fileRevision = useWorkspaceFileChanges((s) => s.revision);
  const agentRunCandidate = useAgentRunFileApply((s) => s.candidate);
  const fileSelection = useAiProjectFiles((s) => s.selection);
  const fileLoading = useAiProjectFiles((s) => s.loading);
  const query = useQuery({
    queryKey: ["workspace-preview", panel.root?.id, panel.path, fileRevision],
    queryFn: () => workspaceApi.preview(panel.root!.id, panel.path!),
    retry: false,
  });
  const lines =
    query.data?.kind === "text" ? query.data.content.split("\n", 10_001) : [];
  const alreadySelected =
    !!fileSelection &&
    fileSelection.rootId === panel.root?.id &&
    fileSelection.files.some(
      ({ snapshot }) =>
        snapshot.path.toLowerCase() === panel.path?.toLowerCase(),
    );
  const selectedCount =
    fileSelection && fileSelection.rootId === panel.root?.id
      ? fileSelection.files.length
      : 0;
  const selectionLimitReached =
    !!fileSelection &&
    fileSelection.rootId === panel.root?.id &&
    selectedCount >= 4 &&
    !alreadySelected;
  return (
    <div className="ws-preview">
      <div className="ws-toolbar">
        <span title={panel.path}>{panel.path}</span>
        <small>只读</small>
        {query.data?.kind === "text" && panel.root && panel.path && (
          <button
            type="button"
            className="button button-quiet"
            title="明确加入本条消息的完整文件上下文；完整内容发送前会再次核对，不会自动发送或写入"
            disabled={fileLoading || selectionLimitReached}
            onClick={() => {
              revealAgentChat();
              void useAiProjectFiles.getState().stage(panel.root!, panel.path!);
            }}
          >
            {alreadySelected
              ? "重新读取已选文件基线"
              : "加入本条消息文件上下文 (" + selectedCount + "/4)"}
          </button>
        )}
        <AiIssueHandoffButton
          content={
            query.data?.kind === "text"
              ? filePreviewHandoff({
                  fileName: panel.title,
                  content: query.data.content,
                  sizeBytes: query.data.size,
                  previewTruncated: query.data.truncated,
                })
              : null
          }
          disabled={query.isFetching}
          label="交给智能体分析"
        />
        <button aria-label="刷新预览" onClick={() => void query.refetch()}>
          <RefreshCw size={15} />
        </button>
      </div>
      {agentRunCandidate ? (
        <AgentRunFileApplyReview
          panel={panel}
          targetIsText={query.data?.kind === "text" && !query.data.truncated}
        />
      ) : null}
      {query.isPending ? (
        <p className="ws-hint">正在读取预览…</p>
      ) : query.isError ? (
        <div className="ws-error" role="alert">
          {workspaceError(query.error)}
        </div>
      ) : (
        <>
          {query.data.truncated && (
            <p className="ws-hint">文件超过 2 MiB，仅预览开头部分。</p>
          )}
          {lines.length > 10_000 && (
            <p className="ws-hint">文件行数较多，仅显示前 10,000 行。</p>
          )}
          {query.data.kind === "image" ? (
            <div className="ws-image">
              <img src={query.data.content} alt={panel.title} />
            </div>
          ) : (
            <pre className="ws-source">
              {lines.slice(0, 10_000).map((line, i) => (
                <div key={i}>
                  <span className="ws-line-number" aria-hidden="true">
                    {i + 1}
                  </span>
                  <code>{line || " "}</code>
                </div>
              ))}
            </pre>
          )}
        </>
      )}
    </div>
  );
}

export function WorkspaceReview({ panel }: { panel: WorkspacePanel }) {
  const fileRevision = useWorkspaceFileChanges((s) => s.revision);
  const currentRoot = useWorkspacePanels((s) => s.root);
  const root = panel.root ?? currentRoot;
  const [staged, setStaged] = useState(false);
  const query = useQuery({
    queryKey: ["workspace-review", root?.id, staged, fileRevision],
    queryFn: () => workspaceApi.review(root!.id, staged),
    enabled: !!root,
    retry: false,
  });
  const lines = query.data?.diff.split("\n", 10_001) ?? [];
  if (!root)
    return (
      <div className="ws-empty">
        <h3>Git 变更审查</h3>
        <p>通过上方文件夹按钮选择仓库根目录。</p>
      </div>
    );
  return (
    <div className="ws-review">
      <div className="ws-toolbar">
        <span>
          {root.name} {query.data?.branch && ` · ${query.data.branch}`}
        </span>
        <AiIssueHandoffButton
          content={
            query.data
              ? gitReviewHandoff({
                  rootName: root.name,
                  branch: query.data.branch,
                  staged,
                  status: query.data.status,
                  diff: query.data.diff,
                })
              : null
          }
          disabled={query.isFetching}
          label="交给智能体审查"
        />
        <button aria-label="刷新变更" onClick={() => void query.refetch()}>
          <RefreshCw size={15} />
        </button>
      </div>
      <div className="ws-switch">
        <button aria-pressed={!staged} onClick={() => setStaged(false)}>
          未暂存
        </button>
        <button aria-pressed={staged} onClick={() => setStaged(true)}>
          已暂存
        </button>
        <small>只读，不执行提交或推送</small>
      </div>
      {query.isPending ? (
        <p className="ws-hint">正在读取 Git…</p>
      ) : query.isError ? (
        <p className="ws-error" role="alert">
          {workspaceError(query.error)}
        </p>
      ) : (
        <div className="ws-review-scroll">
          <details open className="ws-git-status">
            <summary>变更文件</summary>
            <pre>{query.data.status || "工作区干净"}</pre>
          </details>
          <pre className="ws-diff">
            {query.data.diff
              ? lines.slice(0, 10_000).map((line, i) => (
                  <div
                    key={i}
                    className={
                      line.startsWith("+")
                        ? "is-added"
                        : line.startsWith("-")
                          ? "is-removed"
                          : line.startsWith("@@")
                            ? "is-hunk"
                            : ""
                    }
                  >
                    {line || " "}
                  </div>
                ))
              : "此范围没有文本差异。未跟踪文件可在文件标签中预览。"}
          </pre>
          {lines.length > 10_000 && (
            <p className="ws-hint">差异行数较多，仅显示前 10,000 行。</p>
          )}
        </div>
      )}
    </div>
  );
}
