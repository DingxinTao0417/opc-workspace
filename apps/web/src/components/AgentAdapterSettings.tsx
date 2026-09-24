import {
  AlertCircle,
  Bot,
  CheckCircle2,
  LoaderCircle,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
} from "lucide-react";
import { useState } from "react";
import { ApiError } from "../api/client";
import {
  useAgentAdaptersQuery,
  useCheckAgentAdapter,
  useRegisterAgentAdapter,
  useSetAgentAdapterEnabled,
} from "../api/hooks";
import { agentAdapterSettingsHandoff } from "../lib/aiIssueHandoff";
import { AiIssueHandoffButton } from "./AiWorkbenchHandoff";

const capabilityLabels: Record<string, string> = {
  read_task_snapshot: "读取任务快照",
  write_text_artifact: "提交文本产出",
  write_structured_artifact: "提交结构化产出",
};

// ADR-027 gives the code-owned builtin a lifecycle gate, not an OS sandbox gate.
const builtinLifecycleGates = [
  "管道协议往返",
  "进程树回收",
  "超时与取消",
  "启动中断恢复",
];

const healthLabels = {
  unknown: "未检查",
  blocked: "受阻",
  healthy: "健康",
  unhealthy: "异常",
};

function agentAdapterError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "适配器状态已变化，请查看刷新后的状态再重试。";
    }
    if (error.code === "AGENT_ADAPTER_NOT_EXECUTION_READY") {
      return "内置执行器尚未就绪，请重新检查运行条件后再启用。";
    }
    if (error.code === "AGENT_ADAPTER_ALREADY_REGISTERED") {
      return "内置适配器已登记，请刷新设置后继续。";
    }
    if (error.code === "AGENT_ADAPTER_ACTOR_CONFLICT") {
      return "Agent 身份关联冲突，无法启用此适配器；请检查现有 Agent 的关联配置。";
    }
    return `${error.message}${error.requestId ? ` · 请求 ${error.requestId}` : ""}`;
  }
  return "本地 Agent 操作失败，请确认 Sidecar 已就绪后重试。";
}

