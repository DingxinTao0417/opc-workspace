import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  cancelAgentRun,
  createAgentRun,
  getAiProviders,
  getAllActors,
  retryAgentRun,
  ApiError,
} from "../api/client";
import {
  actorQueryKey,
  useAgentAdaptersQuery,
  useCreateTaskAssignment,
  taskAgentRunsQueryKey,
  useTaskAgentRunsQuery,
  useTaskAssignmentsQuery,
} from "../api/hooks";
import { useUiStore } from "../store/ui";
import type {
  Actor,
  AgentAdapter,
  AgentRun,
  Task,
  TaskAssignmentListResult,
} from "../types/models";

function executionSetup(adapters: AgentAdapter[], actors: Actor[]) {
  const adapter = adapters.find(
    (item) => item.adapterKey === "builtin-local-text-v1",
  );
  if (!adapter)
    return {
      problem:
        "尚未登记内置 Agent，请先在「设置 → 本地 Agent」登记、检查并启用适配器。",
    };
  if (!adapter.executionReady || adapter.healthStatus !== "healthy") {
    return {
      problem:
        "内置 Agent 尚未通过当前平台的运行检查，请在「设置 → 本地 Agent」检查运行条件。",
    };
  }
  if (adapter.status !== "enabled")
    return {
      problem:
        "内置 Agent 尚未启用，请先在「设置 → 本地 Agent」显式启用适配器。",
    };
  const matches = actors.filter(
    (actor) =>
      actor.type === "agent" &&
      actor.status === "active" &&
      actor.agentAdapterId === adapter.id,
  );
  if (matches.length !== 1)
    return {
      problem:
        "未找到唯一可执行的 Agent 身份，请在「设置 → 本地 Agent」检查启用状态后刷新。",
    };
  return { actor: matches[0] };
}

function assignmentProblem(
  assignments: TaskAssignmentListResult,
  actor: Actor,
) {
  const assignee = assignments.active.assignee;
  return assignee && assignee.actorId !== actor.id
    ? "当前任务已有其他负责人，请先在「责任分派」中处理分派；不会自动替换现有负责人。"
    : null;
}

function executionError(error: unknown) {
  if (error instanceof ApiError) {
    switch (error.code) {
      case "ACTOR_NOT_FOUND":
      case "ASSIGNMENT_ACTOR_NOT_ACTIVE":
      case "ASSIGNMENT_ACTOR_NOT_EXECUTABLE":
      case "AGENT_RUN_NOT_EXECUTABLE":
        return "Agent 身份或启用状态已变化，请检查「设置 → 本地 Agent」并刷新执行状态。";
      case "VERSION_CONFLICT":
      case "ASSIGNMENT_ALREADY_ACTIVE":
        return "任务或负责人已变化，请刷新并确认最新分派后重试。";
      case "AGENT_PROVIDER_INVALID":
        return "所选模型当前不可用于执行，请检查模型配置后重新选择。";
    }
  }
  return error instanceof Error
    ? error.message
    : "执行操作失败，请刷新状态后重试。";
}

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

function downloadRunResult(taskTitle: string, run: AgentRun) {
  if (!run.resultText) return;
  const isHtml = /<!doctype html|<html/i.test(run.resultText);
  const blob = new Blob([run.resultText], {
    type: isHtml ? "text/html;charset=utf-8" : "text/markdown;charset=utf-8",
  });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `${taskTitle}${isHtml ? ".html" : ".md"}`;
  anchor.click();
  URL.revokeObjectURL(url);
}

function formatRunTime(value: string | null) {
  return value ? new Date(value).toLocaleString() : "—";
}

