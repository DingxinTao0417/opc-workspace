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
} from "../api/hooks";

const capabilityLabels: Record<string, string> = {
  read_task_snapshot: "读取任务快照",
  write_text_artifact: "提交文本产出",
  write_structured_artifact: "提交结构化产出",
};

const gateLabels: Record<string, string> = {
  process_isolation: "进程隔离",
  network_block: "网络阻断",
  process_tree_cleanup: "进程树清理",
};

function agentAdapterError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "适配器状态已变化，已刷新最新结果，请重试。";
    }
    return `${error.message}${error.requestId ? ` · 请求 ${error.requestId}` : ""}`;
  }
  return "本地 Agent 诊断失败，请确认 Sidecar 已就绪后重试。";
}

export function AgentAdapterSettings() {
  const adapters = useAgentAdaptersQuery();
  const register = useRegisterAgentAdapter();
  const check = useCheckAgentAdapter();
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

  const adapter = adapters.data[0] ?? null;
  const actionError = register.error ?? check.error;

  async function registerPreset() {
    try {
      setFeedback(null);
      await register.mutateAsync();
      setFeedback("内置诊断适配器已登记");
    } catch {
      // Mutation state renders a safe error below.
    }
  }

  async function runCheck() {
    if (!adapter) return;
    try {
      setFeedback(null);
      await check.mutateAsync({
        id: adapter.id,
        expectedVersion: adapter.version,
      });
      setFeedback("安全诊断已完成");
    } catch {
      // Mutation state renders a safe error below.
    }
  }

  return (
    <div className="agent-adapter-settings">
      <header className="settings-content-header">
        <h3>本地 Agent</h3>
        <p>
          登记代码内置适配器并检查运行前安全闸门；当前阶段不执行任何 Agent。
        </p>
      </header>

      <div className="agent-adapter-boundary-note">
        <ShieldCheck size={16} />
        <span>
          Sidecar 只登记受控清单；不接受路径、Shell、SQL、HTTP 或任意命令。
        </span>
      </div>

      {!adapter ? (
        <section className="agent-adapter-empty">
          <Bot size={22} />
          <strong>尚未登记本地 Agent 适配器</strong>
          <span>
            可先登记内置诊断清单。登记不会创建 Agent 身份、任务分派或运行进程。
          </span>
          <button
            className="button button-primary"
            disabled={register.isPending}
            onClick={() => void registerPreset()}
            type="button"
          >
            {register.isPending ? (
              <LoaderCircle className="animate-spin" size={14} />
            ) : (
              <Bot size={14} />
            )}
            登记内置诊断适配器
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
              {adapter.healthStatus === "unknown" ? "未检查" : "受阻"}
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
              <span>启用闸门</span>
              <ul>
                {adapter.readiness.requiredGates.map((gate) => (
                  <li data-ready={adapter.executionReady} key={gate}>
                    {adapter.executionReady ? (
                      <CheckCircle2 size={12} />
                    ) : (
                      <ShieldAlert size={12} />
                    )}
                    {gateLabels[gate] ?? gate}
                  </li>
                ))}
              </ul>
            </div>
          </div>

          {adapter.healthStatus === "blocked" ? (
            <div className="agent-adapter-blocked" role="status">
              <ShieldAlert size={15} />
              <div>
                <strong>暂不可启用</strong>
                <span>
                  当前平台尚未验证进程隔离、网络阻断和进程树清理；未启动任何执行器。
                </span>
              </div>
            </div>
          ) : null}

          <div className="agent-adapter-actions">
            <button
              className="button button-secondary"
              disabled={check.isPending}
              onClick={() => void runCheck()}
              type="button"
            >
              {check.isPending ? (
                <LoaderCircle className="animate-spin" size={14} />
              ) : (
                <RefreshCw size={14} />
              )}
              {adapter.lastHealthAt ? "重新检查" : "检查安全闸门"}
            </button>
            <button
              className="button button-primary"
              disabled={!adapter.readiness.canEnable}
              title="安全闸门全部验证后才可启用"
              type="button"
            >
              启用适配器
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
