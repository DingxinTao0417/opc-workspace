import { Link } from "react-router-dom";
import { agentRunFailureRuleId } from "../lib/agentRunFailureSource";
import { automationRetryResultRoute } from "../api/aiAutomationRetry";
import {
  aiActionFields,
  type AiActionProposal,
} from "../api/aiWorkspaceActions";

const names: Record<string, string> = {
  ...aiActionFields,
  project_name: "来源项目名称",
  invoice_id: "来源发票 ID",
  invoice_number: "来源发票编号",
  agent_run_id: "失败 Agent 执行 ID",
  task_id: "来源任务 ID",
  attempt: "Agent 执行次数",
  error_code: "Agent 安全错误码",
  failed_at: "Agent 失败时间（UTC）",
};
const kinds: Record<string, string> = {
  inbox_item: "收件箱事项",
  task: "任务",
  reminder: "本地提醒",
};
export function AiAutomationRetryDetails({
  proposal,
}: {
  proposal: AiActionProposal;
}) {
  const p = proposal.preview.automation_retry;
  if (!p) return null;
  const r = proposal.automation_run_result;
  const route = automationRetryResultRoute(r);
  return (
    <section aria-label="原运行快照与重试结果">
      {p.rule_id === agentRunFailureRuleId ? (
        <p className="ai-action-note">
          这里只重试失败诊断通知，不重跑 Agent、不调用模型、不恢复产出登记。
        </p>
      ) : null}
      <p className="ai-action-note">
        仅重试下方原始本地动作，不改规则、不启用规则、不对外发送消息。原快照只展示给你，不发送给模型。
        {p.trigger_type === "event"
          ? "已捕获事件即使停用也保留交付责任。"
          : "本次提醒将按确认时刻触发，不补发整个旧时间窗口。"}
        若再次发生可重试错误，沿用原自动退避；含首轮总计最多 3 次尝试。
      </p>
      <dl>
        <dt>原运行 ID</dt>
        <dd>{p.run_id}</dd>
        <dt>失败错误码</dt>
        <dd>{p.error_code}</dd>
        <dt>尝试次数</dt>
        <dd>
          {p.attempt} → {p.next_attempt} / 3
        </dd>
        <dt>原快照规则版本</dt>
        <dd>{p.rule_version}</dd>
        <dt>当前规则</dt>
        <dd>
          版本 {p.current_rule_version} · {p.rule_enabled ? "启用" : "停用"}
          （不用于替换原快照）
        </dd>
        <dt>本地动作</dt>
        <dd>{kinds[p.action_type]}</dd>
        <dt>原计划窗口（UTC）</dt>
        <dd>{p.scheduled_for ?? "事件触发"}</dd>
        <dt>所需权限</dt>
        <dd>{p.permissions.join("\n")}</dd>
      </dl>
      <h4>原配置</h4>
      <dl>
        {Object.entries(p.config).map(([key, value]) => (
          <div key={key}>
            <dt>{names[key] ?? key}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
      <h4>原动作快照</h4>
      <dl>
        {Object.entries(p.action).map(([key, value]) => (
          <div key={key}>
            <dt>{names[key] ?? key}</dt>
            <dd>
              {key === "action_type"
                ? kinds[value ?? ""]
                : value === null
                  ? "未设置"
                  : value}
            </dd>
          </div>
        ))}
      </dl>
      {r ? (
        <div role={r.status === "failed" ? "alert" : "status"}>
          {r.status === "succeeded" ? (
            <p>
              第 {r.attempt} 次尝试成功，已创建{kinds[r.result_type ?? ""]}。
            </p>
          ) : (
            <p>
              第 {r.attempt} 次尝试失败（{r.error_code}），本次未创建目标对象。
              {r.retryable
                ? `当时记录的下次自动重试时间（UTC）：${r.retry_at}。后续是否已重试请在自动化设置查看。`
                : "该记录不可继续重试，请查看失败原因。"}
            </p>
          )}
          {route ? (
            <Link to={route}>查看本次创建的{kinds[r.result_type ?? ""]}</Link>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
