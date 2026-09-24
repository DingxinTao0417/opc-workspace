import { AlertCircle, CheckCircle2, FileText } from "lucide-react";
import { Link } from "react-router-dom";
import { knowledgeLocationHref } from "../lib/knowledgeLocation";
import type { AiCitation, AiCitationStatus } from "../types/models";

export function AiCitationEvidence({
  status,
  citations,
  sessionId,
  incomplete = false,
}: {
  status: AiCitationStatus | null;
  citations: AiCitation[];
  sessionId: string;
  incomplete?: boolean;
}) {
  if (status === "not_requested") return null;
  if (status !== "validated") {
    const message =
      status === null
        ? "未能恢复引用信息（可能已过期或服务已重启），不能据此判断回答有无来源。"
        : status === "no_evidence"
          ? "模型标记为没有足够的已授权资料证据。"
          : status === "missing"
            ? "这条回答没有提供结构化引用，请谨慎核对。"
            : "模型给出的引用不在已确认片段内，已被 Sidecar 拒绝。";
    return (
      <div
        className={`ai-citation-status is-${status ?? "unavailable"}`}
        role="status"
      >
        <AlertCircle size={13} />
        {message}
      </div>
    );
  }
  return (
    <section className="ai-citation-evidence" aria-label="已验证知识来源">
      <header>
        <CheckCircle2 size={13} />
        <strong>已验证来源</strong>
        <span>{citations.length}</span>
      </header>
      {incomplete ? (
        <p role="status">
          以下来源对应服务端完整回答；当前仅有片段，来源身份验证不代表内容已获证实。
        </p>
      ) : null}
      <div>
        {citations.map((citation) => {
          const href = knowledgeLocationHref(citation, sessionId);
          return (
            <article key={citation.chunk_id}>
              <FileText size={13} />
              <span>
                <strong>{citation.source_name}</strong>
                <small>
                  {citation.source_type === "pdf"
                    ? `第 ${citation.start_page}–${citation.end_page} 页 · `
                    : ""}
                  第 {citation.start_line}–{citation.end_line} 行 · 文档 v
                  {citation.document_version} · chunk {citation.chunk_index + 1}
                </small>
                {href ? <Link to={href}>核验原片段</Link> : null}
              </span>
            </article>
          );
        })}
      </div>
    </section>
  );
}
