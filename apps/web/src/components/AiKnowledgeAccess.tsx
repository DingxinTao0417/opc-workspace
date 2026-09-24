import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { getKnowledgeSources } from "../api/client";
import type { AiWorkspaceGrant } from "../types/models";

type Sources = NonNullable<AiWorkspaceGrant["knowledge_sources"]>;

/** Local-only discovery. Only selected identities/versions become run consent. */
export function AiKnowledgeAccess({
  value,
  onChange,
  disabled,
}: {
  value: Sources;
  onChange: (value: Sources) => void;
  disabled: boolean;
}) {
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [names, setNames] = useState<Record<string, string>>({});
  const sources = useQuery({
    queryKey: ["knowledge", "consent-sources", search, page],
    queryFn: () => getKnowledgeSources({ page, pageSize: 10, search }),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });
  return (
    <fieldset disabled={disabled} className="ai-knowledge-access">
      <legend>本次允许读取的知识来源（{value.length}/20）</legend>
      <p>
        只授权选中的来源及当前版本，不包含之后新增的来源。重建、删除或版本变化会要求重新授权。搜索与勾选本身仅在本机进行。
      </p>
      <input
        aria-label="搜索可授权知识来源"
        type="search"
        value={search}
        onChange={(event) => {
          setSearch(event.target.value);
          setPage(1);
        }}
        placeholder="按来源名称搜索"
      />
      {sources.isFetching ? (
        <p role="status">正在读取来源…</p>
      ) : sources.isError ? (
        <div role="alert">
          知识来源读取失败。
          <button
            className="button button-secondary"
            type="button"
            onClick={() => void sources.refetch()}
          >
            重试来源列表
          </button>
        </div>
      ) : sources.data?.items.length ? (
        sources.data.items.map((source) => {
          const selected = value.find((item) => item.source_id === source.id);
          const ready =
            !source.deletedAt &&
            source.chunkCount > 0 &&
            (source.status === "ready" || source.status === "indexing");
          return (
            <label key={source.id}>
              <input
                type="checkbox"
                checked={!!selected}
                disabled={!ready || (!selected && value.length >= 20)}
                onChange={(event) => {
                  setNames((current) => ({
                    ...current,
                    [source.id]: source.name,
                  }));
                  onChange(
                    event.target.checked
                      ? [
                          ...value,
                          {
                            source_id: source.id,
                            expected_source_version: source.version,
                          },
                        ]
                      : value.filter((item) => item.source_id !== source.id),
                  );
                }}
              />
              <span>
                <strong>{source.name}</strong>
                <small>
                  {source.sourceType} · v{source.version} · {source.chunkCount}{" "}
                  个片段{!ready ? " · 尚不可读取" : ""}
                  {selected &&
                  selected.expected_source_version !== source.version
                    ? " · 版本已变，请取消后重新勾选"
                    : ""}
                </small>
              </span>
            </label>
          );
        })
      ) : (
        <p>没有可匹配的知识来源，请先在知识库导入并完成索引。</p>
      )}
      <div className="flex items-center gap-2">
        <button
          className="button button-secondary"
          type="button"
          disabled={page === 1 || sources.isFetching}
          onClick={() => setPage(page - 1)}
        >
          上一页来源
        </button>
        <span>第 {page} 页</span>
        <button
          className="button button-secondary"
          type="button"
          disabled={
            sources.isFetching ||
            !sources.data ||
            page * sources.data.meta.pageSize >= sources.data.meta.total
          }
          onClick={() => setPage(page + 1)}
        >
          下一页来源
        </button>
      </div>
      {value.length ? (
        <div aria-label="已授权知识来源">
          {value.map((source) => (
            <div
              key={source.source_id}
              className="flex items-center justify-between gap-2"
            >
              <span>
                {names[source.source_id] ??
                  sources.data?.items.find(
                    (item) => item.id === source.source_id,
                  )?.name ??
                  source.source_id}{" "}
                · 已选 v{source.expected_source_version}
              </span>
              <button
                className="button button-secondary"
                type="button"
                onClick={() =>
                  onChange(
                    value.filter((item) => item.source_id !== source.source_id),
                  )
                }
              >
                移除来源
              </button>
            </div>
          ))}
        </div>
      ) : null}
      <p>
        搜索摘要不能当作引用证据。工具正文不另存历史快照；模型可能将内容写入回答或会话记忆。引用只保存位置与版本，来源删除后可能无法核验原文。
      </p>
    </fieldset>
  );
}
