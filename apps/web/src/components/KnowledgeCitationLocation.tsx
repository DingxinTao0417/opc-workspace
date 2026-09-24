import { useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { ApiError } from "../api/client";
import { getKnowledgeChunk } from "../api/knowledgeChunk";
import {
  isKnowledgeIdentity,
  knowledgeLocationKeys,
  parseKnowledgeLocation,
} from "../lib/knowledgeLocation";
import { useAiChatStore } from "../store/aiChat";
import { Modal } from "./Modal";
import { ErrorState, LoadingState } from "./feedback";

export function KnowledgeCitationLocation() {
  const [params, setParams] = useSearchParams();
  const requested = knowledgeLocationKeys.some((key) => params.has(key));
  const location = parseKnowledgeLocation(params);
  const rawSession = params.get("return_session");
  const returnSession =
    params.getAll("return_session").length === 1 &&
    isKnowledgeIdentity(rawSession)
      ? rawSession
      : null;
  const chunk = useQuery({
    queryKey: ["knowledge", "chunk", location],
    queryFn: ({ signal }) => getKnowledgeChunk(location!, signal),
    enabled: requested && location !== null,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: "always",
  });
  const close = () => {
    const next = new URLSearchParams(params);
    for (const key of [...knowledgeLocationKeys, "return_session"])
      next.delete(key);
    setParams(next, { replace: true });
  };
  const unavailable =
    chunk.error instanceof ApiError &&
    ["KNOWLEDGE_CHUNK_NOT_FOUND", "KNOWLEDGE_LOCATION_CHANGED"].includes(
      chunk.error.code,
    );
  return (
    <Modal
      open={requested}
      title="核验知识引用"
      onClose={close}
      width="760px"
      footer={
        <>
          <button className="button button-quiet" onClick={close} type="button">
            关闭预览
          </button>
          {returnSession ? (
            <Link
              className="button button-primary"
              to="/ai"
              onClick={() =>
                useAiChatStore.getState().setActiveSessionId(returnSession)
              }
            >
              返回对话
            </Link>
          ) : null}
        </>
      }
    >
      <div className="knowledge-citation-preview">
        <p>
          只在本地核验这一片段，不会发送给
          AI。引用验证的是来源身份，不代表回答一定正确。
        </p>
        {!location ? (
          <ErrorState
            compact
            title="引用位置无效"
            message="链接缺少来源、文档、分段或版本信息，请从对话中的已验证来源重新打开。"
          />
        ) : chunk.isFetching ? (
          <LoadingState label="正在核验来源版本…" />
        ) : chunk.isError ? (
          <ErrorState
            compact
            title={unavailable ? "原引用片段已不可用" : "暂时无法读取引用"}
            message={
              unavailable
                ? "来源可能已删除、重建或版本发生变化；不会用新版片段替代这条历史证据。原对话保留引用信息；仅手选上下文另有正文快照，工具读取的正文不单独保存。"
                : "本地服务未能完成读取，请重试；历史引用信息不会被改写。"
            }
            onRetry={() => void chunk.refetch()}
          />
        ) : chunk.data ? (
          <>
            <h3>{chunk.data.source_name}</h3>
            <p>
              {chunk.data.document_title} · 来源 v{chunk.data.source_version} ·
              文档 v{chunk.data.document_version} · 分段{" "}
              {chunk.data.chunk_index + 1}
            </p>
            <p>
              {chunk.data.source_type === "pdf"
                ? `第 ${chunk.data.start_page}–${chunk.data.end_page} 页 · `
                : ""}
              第 {chunk.data.start_line}–{chunk.data.end_line} 行 · 字符{" "}
              {chunk.data.start_char}–{chunk.data.end_char}
            </p>
            {chunk.data.source_type === "pdf" ? (
              <p>以下是 PDF 的提取文本，不是原始版面或扫描图像。</p>
            ) : null}
            <pre aria-label="知识引用原片段">{chunk.data.content}</pre>
          </>
        ) : null}
      </div>
    </Modal>
  );
}
