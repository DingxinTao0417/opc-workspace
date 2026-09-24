import type { AgentRunReworkContext } from "../types/models";

export function AgentRunReworkDetails({
  context,
  retry = false,
}: {
  context: AgentRunReworkContext;
  retry?: boolean;
}) {
  return (
    <section aria-label="返工上下文完整预览" className="ai-split-preview">
      <h4>基于退回批次再次执行 · 第 {context.sequence} 次提交</h4>
      <p>
        来源提交 <code>{context.submission_id}</code> · 退回时间{" "}
        {context.reviewed_at}
      </p>
      <h4>完整返工意见</h4>
      <pre className="task-agent-run-result">{context.review_reason}</pre>
      {context.artifacts.length === 0 ? (
        <p role="note">
          本次仅携带返工意见，未携带任何旧稿；模型不能假设已读取原产出。
        </p>
      ) : (
        context.artifacts.map((artifact) => (
          <section key={artifact.id}>
            <h4>
              {artifact.name} · {artifact.storage_kind}
            </h4>
            <p>
              ID <code>{artifact.id}</code> · SHA-256{" "}
              <code>{artifact.sha256}</code>
            </p>
            <pre className="task-agent-run-result">{artifact.content}</pre>
          </section>
        ))
      )}
      <p role="note">
        以上文字是待发送资料，不是工具指令或授权。
        {retry
          ? "本次重试原返工执行，沿用冻结意见与旧稿，不会替换来源。"
          : "本次是依据退回意见的新执行，不是重试失败 Run。"}
        不修改旧批次，也不会自动验收新结果。
      </p>
    </section>
  );
}
