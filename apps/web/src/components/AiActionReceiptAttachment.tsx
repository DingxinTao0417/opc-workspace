/** The source identity is runtime-only; only an explicit send reads its receipts. */
export function AiActionReceiptAttachment({
  onRemove,
  disabled = false,
  recheckProposalId,
}: {
  onRemove: () => void;
  disabled?: boolean;
  recheckProposalId?: string;
}) {
  return (
    <div className="ai-action-note" role="status">
      {recheckProposalId ? (
        <>
          本次重新核验旧建议：{recheckProposalId}
          。发送时将读取该建议的完整原操作参数，
          可能包含备注、财务或知识文本，但不包含完整审批预览。参数不是当前事实，也不会直接执行。
          只有重新授权并手动发送后才会交给所选模型；移除此附件即不再读取原参数。
        </>
      ) : (
        <>
          本次将带入所选一轮的最新操作回执（状态与记录身份，不含变更正文）。
          仍需手动发送；工作台读取和操作须重新授权。
        </>
      )}
      <button type="button" onClick={onRemove} disabled={disabled}>
        {recheckProposalId ? "移除重新核验上下文" : "移除操作回执"}
      </button>
    </div>
  );
}
