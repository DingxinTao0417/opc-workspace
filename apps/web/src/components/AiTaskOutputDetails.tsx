import type { AiActionProposal } from "../api/aiWorkspaceActions";

export function AiTaskOutputDetails({
  proposal,
}: {
  proposal: AiActionProposal;
}) {
  const data = proposal.preview.task_output;
  if (!data) return null;
  const submitting = proposal.action.action === "task.submit_output";
  return (
    <section className="ai-split-preview" aria-label="提交与验收证据">
      <p className="ai-action-note">
        {submitting
          ? "以下内容经你核实后按人工提交记录，产出归属于当前负责人、由你录入。智能体起草不证明负责人实际完成，也不会启动 Agent。提交只进入待验收；标记需跟进的产出会生成收件箱事项。"
          : proposal.action.changes.decision === "accept"
            ? "接受后该批次记录为已接受，任务完成、活动分派结束，并按既有规则更新收件箱及父任务。请由你核实全部验收证据；智能体的建议不能代替人工验收。"
            : "要求返工后保留该批次和所有产出历史，任务回到进行中，保留当前分派；不自动创建新提交或启动 Agent。"}
      </p>
      <h4>当前责任</h4>
      {data.assignments.map((row) => (
        <p key={row.id}>
          {row.role === "reviewer" ? "验收人" : "负责人"}：{row.actor_name}（
          {row.actor_type === "owner"
            ? "所有者"
            : row.actor_type === "person"
              ? "本地人员"
              : "智能体"}
          ）
        </p>
      ))}
      <h4>完整提交说明</h4>
      <p style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
        {data.summary || "无摘要"}
      </p>
      <h4>产出（{data.artifacts.length}）</h4>
      {data.artifacts.map((art, i) => (
        <section key={art.id ?? i} aria-label={`产出 ${i + 1}`}>
          <strong>{art.name}</strong>
          <p>
            {art.storage_kind} · {art.requires_followup ? "需跟进" : "无需跟进"}
            {art.deleted ? " · 已删除，仅保留元数据" : ""}
          </p>
          {art.storage_kind === "file" ? (
            <p>
              文件元数据：{art.size_bytes} 字节，SHA-256：{art.sha256}
              。没有读取或核验文件内容，请在任务详情下载并人工检查。
            </p>
          ) : art.content !== null ? (
            <pre style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
              {art.content}
            </pre>
          ) : null}
        </section>
      ))}
      {!data.artifacts.length ? (
        <p>此批次没有附件，不代表已核实实际交付。</p>
      ) : null}
      {proposal.status === "confirmed" ? (
        <p>
          本次提交批次：{proposal.result_id}
          。这是确认时的历史结果，当前状态请在任务详情核对。
        </p>
      ) : null}
    </section>
  );
}
