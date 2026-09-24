import { useQuery } from "@tanstack/react-query";
import { getAiGeneration } from "../api/aiActions";
import type { AiContinuationLocation } from "../lib/aiContinuationLocation";
import type { AiActionContinuation } from "../lib/aiActionContinuation";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import { AiWorkspaceActions } from "./AiWorkspaceActions";
import { Modal } from "./Modal";

/** Reopen the original approval, independent of chat history pagination. */
export function AiApprovalContinuation({
  target,
  onClose,
  onContinue,
}: {
  target: Extract<AiContinuationLocation, { kind: "approval" }>;
  onClose: () => void;
  onContinue?: (request: AiActionContinuation) => void;
}) {
  const valid = [
    target.sessionId,
    target.generationId,
    target.proposalId,
  ].every(isWorkspaceIdentity);
  const generation = useQuery({
    queryKey: ["ai", "approval-source", target.sessionId, target.generationId],
    queryFn: async () => {
      const source = await getAiGeneration(target.generationId);
      return {
        id: source.id,
        session_id: source.session_id,
        persist: source.persist,
      };
    },
    enabled: valid,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  });
  const matches =
    generation.data?.id === target.generationId &&
    generation.data.session_id === target.sessionId &&
    generation.data.persist;

  return (
    <Modal open onClose={onClose} title="续办操作建议" width="720px">
      {!valid ? (
        <p role="alert">操作建议位置无效，请从续办队列重新进入。</p>
      ) : generation.isPending ? (
        <p role="status">正在定位原会话的操作建议…</p>
      ) : generation.isError ? (
        <p role="alert">
          无法读取原操作建议，原会话可能已删除或服务暂不可用。
          <button type="button" onClick={() => void generation.refetch()}>
            重新定位
          </button>
        </p>
      ) : !matches ? (
        <p role="alert">该操作建议不属于当前保存会话，请从续办队列重新进入。</p>
      ) : (
        <>
          <p className="ai-action-note">
            已定位到原操作建议。请核对最新状态和变更内容后再决定。
          </p>
          <AiWorkspaceActions
            generationId={target.generationId}
            sessionId={target.sessionId}
            proposalId={target.proposalId}
            onContinue={onContinue}
          />
        </>
      )}
    </Modal>
  );
}