export function TaskAgentRunsSection({
  task,
  disabled,
}: TaskAgentRunsSectionProps) {
  const queryClient = useQueryClient();
  const openAgentRunDrawer = useUiStore((state) => state.openAgentRunDrawer);
  const [providerId, setProviderId] = useState("");
  const [expandedRunId, setExpandedRunId] = useState<string | null>(null);
  const adaptersQuery = useAgentAdaptersQuery();
  const actorsQuery = useQuery({
    queryKey: [...actorQueryKey, "agent-execution-options"],
    queryFn: ({ signal }) =>
      getAllActors({ type: "agent", status: "active" }, signal),
    enabled:
      adaptersQuery.data?.some(
        (adapter) => adapter.status === "enabled" && adapter.executionReady,
      ) ?? false,
    retry: false,
    staleTime: 10_000,
  });
  const assignmentsQuery = useTaskAssignmentsQuery(task.id);
  const assign = useCreateTaskAssignment();

  const runsQuery = useTaskAgentRunsQuery(task.id);

  const providersQuery = useQuery({
    queryKey: ["ai-providers-for-agent"],
    queryFn: () => getAiProviders(),
  });
  const usableProviders = (providersQuery.data ?? [])
    .filter(
      (provider) =>
        provider.status === "ready" && provider.health_status === "healthy",
    )
    .filter((provider) => provider.protocol === "openai_chat");
  const setup = executionSetup(
    adaptersQuery.data ?? [],
    actorsQuery.data ?? [],
  );
  const assignments = assignmentsQuery.data?.pages[0];
  const setupLoading =
    adaptersQuery.isPending ||
    assignmentsQuery.isPending ||
    (actorsQuery.isEnabled && actorsQuery.isPending);
  const setupError =
    adaptersQuery.error ?? actorsQuery.error ?? assignmentsQuery.error;
  const readinessProblem = setupLoading
    ? "正在检查 Agent 与任务分派状态…"
    : setupError
      ? "无法读取 Agent 或任务分派状态，请刷新重试。"
      : (setup.problem ??
        (setup.actor && assignments
          ? assignmentProblem(assignments, setup.actor)
          : "无法确认当前任务分派，请刷新重试。"));
  const taskProblem =
    task.status !== "todo" && task.status !== "in_progress"
      ? "当前任务状态不可启动执行，请先将任务恢复为待办或执行中。"
      : null;
  const activeRun = runsQuery.data?.some(
    (run) => run.status === "queued" || run.status === "running",
  );
  const selectedProvider = usableProviders.find(
    (provider) => provider.id === providerId,
  );
  const startProblem =
    readinessProblem ??
    taskProblem ??
    (activeRun ? "任务已有正在进行的执行，请等待结束或先取消。" : null);

  async function refreshExecutionState() {
    return Promise.all([
      adaptersQuery.refetch(),
      actorsQuery.refetch(),
      assignmentsQuery.refetch(),
      providersQuery.refetch(),
      runsQuery.refetch(),
    ]);
  }

  async function checkBeforeExecution() {
    const [adapters, actors, taskAssignments, providers, runs] =
      await refreshExecutionState();
    if (
      adapters.error ||
      actors.error ||
      taskAssignments.error ||
      providers.error ||
      runs.error
    ) {
      throw new Error("无法确认最新执行状态，本次未启动，请刷新后重试。");
    }
    const current = executionSetup(adapters.data ?? [], actors.data ?? []);
    if (!current.actor) throw new Error(current.problem);
    const currentAssignments = taskAssignments.data?.pages[0];
    if (!currentAssignments)
      throw new Error("无法读取当前任务分派，本次未启动。");
    const conflict = assignmentProblem(currentAssignments, current.actor);
    if (conflict || taskProblem) throw new Error(conflict ?? taskProblem!);
    if (
      runs.data?.some(
        (run) => run.status === "queued" || run.status === "running",
      )
    ) {
      throw new Error("任务已有正在进行的执行，请等待结束或先取消。");
    }
    return {
      actor: current.actor,
      assignments: currentAssignments,
      providers: providers.data ?? [],
    };
  }

  function requireProvider(
    providers: NonNullable<typeof providersQuery.data>,
    id: string,
  ) {
    if (
      !providers.some(
        (provider) =>
          provider.id === id &&
          provider.status === "ready" &&
          provider.health_status === "healthy" &&
          provider.protocol === "openai_chat",
      )
    ) {
      throw new Error("所选模型不可用于执行，请选择已就绪的 OpenAI 兼容模型。");
    }
  }

  const invalidate = () => {
    void queryClient.invalidateQueries({
      queryKey: taskAgentRunsQueryKey(task.id),
    });
  };

  const startMutation = useMutation({
    mutationFn: async () => {
      const current = await checkBeforeExecution();
      requireProvider(current.providers, providerId);
      if (!current.assignments.active.assignee) {
        await assign.mutateAsync({
          taskId: task.id,
          input: {
            role: "assignee",
            actorId: current.actor.id,
            expectedVersion: current.assignments.meta.taskVersion,
          },
        });
      }
      return createAgentRun(task.id, providerId);
    },
    onSuccess: (run) => {
      setProviderId("");
      invalidate();
      openAgentRunDrawer(task.id, run.id);
    },
  });

  const cancelMutation = useMutation({
    mutationFn: (runId: string) => cancelAgentRun(runId),
    onSuccess: invalidate,
  });
  const retryMutation = useMutation({
    mutationFn: async (run: AgentRun) => {
      const current = await checkBeforeExecution();
      if (
        current.assignments.active.assignee?.actorId !== run.actorId ||
        current.actor.id !== run.actorId
      ) {
        throw new Error(
          "原执行的负责人已变化，不能直接重试；请确认分派后重新启动。",
        );
      }
      requireProvider(current.providers, run.providerId);
      return retryAgentRun(run.id);
    },
    onSuccess: invalidate,
  });

  const runs = runsQuery.data ?? [];
  const busy =
    startMutation.isPending ||
    cancelMutation.isPending ||
    retryMutation.isPending;

  return (
    <section className="task-section task-agent-runs">
      <div className="task-outputs-heading task-agent-runs-heading">
        <div>
          <h3>Agent 执行</h3>
          <p>
            由已启用的内置 Agent
            调用所选模型生成交付文本；满足人工验收条件时提交产出，否则保留在执行记录中。
          </p>
        </div>
        <button
          className="button button-secondary"
          onClick={() => openAgentRunDrawer(task.id, runs[0]?.id)}
          type="button"
        >
          查看执行过程
        </button>
      </div>
      <div className="task-agent-runs-controls">
        <select
          aria-label="选择执行模型"
          disabled={
            disabled ||
            busy ||
            Boolean(startProblem) ||
            providersQuery.isPending ||
            providersQuery.isError
          }
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
          className="button button-primary"
          disabled={
            disabled ||
            busy ||
            !selectedProvider ||
            Boolean(startProblem) ||
            providersQuery.isError ||
            runsQuery.isPending ||
            runsQuery.isError
          }
          onClick={() => startMutation.mutate()}
          type="button"
        >
          启动执行
        </button>
      </div>
      {startProblem ? (
        <p className="task-agent-runs-alert" role="status">
          {startProblem}
        </p>
      ) : !assignments?.active.assignee ? (
        <p className="task-agent-runs-empty">
          点击「启动执行」会先将此任务分派给「{setup.actor?.displayName}
          」，再调用所选模型。
        </p>
      ) : null}
      {providersQuery.isError ? (
        <p role="alert">无法读取执行模型，请刷新重试。</p>
      ) : providersQuery.isPending ? null : usableProviders.length === 0 ? (
        <p role="status">
          暂无已就绪的 OpenAI 兼容模型，请在「设置 → AI 助手」配置。
        </p>
      ) : null}
      <button
        className="button button-secondary"
        disabled={
          busy ||
          adaptersQuery.isFetching ||
          actorsQuery.isFetching ||
          assignmentsQuery.isFetching ||
          providersQuery.isFetching ||
          runsQuery.isFetching
        }
        onClick={() => {
          startMutation.reset();
          retryMutation.reset();
          cancelMutation.reset();
          void refreshExecutionState();
        }}
        type="button"
      >
        刷新执行状态
      </button>
      {startMutation.error ? (
        <p className="task-agent-runs-alert" role="alert">
          {executionError(startMutation.error)}
        </p>
      ) : null}
      {cancelMutation.error || retryMutation.error ? (
        <p className="task-agent-runs-alert" role="alert">
          {executionError(cancelMutation.error ?? retryMutation.error)}
        </p>
      ) : null}
      {runsQuery.isError ? (
        <p className="task-agent-runs-alert" role="alert">
          无法读取执行记录，请刷新重试。
        </p>
      ) : runsQuery.isPending ? (
        <p role="status">正在读取执行记录…</p>
      ) : runs.length === 0 ? (
        <p className="task-agent-runs-empty">尚无执行记录。</p>
      ) : (
        <ul className="task-agent-runs-list">
          {runs.map((run) => (
            <li
              key={run.id}
              className="task-agent-run-card"
              data-status={run.status}
            >
              <div className="task-agent-run-heading">
                <span
                  className="task-agent-run-status"
                  data-status={run.status}
                >
                  {statusLabels[run.status]}
                </span>
                <span className="task-agent-run-meta">
                  第 {run.attempt} 次 · {run.model} ·{" "}
                  {formatRunTime(run.completedAt ?? run.startedAt)}
                </span>
                <span className="task-agent-run-actions">
                  <button
                    className="button button-secondary"
                    onClick={() => openAgentRunDrawer(task.id, run.id)}
                    type="button"
                  >
                    查看过程
                  </button>
                  {run.status === "queued" || run.status === "running" ? (
                    <button
                      className="button button-secondary"
                      disabled={disabled || busy}
                      onClick={() => cancelMutation.mutate(run.id)}
                      type="button"
                    >
                      取消
                    </button>
                  ) : null}
                  {run.status !== "queued" && run.status !== "running" ? (
                    <button
                      className="button button-secondary"
                      disabled={
                        disabled ||
                        busy ||
                        Boolean(startProblem) ||
                        assignments?.active.assignee?.actorId !== run.actorId
                      }
                      onClick={() => retryMutation.mutate(run)}
                      type="button"
                    >
                      重试
                    </button>
                  ) : null}
                  {run.resultText ? (
                    <>
                      <button
                        className="button button-secondary"
                        onClick={() =>
                          setExpandedRunId((current) =>
                            current === run.id ? null : run.id,
                          )
                        }
                        type="button"
                      >
                        {expandedRunId === run.id ? "收起产出" : "查看产出"}
                      </button>
                      <button
                        className="button button-secondary"
                        onClick={() => downloadRunResult(task.title, run)}
                        type="button"
                      >
                        下载
                      </button>
                    </>
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
