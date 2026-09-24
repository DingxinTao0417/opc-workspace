import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getAgentProjectFiles } from "../api/agentProjectFiles";
import "./ProjectTaskFileSelect.css";
import type {
  AgentProjectFileSource,
  AgentRunFileCandidate,
} from "../types/models";

export function ProjectFileSourceDetails({
  source,
}: {
  source?: AgentProjectFileSource;
}) {
  return source ? (
    <span className="agent-project-file-source">
      来源任务「{source.task_title}」 · 任务版本 {source.task_version} · 第{" "}
      {source.submission_sequence} 批已验收 · Task <code>{source.task_id}</code>{" "}
      · Submission <code>{source.submission_id}</code> · 项目{" "}
      <code>{source.project_id}</code>
    </span>
  ) : null;
}
export function ProjectTaskFileSelect({
  taskId,
  projectId,
  selected,
  onChange,
  disabled,
  otherCount = 0,
  otherBytes = 0,
}: {
  taskId: string;
  projectId: string | null;
  selected: AgentRunFileCandidate[];
  onChange: (files: AgentRunFileCandidate[]) => void;
  disabled?: boolean;
  otherCount?: number;
  otherBytes?: number;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [offset, setOffset] = useState(0);
  useEffect(() => {
    setOpen(false);
    setSearch("");
    setQuery("");
    setOffset(0);
  }, [taskId, projectId]);
  useEffect(() => {
    if (search.trim() === query) return;
    const timer = setTimeout(() => {
      setQuery(search.trim());
      setOffset(0);
    }, 250);
    return () => clearTimeout(timer);
  }, [search, query]);
  const candidates = useQuery({
    queryKey: ["agent-project-task-files", taskId, projectId, query, offset],
    queryFn: ({ signal }) =>
      getAgentProjectFiles(taskId, offset, query, signal),
    enabled: open && !!projectId,
    retry: false,
    staleTime: 0,
  });
  const bytes =
    otherBytes + selected.reduce((sum, file) => sum + file.sizeBytes, 0);
  const identityInvalid = candidates.data?.items.some(
    (file) => file.sourceTask?.project_id !== projectId,
  );
  return (
    <fieldset
      className="agent-project-file-select"
      disabled={disabled}
      aria-label="跨任务已验收文件"
    >
      <legend>同项目其他任务的已验收文件</legend>
      <p>
        只选择其他已完成任务当前已验收批次中的有效文本文件；不会自动携带旧资料。与本任务文件和项目附件共用最多
        4 个、总计 128 KiB。来源已验收不代表本次任务已完成。
      </p>
      {selected.map((file) => (
        <article key={file.id}>
          <strong>{file.name}</strong> · {file.mime} · {file.sizeBytes} B ·
          SHA-256 <code>{file.sha256}</code>
          <br />
          <ProjectFileSourceDetails source={file.sourceTask} />
          <button
            type="button"
            onClick={() =>
              onChange(selected.filter((item) => item.id !== file.id))
            }
          >
            移除跨任务文件 {file.name}
          </button>
        </article>
      ))}
      {!projectId ? (
        <p>当前任务未归属项目，不能跨任务选择文件。</p>
      ) : (
        <button type="button" onClick={() => setOpen((value) => !value)}>
          {open ? "收起跨任务文件候选" : "选择跨任务已验收文件"}
        </button>
      )}
      {open && projectId ? (
        <>
          <label>
            搜索来源任务或文件
            <input
              value={search}
              maxLength={200}
              onChange={(event) => setSearch(event.target.value)}
            />
          </label>
          {candidates.isFetching ? (
            <p role="status">正在读取已验收文件候选…</p>
          ) : null}
          {candidates.isError || identityInvalid ? (
            <p role="alert">
              跨任务文件读取失败或项目来源不一致；不会替换已选来源。
              <button
                type="button"
                disabled={candidates.isFetching}
                onClick={() => void candidates.refetch()}
              >
                重读跨任务文件
              </button>
            </p>
          ) : null}
          <div className="agent-project-file-candidates">
            {!candidates.isError && !identityInvalid
              ? candidates.data?.items.map((file) => {
                  const checked = selected.some((item) => item.id === file.id);
                  const over =
                    otherCount + selected.length >= 4 ||
                    bytes + file.sizeBytes > 131072;
                  return (
                    <label
                      key={file.id}
                      className="agent-project-file-candidate"
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        disabled={
                          candidates.isFetching ||
                          search.trim() !== query ||
                          !file.eligible ||
                          (!checked && over)
                        }
                        onChange={(event) =>
                          onChange(
                            event.target.checked
                              ? [...selected, file]
                              : selected.filter((item) => item.id !== file.id),
                          )
                        }
                        aria-label={`选择跨任务文件 ${file.name}`}
                      />
                      <span>
                        {file.name} · {file.sizeBytes} B ·{" "}
                        <ProjectFileSourceDetails source={file.sourceTask} />
                        {!file.eligible
                          ? " · 当前文件不可用于执行"
                          : !checked && over
                            ? " · 已达到共同文件预算"
                            : ""}
                      </span>
                    </label>
                  );
                })
              : null}
          </div>
          {!candidates.isFetching &&
          !candidates.isError &&
          candidates.data?.items.length === 0 ? (
            <p>没有匹配的已验收文件。</p>
          ) : null}
          <nav aria-label="跨任务文件分页">
            <button
              type="button"
              disabled={candidates.isFetching || offset === 0}
              onClick={() => setOffset(Math.max(0, offset - 20))}
            >
              上一页文件
            </button>
            <span>
              第 {Math.floor(offset / 20) + 1} 页 ·{" "}
              {candidates.data?.total ?? 0} 项
            </span>
            <button
              type="button"
              disabled={
                candidates.isFetching || candidates.data?.nextOffset == null
              }
              onClick={() => setOffset(candidates.data!.nextOffset!)}
            >
              下一页文件
            </button>
          </nav>
        </>
      ) : null}
    </fieldset>
  );
}
