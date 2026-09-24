import { useEffect, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { useAiChatStream } from "../api/hooks";
import { cancelAiGeneration, type AiGeneration } from "../api/aiActions";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import { AiContinuationMonitor } from "./AiPlanContinuation";

function generationSessionHref(
  generation: AiGeneration | undefined,
): string | null {
  return generation?.persist && isWorkspaceIdentity(generation.session_id)
    ? `/ai?session=${generation.session_id}`
    : null;
}

// Mounted above routed pages: all active generations stay discoverable and
// stoppable, including requests started by another window or before a reload.
export function AiGenerationMonitor() {
  const chat = useAiChatStream();
  const location = useLocation();
  const [cancelError, setCancelError] = useState<string | null>(null);
  useEffect(() => {
    void chat.recover();
    const timer = window.setInterval(() => void chat.recover(), 3_000);
    const recover = () => void chat.recover();
    window.addEventListener("online", recover);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("online", recover);
    };
  }, [chat.recover]);
  const otherGenerations = chat.activeGenerations.filter(
    (item) => !item.origin && item.id !== chat.streaming?.generationId,
  );
  const currentGeneration = chat.activeGenerations.find(
    (item) => item.id === chat.streaming?.generationId,
  );
  const currentSessionHref = generationSessionHref(currentGeneration);
  if (!chat.isStreaming && otherGenerations.length === 0)
    return <AiContinuationMonitor />;
  return (
    <>
      <AiContinuationMonitor />
      <aside
        className="ai-provider-banner ai-generation-monitor"
        aria-label="AI 生成状态"
      >
        {chat.isStreaming ? (
          <>
            <span role="status">AI 助手正在生成回复</span>
            {location.pathname !== "/ai" ||
            (currentSessionHref &&
              chat.activeSessionId !== currentGeneration?.session_id) ? (
              <Link to={currentSessionHref ?? "/ai"}>返回会话</Link>
            ) : null}
            <button type="button" onClick={() => void chat.stop()}>
              停止生成
            </button>
          </>
        ) : null}
        {otherGenerations.map((item) => {
          const href = generationSessionHref(item);
          return (
            <span key={item.id}>
              另一个会话正在生成
              {href ? <Link to={href}>查看会话</Link> : null}
              <button
                type="button"
                onClick={() =>
                  void cancelAiGeneration(item.id)
                    .then(() => {
                      setCancelError(null);
                      return chat.recover();
                    })
                    .catch(() => {
                      setCancelError("停止请求未确认，请稍后重试");
                      void chat.recover();
                    })
                }
              >
                停止该生成
              </button>
            </span>
          );
        })}
        {cancelError ? <span role="alert">{cancelError}</span> : null}
      </aside>
    </>
  );
}
