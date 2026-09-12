import {
  BookOpenText,
  CircleStop,
  Database,
  Download,
  FileText,
  HardDrive,
  LoaderCircle,
  LockKeyhole,
  RefreshCw,
  Search,
  ShieldCheck,
  Trash2,
  Upload,
} from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import {
  ApiError,
  cancelKnowledgeIndexJob,
  createKnowledgeSource,
  deleteKnowledgeSource,
  downloadKnowledgeSourcesCSV,
  getKnowledgeSources,
  reindexKnowledgeSource,
  retryKnowledgeIndexJob,
  searchKnowledge,
} from "../api/client";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { Modal } from "../components/Modal";
import { PageHeader } from "../components/PageHeader";
import type {
  KnowledgeHighlight,
  KnowledgeSearchResult,
  KnowledgeSource,
} from "../types/models";
import "./knowledge-base-page.css";

const sourceStatus: Record<KnowledgeSource["status"], string> = {
  pending: "待索引",
  indexing: "索引中",
  ready: "可检索",
  stale: "需更新",
  missing: "来源缺失",
  failed: "索引失败",
  deleted: "已删除",
};

const indexStage: Record<
  NonNullable<KnowledgeSource["latestJob"]>["stage"],
  string
> = {
  queued: "等待处理",
  extracting: "提取文本",
  chunking: "生成分段",
  indexing: "写入索引",
  complete: "已完成",
};