export function AgentAdapterSettings() {
  const adapters = useAgentAdaptersQuery();
  const register = useRegisterAgentAdapter();
  const check = useCheckAgentAdapter();
  const setEnabled = useSetAgentAdapterEnabled();
  const [feedback, setFeedback] = useState<string | null>(null);

  if (adapters.isPending) {
    return (
      <div aria-live="polite" className="settings-state" role="status">
        <LoaderCircle className="animate-spin" size={16} />
        正在读取本地 Agent 适配器…
      </div>
    );
  }

  if (adapters.isError || !adapters.data) {
    return (
      <div className="settings-state settings-state-error" role="alert">
        <AlertCircle size={16} />
        <div>
          <strong>无法读取本地 Agent 设置</strong>
          <span>{agentAdapterError(adapters.error)}</span>
        </div>
        <button
          className="button button-secondary"
          onClick={() => void adapters.refetch()}
          type="button"
        >
          <RefreshCw size={14} />
          重试
        </button>
      </div>
    );
  }

  const adapter =
    adapters.data.find((item) => item.adapterKey === "builtin-local-text-v1") ??
    null;
  const actionError = register.error ?? check.error ?? setEnabled.error;
  const busy = register.isPending || check.isPending || setEnabled.isPending;
  const isEnabled = adapter?.status === "enabled";
  const canEnable =
    adapter?.readiness.canEnable &&
    adapter.executionReady &&
    adapter.healthStatus === "healthy";

  function resetActionState() {
    setFeedback(null);
    register.reset();
    check.reset();
    setEnabled.reset();
  }

  async function registerPreset() {
    if (busy) return;
    try {
      resetActionState();
      await register.mutateAsync();
      setFeedback("内置适配器已登记，尚未启用。");
    } catch {
      // Mutation state renders a safe error below.
    }
  }

  async function runCheck() {
    if (!adapter || busy) return;
    try {
      resetActionState();
      const result = await check.mutateAsync({
        id: adapter.id,
        expectedVersion: adapter.version,
      });
      setFeedback(
        result.executionReady && result.healthStatus === "healthy"
          ? "内置执行器生命周期检查已通过。"
          : "检查已完成，当前运行条件尚未就绪。",
      );
    } catch {
      // Mutation state renders a safe error below.
    }
  }

  async function changeEnabled() {
    if (!adapter || busy || (!isEnabled && !canEnable)) return;
    try {
      resetActionState();
      const result = await setEnabled.mutateAsync({
        id: adapter.id,
        enabled: !isEnabled,
        expectedVersion: adapter.version,
      });
      setFeedback(
        result.status === "enabled"
          ? "适配器已启用；请到任务详情启动执行。"
          : "适配器已停用，不再接受新的 Agent 分派或执行。",
      );
    } catch {
      // Mutation state renders a safe error below.
    }
  }

  return (
    <div className="agent-adapter-settings">
      <header className="settings-content-header">
        <h3>本地 Agent</h3>
        <p>登记、检查并显式启用内置执行器；启用后可在任务详情启动执行。</p>
      </header>

      <div className="agent-adapter-boundary-note">
        <ShieldCheck size={16} />
        <span>
          Sidecar 只接受代码内置适配器，不接受自定义路径、Shell、SQL、HTTP
          或任意命令；模型访问受代码端点校验约束。
        </span>
      </div>

      {!adapter ? (
        <section className="agent-adapter-empty">
          <Bot size={22} />
          <strong>尚未登记本地 Agent 适配器</strong>
          <span>
            登记不会创建 Agent
            身份、任务分派或运行进程；运行条件就绪后仍需手动启用。
          </span>
          <button
            className="button button-primary"
            disabled={busy}
            onClick={() => void registerPreset()}
            type="button"
          >
            {register.isPending ? (
              <LoaderCircle className="animate-spin" size={14} />
            ) : (
              <Bot size={14} />
            )}
            {register.isPending ? "正在登记…" : "登记内置适配器"}
          </button>
        </section>
      ) : (
        <section className="agent-adapter-card">
          <div className="agent-adapter-heading">
            <div className="agent-adapter-icon">
              <Bot size={18} />
            </div>
            <div>
              <h4>{adapter.displayName}</h4>
              <p>{adapter.protocolVersion} · 短生命周期进程协议</p>
            </div>
            <span data-status={adapter.healthStatus}>
              {isEnabled ? "已启用" : "未启用"} ·{" "}
              {healthLabels[adapter.healthStatus]}
            </span>
          </div>

          <div className="agent-adapter-grid">
            <div>
              <span>允许能力</span>
              <ul>
                {adapter.manifest.capabilities.map((capability) => (
                  <li key={capability}>
                    {capabilityLabels[capability] ?? capability}
                  </li>
                ))}
              </ul>
            </div>
            <div>
              <span>内置执行器生命周期闸门</span>
              <ul>
                {builtinLifecycleGates.map((gate) => (
                  <li data-ready={adapter.executionReady} key={gate}>
                    {adapter.executionReady ? (
                      <CheckCircle2 size={12} />
                    ) : (
                      <ShieldAlert size={12} />
                    )}
                    {gate}
                  </li>
                ))}
              </ul>
            </div>
          </div>

          <p>
            生命周期检查不代表已通过操作系统沙箱或禁网验证；外部执行器仍不可用。
          </p>

          {!canEnable ? (
            <div className="agent-adapter-blocked" role="status">
              <ShieldAlert size={15} />
              <div>
                <strong>暂不可启用</strong>
                <span>
                  {adapter.healthStatus === "unknown"
                    ? "请先检查内置执行器运行条件；未就绪时不能启用。"
                    : "内置执行器运行条件尚未就绪，请重新检查；未验证的平台保持禁用。"}
                </span>
              </div>
            </div>
          ) : (
            <p>
              {isEnabled
                ? "已启用，可在任务详情分派并启动 Agent；启用本身不会启动任务。"
                : "运行条件已就绪。启用会创建对应 Agent 身份，不会自动分派或启动任务。"}
            </p>
          )}

          <div className="agent-adapter-actions">
            <AiIssueHandoffButton
              content={agentAdapterSettingsHandoff(
                adapter.id,
                adapter.displayName,
                adapter.status,
                adapter.healthStatus,
                adapter.executionReady,
              )}
              disabled={busy}
            />
            <button
              className="button button-secondary"
              disabled={busy}
              onClick={() => void runCheck()}
              type="button"
            >
              {check.isPending ? (
                <LoaderCircle className="animate-spin" size={14} />
              ) : (
                <RefreshCw size={14} />
              )}
              {check.isPending
                ? "正在检查…"
                : adapter.lastHealthAt
                  ? "重新检查"
                  : "检查运行条件"}
            </button>
            <button
              className="button button-primary"
              disabled={busy || (!isEnabled && !canEnable)}
              onClick={() => void changeEnabled()}
              title={
                isEnabled
                  ? "停用后不再接受新的分派或执行"
                  : "内置执行器生命周期检查通过后才可启用"
              }
              type="button"
            >
              {setEnabled.isPending ? (
                <LoaderCircle className="animate-spin" size={14} />
              ) : null}
              {setEnabled.isPending
                ? setEnabled.variables?.enabled
                  ? "正在启用…"
                  : "正在停用…"
                : isEnabled
                  ? "停用适配器"
                  : "启用适配器"}
            </button>
          </div>
        </section>
      )}

      {feedback ? (
        <div className="automation-success" role="status">
          <CheckCircle2 size={15} />
          {feedback}
        </div>
      ) : null}
      {actionError ? (
        <div className="automation-error" role="alert">
          <AlertCircle size={15} />
          {agentAdapterError(actionError)}
        </div>
      ) : null}
    </div>
  );
}
