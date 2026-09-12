import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  cancelAgentRun,
  createAgentRun,
  getAiProviders,
  getTaskAgentRuns,
  retryAgentRun,
  BUILTIN_AGENT_ACTOR_ID,
} from "../api/client";
import type { AgentRun, Task } from "../types/models";

const statusLabels: Record<AgentRun["status"], string> = {
  queued: "排队中",
  running: "执行中",
  succeeded: "已成功",
  failed: "已失败",
  cancelled: "已取消",
  interrupted: "已中断",
};

interface TaskAgentRunsSectionProps {
  task: Task;
  disabled?: boolean;
}

function formatRunTime(value: string | null) {
  return value ? new Date(value).toLocaleString() : "—";
}

export function TaskAgentRunsSection({
  task,
  disabled,
}: TaskAgentRunsSectionProps) {
  const queryClient = useQueryClient();
  const [providerId, setProviderId] = useState("");
  const [expandedRunId, setExpandedRunId] = useState<string | null>(null);

  const runsQuery = useQuery({
    queryKey: ["task-agent-runs", task.id],
    queryFn: () => getTaskAgentRuns(task.id),
    refetchInterval: (query) =>
      query.state.data?.some(
        (run) => run.status === "queued" || run.status === "running",
      )
        ? 2000
        : false,
  });

  const providersQuery = useQuery({
    queryKey: ["ai-providers-for-agent"],
    queryFn: () => getAiProviders(),
  });
  const usableProviders = (providersQuery.data ?? []).filter(
    (provider) =>
      provider.status === "ready" && provider.health_status === "healthy",
  );

  const invalidate = () => {
    void queryClient.invalidateQueries({
      queryKey: ["task-agent-runs", task.id],
    });
  };

  const startMutation = useMutation({
    mutationFn: async () => {
      if (!providerId) throw new Error("选择模型后再启动");
      try {
        return await createAgentRun(task.id, providerId);
      } catch (error) {
        // The task may not have the builtin agent assigned yet; assign it
        // once automatically (ADR-027 deterministic actor id), then retry.
        if (
          error instanceof Object &&
          "code" in error &&
          (error as { code?: string }).code === "AGENT_RUN_NOT_EXECUTABLE"
        ) {
          await assignBuiltinAgent(task.id, task.version);
          return createAgentRun(task.id, providerId);
        }
        throw error;
      }
    },
    onSuccess: () => {
      setProviderId("");
      invalidate();
    },
  });

  const cancelMutation = useMutation({
    mutationFn: (runId: string) => cancelAgentRun(runId),
    onSuccess: invalidate,
  });
  const retryMutation = useMutation({
    mutationFn: (runId: string) => retryAgentRun(runId),
    onSuccess: invalidate,
  });

  const runs = runsQuery.data ?? [];
  const busy =
    startMutation.isPending ||
    cancelMutation.isPending ||
    retryMutation.isPending;

  return (
    <section className="task-agent-runs">
      <h3>Agent 执行</h3>
      <p className="task-agent-runs-hint">
        把任务交给内置执行代理，由本地或在线模型生成交付文本；产出仅进入执行记录。
      </p>
      <div className="task-agent-runs-controls">
        <select
          aria-label="选择执行模型"
          disabled={disabled || busy}
          onChange={(event) => setProviderId(event.currentTarget.value)}
          value={providerId}
        >
          <option value="">选择模型…</option>
          {usableProviders.map((provider) => (
            <option key={provider.id} value={provider.id}>
              {provider.name}（{provider.kind === "local" ? "本地" : "在线"} ·{" "}
              {provider.model}）
            </option>
          ))}
        </select>
        <button
          disabled={disabled || busy || !providerId}
          onClick={() => startMutation.mutate()}
          type="button"
        >
          启动执行
        </button>
      </div>
      {startMutation.error ? (
        <p className="task-agent-runs-error" role="alert">
          {startMutation.error instanceof Error
            ? startMutation.error.message
            : "启动失败"}
        </p>
      ) : null}
      {runs.length === 0 ? (
        <p className="task-agent-runs-empty">尚无执行记录。</p>
      ) : (
        <ul className="task-agent-runs-list">
          {runs.map((run) => (
            <li key={run.id} data-status={run.status}>
              <div className="task-agent-run-heading">
                <strong>{statusLabels[run.status]}</strong>
                <span>
                  第 {run.attempt} 次 · {run.model} ·{" "}
                  {formatRunTime(run.completedAt ?? run.startedAt)}
                </span>
                <span className="task-agent-run-actions">
                  {run.status === "queued" || run.status === "running" ? (
                    <button
                      disabled={disabled || busy}
                      onClick={() => cancelMutation.mutate(run.id)}
                      type="button"
                    >
                      取消
                    </button>
                  ) : null}
                  {run.status === "failed" ||
                  run.status === "cancelled" ||
                  run.status === "interrupted" ? (
                    <button
                      disabled={disabled || busy}
                      onClick={() => retryMutation.mutate(run.id)}
                      type="button"
                    >
                      重试
                    </button>
                  ) : null}
                  {run.resultText ? (
                    <button
                      onClick={() =>
                        setExpandedRunId((current) =>
                          current === run.id ? null : run.id,
                        )
                      }
                      type="button"
                    >
                      {expandedRunId === run.id ? "收起产出" : "查看产出"}
                    </button>
                  ) : null}
                </span>
              </div>
              {run.errorCode ? (
                <p className="task-agent-run-error">失败码：{run.errorCode}</p>
              ) : null}
              {expandedRunId === run.id && run.resultText ? (
                <pre className="task-agent-run-result">{run.resultText}</pre>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

async function assignBuiltinAgent(taskId: string, taskVersion: number) {
  const { apiRequest } = await import("../api/client");
  await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(taskId)}/assignments`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "If-Match": `"${taskVersion}"`,
      },
      body: JSON.stringify({
        role: "assignee",
        actor_id: BUILTIN_AGENT_ACTOR_ID,
      }),
    },
  );
}