function knowledgeSourceTypeLabel(sourceType: KnowledgeSource["sourceType"]) {
  if (sourceType === "pdf") return "PDF";
  if (sourceType === "markdown") return "Markdown";
  return "TXT";
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MiB`;
}

function formatDate(value: string | null) {
  if (!value) return "尚未索引";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function errorMessage(error: unknown) {
  if (!(error instanceof Error)) return "操作失败，请稍后重试。";
  if (error instanceof ApiError && error.requestId) {
    return `${error.message} · 请求 ID：${error.requestId}`;
  }
  return error.message;
}

function HighlightedExcerpt({
  text,
  highlights,
}: {
  text: string;
  highlights: KnowledgeHighlight[];
}) {
  const characters = Array.from(text);
  const parts: ReactNode[] = [];
  let cursor = 0;
  highlights.forEach((range, index) => {
    const start = Math.max(cursor, Math.min(range.start, characters.length));
    const end = Math.max(start, Math.min(range.end, characters.length));
    if (start > cursor) {
      parts.push(
        <span key={`text-${index}`}>
          {characters.slice(cursor, start).join("")}
        </span>,
      );
    }
    if (end > start) {
      parts.push(
        <mark key={`mark-${index}`}>
          {characters.slice(start, end).join("")}
        </mark>,
      );
    }
    cursor = end;
  });
  if (cursor < characters.length) {
    parts.push(<span key="text-end">{characters.slice(cursor).join("")}</span>);
  }
  return <>{parts.length ? parts : text}</>;
}

function SourceCard({
  source,
  selected,
  busy,
  onToggle,
  onReindex,
  onCancel,
  onRetry,
  onDelete,
}: {
  source: KnowledgeSource;
  selected: boolean;
  busy: boolean;
  onToggle: () => void;
  onReindex: () => void;
  onCancel: () => void;
  onRetry: () => void;
  onDelete: () => void;
}) {
  const activeJob =
    source.latestJob?.status === "queued" ||
    source.latestJob?.status === "running"
      ? source.latestJob
      : null;
  const retryableJob =
    source.latestJob?.status === "failed" ||
    source.latestJob?.status === "cancelled"
      ? source.latestJob
      : null;
  return (
    <article
      className={`knowledge-source-card${selected ? " is-selected" : ""}`}
    >
      <button
        aria-label={`${selected ? "取消筛选" : "筛选"}来源 ${source.name}`}
        aria-pressed={selected}
        className="knowledge-source-main"
        disabled={source.status !== "ready" && source.documentId === null}
        onClick={onToggle}
        type="button"
      >
        <span className="knowledge-source-icon" aria-hidden="true">
          <FileText size={17} />
        </span>
        <span className="knowledge-source-copy">
          <strong title={source.name}>{source.name}</strong>
          <span>
            {formatBytes(source.sizeBytes)} ·{" "}
            {knowledgeSourceTypeLabel(source.sourceType)} · {source.chunkCount}{" "}
            个分段
          </span>
        </span>
        <span className={`knowledge-status knowledge-status-${source.status}`}>
          {sourceStatus[source.status]}
          {activeJob ? ` ${activeJob.progress}%` : ""}
        </span>
      </button>
      <div className="knowledge-source-footer">
        <span>
          {activeJob
            ? `${indexStage[activeJob.stage]} · attempt ${activeJob.attempt}`
            : retryableJob
              ? `${retryableJob.errorCode ?? sourceStatus[source.status]} · attempt ${retryableJob.attempt}`
              : `v${source.documentVersion ?? "—"} · ${formatDate(source.lastIndexedAt)}`}
        </span>
        <span className="knowledge-source-actions">
          {activeJob ? (
            <button
              aria-label={`取消索引 ${source.name}`}
              className="icon-button"
              disabled={busy}
              onClick={onCancel}
              title="取消当前索引"
              type="button"
            >
              <CircleStop size={14} />
            </button>
          ) : (
            <button
              aria-label={`${retryableJob ? "重试索引" : "重建索引"} ${source.name}`}
              className="icon-button"
              disabled={busy || source.status === "deleted"}
              onClick={retryableJob ? onRetry : onReindex}
              title={retryableJob ? "重试失败的索引" : "重建索引"}
              type="button"
            >
              <RefreshCw className={busy ? "animate-spin" : ""} size={14} />
            </button>
          )}
          <button
            aria-label={`删除来源 ${source.name}`}
            className="icon-button icon-button-danger"
            disabled={busy}
            onClick={onDelete}
            title="删除来源及索引"
            type="button"
          >
            <Trash2 size={14} />
          </button>
        </span>
      </div>
    </article>
  );
}

function SearchResultCard({ result }: { result: KnowledgeSearchResult }) {
  return (
    <article className="knowledge-result-card">
      <div className="knowledge-result-heading">
        <span className="knowledge-result-source">
          <FileText aria-hidden="true" size={15} />
          <strong>{result.sourceName}</strong>
        </span>
        <span className="knowledge-result-location">
          {result.sourceType === "pdf"
            ? `第 ${result.startPage}–${result.endPage} 页 · `
            : ""}
          第 {result.startLine}–{result.endLine} 行 · 文档 v
          {result.documentVersion}
        </span>
      </div>
      <p className="knowledge-result-excerpt">
        <HighlightedExcerpt
          highlights={result.highlights}
          text={result.excerpt}
        />
      </p>
      <div className="knowledge-result-footer">
        <span>分段 {result.chunkIndex + 1}</span>
        <span>
          字符 {result.startChar}–{result.endChar}
        </span>
      </div>
    </article>
  );
}

export function KnowledgeBasePage() {
  const queryClient = useQueryClient();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState("");
  const [selectedSourceIDs, setSelectedSourceIDs] = useState<string[]>([]);
  const [notice, setNotice] = useState<string | null>(null);
  const [operationError, setOperationError] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<KnowledgeSource | null>(
    null,
  );

  const sourcesQuery = useQuery({
    queryKey: ["knowledge", "sources"],
    queryFn: () => getKnowledgeSources({ pageSize: 100 }),
    refetchInterval: (queryState) =>
      queryState.state.data?.items.some(
        (source) => source.status === "indexing",
      )
        ? 750
        : false,
  });
  const importMutation = useMutation({
    mutationFn: (file: File) => createKnowledgeSource(file),
    onSuccess: async (result) => {
      setOperationError(null);
      setNotice(
        `已将 ${result.source.name} 加入本地索引队列；完成前不会出现在检索结果中。`,
      );
      await queryClient.invalidateQueries({
        queryKey: ["knowledge", "sources"],
      });
    },
    onError: (error) => {
      setNotice(null);
      setOperationError(errorMessage(error));
    },
  });
  const reindexMutation = useMutation({
    mutationFn: (source: KnowledgeSource) =>
      reindexKnowledgeSource(source.id, source.version),
    onSuccess: async (result) => {
      setOperationError(null);
      setNotice(
        `已开始重建 ${result.source.name}；旧文档在新索引原子发布前保持可解释。`,
      );
      await queryClient.invalidateQueries({
        queryKey: ["knowledge", "sources"],
      });
    },
    onError: (error) => {
      setNotice(null);
      setOperationError(errorMessage(error));
    },
  });
  const deleteMutation = useMutation({
    mutationFn: (source: KnowledgeSource) =>
      deleteKnowledgeSource(source.id, source.version),
    onSuccess: async (_, source) => {
      setPendingDelete(null);
      setSelectedSourceIDs((current) =>
        current.filter((id) => id !== source.id),
      );
      setNotice(`已删除 ${source.name} 的受控副本、派生文本和全部索引。`);
      setOperationError(null);
      await queryClient.invalidateQueries({
        queryKey: ["knowledge", "sources"],
      });
    },
    onError: (error) => setOperationError(errorMessage(error)),
  });
  const cancelIndexMutation = useMutation({
    mutationFn: (source: KnowledgeSource) => {
      if (!source.latestJob) {
        throw new Error("当前来源没有可取消的索引任务。");
      }
      return cancelKnowledgeIndexJob(source.latestJob.id);
    },
    onSuccess: async (_, source) => {
      setNotice(`已取消 ${source.name} 的当前索引任务。`);
      setOperationError(null);
      await queryClient.invalidateQueries({
        queryKey: ["knowledge", "sources"],
      });
    },
    onError: async (error) => {
      setNotice(null);
      setOperationError(errorMessage(error));
      await queryClient.invalidateQueries({
        queryKey: ["knowledge", "sources"],
      });
    },
  });
  const retryIndexMutation = useMutation({
    mutationFn: (source: KnowledgeSource) => {
      if (!source.latestJob) {
        throw new Error("当前来源没有可重试的索引任务。");
      }
      return retryKnowledgeIndexJob(source.latestJob.id, source.version);
    },
    onSuccess: async (result) => {
      setNotice(`已将 ${result.source.name} 的新 attempt 加入本地索引队列。`);
      setOperationError(null);
      await queryClient.invalidateQueries({
        queryKey: ["knowledge", "sources"],
      });
    },
    onError: async (error) => {
      setNotice(null);
      setOperationError(errorMessage(error));
      await queryClient.invalidateQueries({
        queryKey: ["knowledge", "sources"],
      });
    },
  });
  const exportMutation = useMutation({
    mutationFn: downloadKnowledgeSourcesCSV,
    onSuccess: (result) => {
      if (typeof URL.createObjectURL !== "function") {
        setOperationError("当前环境无法保存知识库来源清单。");
        return;
      }
      const url = URL.createObjectURL(result.blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = result.fileName;
      anchor.click();
      URL.revokeObjectURL(url);
      setNotice(`已导出来源与索引状态清单：${result.fileName}`);
      setOperationError(null);
    },
    onError: (error) => {
      setNotice(null);
      setOperationError(errorMessage(error));
    },
  });
  const searchMutation = useMutation({
    mutationFn: ({ text, sourceIDs }: { text: string; sourceIDs: string[] }) =>
      searchKnowledge(text, sourceIDs),
    onError: (error) => setOperationError(errorMessage(error)),
    onSuccess: () => setOperationError(null),
  });

  const sources = sourcesQuery.data?.items ?? [];
  const readySources = sources.filter(
    (source) =>
      source.status === "ready" ||
      (source.status === "indexing" && source.documentId !== null),
  );
  const stats = useMemo(
    () => ({
      sourceCount: readySources.length,
      chunkCount: readySources.reduce(
        (total, source) => total + source.chunkCount,
        0,
      ),
      bytes: readySources.reduce(
        (total, source) => total + source.sizeBytes,
        0,
      ),
    }),
    [readySources],
  );
  const activeMutationID =
    reindexMutation.isPending && reindexMutation.variables
      ? reindexMutation.variables.id
      : deleteMutation.isPending && deleteMutation.variables
        ? deleteMutation.variables.id
        : cancelIndexMutation.isPending && cancelIndexMutation.variables
          ? cancelIndexMutation.variables.id
          : retryIndexMutation.isPending && retryIndexMutation.variables
            ? retryIndexMutation.variables.id
            : null;

  const handleSearch = (event: FormEvent) => {
    event.preventDefault();
    const text = query.trim();
    if (!text) return;
    setNotice(null);
    searchMutation.mutate({ text, sourceIDs: selectedSourceIDs });
  };

  const toggleSource = (sourceID: string) => {
    setSelectedSourceIDs((current) =>
      current.includes(sourceID)
        ? current.filter((id) => id !== sourceID)
        : [...current, sourceID],
    );
  };

  return (
    <div className="page knowledge-page">
      <PageHeader
        actions={
          <>
            <input
              accept=".txt,.md,.markdown,.pdf,text/plain,text/markdown,application/pdf"
              aria-label="选择知识库文件"
              className="knowledge-file-input"
              onChange={(event) => {
                const file = event.currentTarget.files?.[0];
                if (file) importMutation.mutate(file);
                event.currentTarget.value = "";
              }}
              ref={fileInputRef}
              type="file"
            />
            <button
              className="button button-quiet"
              disabled={sources.length === 0 || exportMutation.isPending}
              onClick={() => exportMutation.mutate()}
              type="button"
            >
              {exportMutation.isPending ? (
                <LoaderCircle className="animate-spin" size={15} />
              ) : (
                <Download size={15} />
              )}
              导出清单
            </button>
            <button
              className="button button-primary"
              disabled={importMutation.isPending}
              onClick={() => fileInputRef.current?.click()}
              type="button"
            >
              {importMutation.isPending ? (
                <LoaderCircle className="animate-spin" size={15} />
              ) : (
                <Upload size={15} />
              )}
              {importMutation.isPending ? "正在导入" : "导入资料"}
            </button>
          </>
        }
        eyebrow={
          <span className="knowledge-eyebrow">
            <LockKeyhole size={13} /> 本地资料层
          </span>
        }
        meta={<span className="local-pill">Local-only</span>}
        title="知识库"
      />

      <section className="knowledge-overview" aria-label="知识库概览">
        <div className="knowledge-overview-copy">
          <span className="knowledge-overview-icon" aria-hidden="true">
            <BookOpenText size={22} />
          </span>
          <div>
            <h2>把常用资料变成可定位的本地答案</h2>
            <p>
              仅处理你明确选择的 TXT 或 Markdown。原文、分段和 FTS
              索引随工作区数据库一致备份，不会自动联网。
            </p>
          </div>
        </div>
        <div className="knowledge-stat-grid">
          <div>
            <Database size={15} />
            <strong>{stats.sourceCount}</strong>
            <span>可用来源</span>
          </div>
          <div>
            <BookOpenText size={15} />
            <strong>{stats.chunkCount}</strong>
            <span>索引分段</span>
          </div>
          <div>
            <HardDrive size={15} />
            <strong>{formatBytes(stats.bytes)}</strong>
            <span>原文大小</span>
          </div>
        </div>
      </section>

      {notice ? (
        <div className="knowledge-notice knowledge-notice-success">
          <ShieldCheck size={15} />
          {notice}
        </div>
      ) : null}
      {operationError ? (
        <ErrorState compact message={operationError} title="知识库操作未完成" />
      ) : null}

      <div className="knowledge-workspace">
        <aside className="knowledge-sources-panel" aria-label="知识来源">
          <div className="knowledge-panel-heading">
            <div>
              <span className="knowledge-section-kicker">SOURCES</span>
              <h2>知识来源</h2>
            </div>
            {selectedSourceIDs.length ? (
              <button
                className="button button-quiet"
                onClick={() => setSelectedSourceIDs([])}
                type="button"
              >
                清除筛选
              </button>
            ) : null}
          </div>
          <p className="knowledge-panel-help">点选来源可限定右侧检索范围。</p>
          {sourcesQuery.isPending ? <SkeletonRows count={3} /> : null}
          {sourcesQuery.isError ? (
            <ErrorState
              compact
              message={errorMessage(sourcesQuery.error)}
              onRetry={() => void sourcesQuery.refetch()}
            />
          ) : null}
          {!sourcesQuery.isPending &&
          !sourcesQuery.isError &&
          sources.length === 0 ? (
            <div className="knowledge-source-empty">
              <FileText size={20} />
              <strong>还没有来源</strong>
              <span>导入一份本地资料后即可检索。</span>
            </div>
          ) : null}
          <div className="knowledge-source-list">
            {sources.map((source) => (
              <SourceCard
                busy={activeMutationID === source.id}
                key={source.id}
                onCancel={() => {
                  setNotice(null);
                  cancelIndexMutation.mutate(source);
                }}
                onDelete={() => {
                  setOperationError(null);
                  setPendingDelete(source);
                }}
                onReindex={() => {
                  setNotice(null);
                  reindexMutation.mutate(source);
                }}
                onRetry={() => {
                  setNotice(null);
                  retryIndexMutation.mutate(source);
                }}
                onToggle={() => toggleSource(source.id)}
                selected={selectedSourceIDs.includes(source.id)}
                source={source}
              />
            ))}
          </div>
        </aside>

        <main className="knowledge-search-panel">
          <div className="knowledge-panel-heading knowledge-search-heading">
            <div>
              <span className="knowledge-section-kicker">LOCAL SEARCH</span>
              <h2>检索资料</h2>
            </div>
            <span className="knowledge-filter-summary">
              {selectedSourceIDs.length
                ? `已选 ${selectedSourceIDs.length} 个来源`
                : "全部可用来源"}
            </span>
          </div>
          <form className="knowledge-search-form" onSubmit={handleSearch}>
            <Search aria-hidden="true" size={18} />
            <input
              aria-label="搜索知识库"
              maxLength={256}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="搜索术语、客户流程或交付规范…"
              value={query}
            />
            <button
              className="button button-primary"
              disabled={
                !query.trim() ||
                searchMutation.isPending ||
                readySources.length === 0
              }
              type="submit"
            >
              {searchMutation.isPending ? (
                <LoaderCircle className="animate-spin" size={15} />
              ) : null}
              搜索
            </button>
          </form>

          {!searchMutation.data && !searchMutation.isPending ? (
            <div className="knowledge-search-placeholder">
              <span className="knowledge-search-placeholder-icon">
                <Search size={23} />
              </span>
              <h3>确定性关键词检索</h3>
              <p>
                结果会附带来源、文档版本、行号和字符范围。当前不会把检索内容发送给
                AI 或任何远程服务。
              </p>
            </div>
          ) : null}
          {searchMutation.isPending ? <SkeletonRows count={4} /> : null}
          {searchMutation.data?.items.length === 0 ? (
            <EmptyState
              title="没有匹配结果"
              message="可以减少关键词、清除来源筛选，或确认资料已经完成索引。"
            />
          ) : null}
          {searchMutation.data?.items.length ? (
            <section className="knowledge-results" aria-label="知识库搜索结果">
              <div className="knowledge-results-summary">
                <span>找到 {searchMutation.data.items.length} 个结果</span>
                <span>查询“{searchMutation.data.query}”</span>
              </div>
              {searchMutation.data.items.map((result) => (
                <SearchResultCard key={result.chunkId} result={result} />
              ))}
            </section>
          ) : null}
        </main>
      </div>

      <Modal
        footer={
          <>
            <button
              className="button button-quiet"
              disabled={deleteMutation.isPending}
              onClick={() => setPendingDelete(null)}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-danger"
              disabled={deleteMutation.isPending}
              onClick={() =>
                pendingDelete && deleteMutation.mutate(pendingDelete)
              }
              type="button"
            >
              {deleteMutation.isPending ? (
                <LoaderCircle className="animate-spin" size={15} />
              ) : (
                <Trash2 size={15} />
              )}
              删除来源
            </button>
          </>
        }
        onClose={() => setPendingDelete(null)}
        open={pendingDelete !== null}
        title="删除知识来源？"
      >
        <div className="knowledge-delete-copy">
          <p>
            将删除 <strong>{pendingDelete?.name}</strong>{" "}
            的受控原文副本、派生文本、全部分段和 FTS 索引。
          </p>
          <p>此操作不会删除你最初选择的磁盘文件，但知识库内的数据无法恢复。</p>
          {operationError && pendingDelete ? (
            <ErrorState compact message={operationError} title="删除失败" />
          ) : null}
        </div>
      </Modal>
    </div>
  );
}
