import { Link } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { aiWorkspaceHref } from "../lib/aiWorkspaceLinks";
import { ReturnToAiChat } from "./ClientRecordLocation";
import {
  useAgentRunQuery,
  useRetryAgentRunOutputDelivery,
  useTaskAgentRunsQuery,
  useTaskQuery,
  invalidateAgentRunDeliveryFacts,
} from "../api/hooks";
import { ApiError } from "../api/client";
import { agentRunDeliveryReason } from "../lib/agentRunDelivery";
import { agentRunIssueHandoff } from "../lib/aiIssueHandoff";
import { agentRunFailureReason } from "../lib/agentRunFailureSource";
import { agentRunLifecycle } from "../lib/agentRunLifecycle";
import {
  agentRunElapsedLabel,
  agentRunProgressLabel,
} from "../lib/agentRunProgress";
import { taskSubmissionHref } from "../lib/taskSubmissionLocation";
import { useUiStore } from "../store/ui";
import type { AgentRun } from "../types/models";
import { AiIssueHandoffButton } from "./AiWorkbenchHandoff";
import { Modal } from "./Modal";
import { useEffect, useState } from "react";
import { AgentRunRestartModal } from "./AgentRunRestartModal";
import { canRestartAgentRun } from "../lib/agentRunRestart";
import {
  agentRunFileApplyCandidates,
  useAgentRunFileApply,
} from "../store/agentRunFileApply";

const deliveryStatusLabels: Record<AgentRun["outputDeliveryStatus"], string> = {
  not_ready: "未进入产出登记",
  pending: "登记待恢复",
  submitted: "已提交，待人工验收",
  retained: "结果已保留，未提交",
};

function runStatusLabel(run: AgentRun): string {
  return agentRunLifecycle(run).label;
}

function RunTime({ value }: { value: string | null }) {
  if (!value || Number.isNaN(Date.parse(value))) return <>—</>;
  return (
    <time dateTime={value}>
      {new Intl.DateTimeFormat("zh-CN", {
        dateStyle: "medium",
        timeStyle: "medium",
        hour12: false,
      }).format(new Date(value))}
    </time>
  );
}

function downloadResult(title: string, run: AgentRun) {
  if (run.resultText === null) return;
  const filename =
    title.replace(/[\\/:*?"<>|\u0000-\u001f]/g, "_").slice(0, 80) || "任务";
  const url = URL.createObjectURL(
    new Blob([run.resultText], { type: "text/plain;charset=utf-8" }),
  );
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `${filename}-第${run.attempt}次.txt`;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 0);
}

export function AgentRunDrawer() {
  const queryClient = useQueryClient();
  const [restartRunId, setRestartRunId] = useState<string | null>(null);
  const drawer = useUiStore((state) => state.agentRunDrawer);
  const closeDrawer = useUiStore((state) => state.closeAgentRunDrawer);
  const openDrawer = useUiStore((state) => state.openAgentRunDrawer);
  const setTaskDetailId = useUiStore((state) => state.setTaskDetailId);
  const taskId = drawer?.taskId ?? null;
  useEffect(() => setRestartRunId(null), [taskId, drawer?.runId]);
  const taskQuery = useTaskQuery(taskId);
  const runsQuery = useTaskAgentRunsQuery(taskId, Boolean(drawer));
  const runs = runsQuery.data ?? [];
  const exactRunQuery = useAgentRunQuery(
    drawer?.runId ?? null,
    taskId,
    Boolean(drawer?.runId),
  );
  const retryDelivery = useRetryAgentRunOutputDelivery();
  const selectedRun = drawer?.runId ? exactRunQuery.data : runs[0];
  const taskTitle = taskQuery.data?.title ?? "任务执行";
  const fileApplyCandidates = selectedRun
    ? agentRunFileApplyCandidates(selectedRun, taskTitle)
    : [];
  const deliveryPending =
    selectedRun?.status === "running" &&
    selectedRun.outputDeliveryStatus === "pending";
  const active =
    selectedRun?.status === "queued" ||
    (selectedRun?.status === "running" && !deliveryPending);

  return (
    <Modal
      open={Boolean(drawer)}
      onClose={() => {
        if (!retryDelivery.isPending) closeDrawer();
      }}
      dismissible={!retryDelivery.isPending}
      title="执行过程"
      closeLabel="收起执行过程"
      width="480px"
      panelClassName="agent-run-drawer"
      bodyClassName="agent-run-drawer-body"
      footer={
        <>
          <ReturnToAiChat
            sessionId={drawer?.returnSession}
            disabled={retryDelivery.isPending}
            onNavigate={closeDrawer}
          />
          <button
            className="button button-secondary"
            disabled={!taskId || retryDelivery.isPending}
            onClick={() => {
              if (!taskId) return;
              setTaskDetailId(taskId);
              closeDrawer();
            }}
            type="button"
          >
            打开任务
          </button>
          <button
            className="button button-secondary"
            disabled={retryDelivery.isPending}
            onClick={closeDrawer}
            type="button"
          >
            收起
          </button>
        </>
      }
    >
      {taskId && restartRunId ? (
        <AgentRunRestartModal
          key={`${taskId}:${restartRunId}`}
          taskId={taskId}
          runId={restartRunId}
          onClose={() => setRestartRunId(null)}
          onSuccess={(run) => {
            setRestartRunId(null);
            void invalidateAgentRunDeliveryFacts(queryClient, run.taskId);
            void runsQuery.refetch();
            openDrawer(taskId, run.id, drawer?.returnSession);
          }}
        />
      ) : null}
      <header className="agent-run-drawer-heading">
        <h3>{taskTitle}</h3>
        <p className="agent-run-drawer-note">
          这里只展示服务端执行记录。执行成功不等于任务已验收完成。
        </p>
      </header>
      {taskQuery.isPending && drawer ? (
        <p role="status">正在读取任务信息…</p>
      ) : null}
      {taskQuery.isError ? (
        <div className="agent-run-drawer-error" role="alert">
          <p>无法读取任务信息，执行记录仍可单独查看。</p>
          <button
            className="button button-secondary"
            onClick={() => void taskQuery.refetch()}
            type="button"
          >
            重新读取任务
          </button>
        </div>
      ) : null}
      {runsQuery.isError ? (
        <div className="agent-run-drawer-error" role="alert">
          <p>无法读取最新执行记录，请刷新重试。</p>
          <button
            className="button button-secondary"
            disabled={runsQuery.isFetching}
            onClick={() => void runsQuery.refetch()}
            type="button"
          >
            刷新执行记录
          </button>
        </div>
      ) : null}
      {runsQuery.isPending && drawer ? (
        <p role="status">正在读取执行记录…</p>
      ) : !runsQuery.isError && runs.length === 0 ? (
        <p className="agent-run-drawer-empty" role="status">
          此任务尚无执行记录。
        </p>
      ) : null}
      {drawer?.runId && exactRunQuery.isPending ? (
        <p role="status">正在精确读取所选执行记录…</p>
      ) : null}
      {drawer?.runId && exactRunQuery.isError ? (
        <div className="agent-run-drawer-error" role="alert">
          <p>
            {exactRunQuery.error instanceof ApiError &&
            exactRunQuery.error.code === "AGENT_RUN_TASK_MISMATCH"
              ? "所选执行记录不属于当前任务，未展示其他记录代替。"
              : "无法精确读取所选执行记录，未展示其他记录代替。"}
          </p>
          <button
            className="button button-secondary"
            disabled={exactRunQuery.isFetching}
            onClick={() => void exactRunQuery.refetch()}
            type="button"
          >
            重新读取所选记录
          </button>
        </div>
      ) : null}
      {runs.length > 0 ? (
        <div className="agent-run-drawer-history">
          <label htmlFor="agent-run-history">执行记录</label>
          <select
            id="agent-run-history"
            disabled={retryDelivery.isPending}
            value={selectedRun?.id ?? ""}
            onChange={(event) => {
              if (taskId)
                openDrawer(
                  taskId,
                  event.currentTarget.value,
                  drawer?.returnSession,
                );
            }}
          >
            {!selectedRun ? <option value="">选择执行记录…</option> : null}
            {selectedRun && !runs.some((run) => run.id === selectedRun.id) ? (
              <option value={selectedRun.id}>
                第 {selectedRun.attempt} 次 · {runStatusLabel(selectedRun)} ·{" "}
                {selectedRun.model}
              </option>
            ) : null}
            {runs.map((run) => (
              <option key={run.id} value={run.id}>
                第 {run.attempt} 次 · {runStatusLabel(run)} · {run.model}
              </option>
            ))}
          </select>
        </div>
      ) : null}
      {selectedRun ? (
        <>
          <dl className="agent-run-drawer-facts">
            <div>
              <dt>状态</dt>
              <dd>
                <span
                  aria-live="polite"
                  className="task-agent-run-status"
                  data-phase={agentRunLifecycle(selectedRun).phase}
                  data-status={selectedRun.status}
                >
                  {runStatusLabel(selectedRun)}
                </span>
              </dd>
            </div>
            <div>
              <dt>模型</dt>
              <dd>{selectedRun.model}</dd>
            </div>
            <div>
              <dt>执行次数</dt>
              <dd>第 {selectedRun.attempt} 次</dd>
            </div>
            <div>
              <dt>执行链</dt>
              <dd>
                {selectedRun.parentRunId ? (
                  <button
                    className="button button-quiet"
                    onClick={() => {
                      if (!taskId) return;
                      openDrawer(
                        taskId,
                        selectedRun.parentRunId!,
                        drawer?.returnSession,
                      );
                    }}
                    type="button"
                  >
                    打开重试来源
                  </button>
                ) : (
                  "首次执行"
                )}
                {selectedRun.parentRunId ? (
                  <code>{selectedRun.parentRunId}</code>
                ) : null}
              </dd>
            </div>
            {selectedRun.startGate ? (
              <div>
                <dt>启动条件</dt>
                <dd>
                  {selectedRun.startGate.status === "waiting" ? (
                    <>
                      等待前置执行的产出
                      {selectedRun.startGate.require === "accepted"
                        ? "被人工验收接受"
                        : "登记为任务提交"}
                      ，截止 <RunTime value={selectedRun.startGate.expiresAt} />
                    </>
                  ) : selectedRun.startGate.status === "released" ? (
                    "前置条件已满足，已进入普通执行队列"
                  ) : (
                    agentRunLifecycle(selectedRun).detail
                  )}{" "}
                  <code>{selectedRun.startGate.predecessorRunId}</code>
                </dd>
              </div>
            ) : null}
            <div>
              <dt>创建时间</dt>
              <dd>
                <RunTime value={selectedRun.createdAt} />
              </dd>
            </div>
            <div>
              <dt>开始时间</dt>
              <dd>
                <RunTime value={selectedRun.startedAt} />
              </dd>
            </div>
            <div>
              <dt>结束时间</dt>
              <dd>
                <RunTime value={selectedRun.completedAt} />
              </dd>
            </div>
            <div>
              <dt>错误码</dt>
              <dd>
                {selectedRun.errorCode ? (
                  <>
                    <code>{selectedRun.errorCode}</code>
                    {agentRunFailureReason(selectedRun.errorCode) ? (
                      <p>{agentRunFailureReason(selectedRun.errorCode)}</p>
                    ) : null}
                  </>
                ) : (
                  "无"
                )}
              </dd>
            </div>
            <div>
              <dt>产出登记</dt>
              <dd>{deliveryStatusLabels[selectedRun.outputDeliveryStatus]}</dd>
            </div>
          </dl>
          {selectedRun.status === "running" && selectedRun.progress ? (
            <p
              className="agent-run-drawer-progress"
              role="status"
              aria-live="polite"
            >
              <span>{agentRunProgressLabel(selectedRun.progress)}</span>
              <span aria-hidden="true">
                {` · 已运行 ${agentRunElapsedLabel(selectedRun.progress.elapsedMs)}`}
              </span>
            </p>
          ) : null}
          <section
            className="agent-run-drawer-result"
            aria-label="产出登记状态"
          >
            {selectedRun.restartOfRunId ? (
              <p>
                按当前事实重新执行 · 关联原运行{" "}
                <code>{selectedRun.restartOfRunId}</code>；旧记录与结果保留。
              </p>
            ) : null}
            <h3>产出登记</h3>
            {selectedRun.outputDeliveryStatus === "pending" ? (
              <>
                <p>
                  执行结果已经保留，服务端仍在收口任务产出。状态刷新只会读取；再次登记必须由你手动触发。
                </p>
                <button
                  className="button button-secondary"
                  disabled={retryDelivery.isPending}
                  onClick={() =>
                    retryDelivery.mutate({
                      runId: selectedRun.id,
                      taskId: selectedRun.taskId,
                    })
                  }
                  type="button"
                >
                  {retryDelivery.isPending ? "正在重试登记…" : "重试登记产出"}
                </button>
              </>
            ) : selectedRun.outputDeliveryStatus === "submitted" &&
              selectedRun.submissionId &&
              selectedRun.artifactId ? (
              <>
                <p>已创建待验收提交；执行成功没有代替人工验收。</p>
                <Link
                  onClick={(event) => {
                    if (
                      event.defaultPrevented ||
                      event.button !== 0 ||
                      event.ctrlKey ||
                      event.metaKey ||
                      event.shiftKey ||
                      event.altKey
                    )
                      return;
                    closeDrawer();
                  }}
                  to={aiWorkspaceHref(
                    taskSubmissionHref(
                      selectedRun.taskId,
                      selectedRun.submissionId,
                    ),
                    drawer?.returnSession,
                  )}
                >
                  打开提交批次
                </Link>
                <p>
                  产出 ID：<code>{selectedRun.artifactId}</code>
                </p>
              </>
            ) : selectedRun.outputDeliveryStatus === "retained" ? (
              <p>
                {agentRunDeliveryReason(selectedRun.outputDeliveryErrorCode)}
              </p>
            ) : (
              <p>此次执行尚未产生可登记的任务产出。</p>
            )}
            {canRestartAgentRun(selectedRun) ? (
              <button
                type="button"
                className="button button-secondary"
                disabled={retryDelivery.isPending}
                onClick={() => setRestartRunId(selectedRun.id)}
              >
                按当前事实重新执行
              </button>
            ) : null}
            {retryDelivery.isError &&
            selectedRun.outputDeliveryStatus === "pending" ? (
              <p role="alert">
                {retryDelivery.error instanceof ApiError &&
                retryDelivery.error.code === "AGENT_OUTPUT_DELIVERY_PENDING"
                  ? "登记仍在恢复中，结果已保留；稍后可再次手动重试。"
                  : retryDelivery.error instanceof ApiError &&
                      retryDelivery.error.code ===
                        "AGENT_OUTPUT_DELIVERY_NOT_PENDING"
                    ? "登记状态已经变化，未重复处理；正在读取最新状态。"
                    : "重试登记失败，结果仍保留在本次执行记录中。"}
              </p>
            ) : null}
          </section>
          <section className="agent-run-drawer-result" aria-label="执行结果">
            <div className="agent-run-drawer-section-heading">
              <h3>执行结果</h3>
              <AiIssueHandoffButton
                disabled={retryDelivery.isPending}
                content={agentRunIssueHandoff({
                  ...selectedRun,
                  taskTitle,
                })}
              />
            </div>
            {selectedRun.resultText !== null ? (
              <>
                {fileApplyCandidates.length > 0 ? (
                  <div className="agent-run-drawer-actions">
                    {fileApplyCandidates.map((candidate) => (
                      <button
                        className="button button-secondary"
                        key={`${candidate.artifactId}:${candidate.index}`}
                        onClick={() => {
                          if (
                            !useAgentRunFileApply.getState().select(candidate)
                          )
                            return;
                          useUiStore.setState({
                            rightOverviewCollapsed: false,
                          });
                          useUiStore
                            .getState()
                            .setRightPanelTab("files", "activate");
                          closeDrawer();
                        }}
                        type="button"
                      >
                        用于项目文件：{candidate.name}
                      </button>
                    ))}
                  </div>
                ) : null}
                <button
                  className="button button-secondary"
                  onClick={() => downloadResult(taskTitle, selectedRun)}
                  type="button"
                >
                  下载文本
                </button>
                <pre>{selectedRun.resultText}</pre>
              </>
            ) : (
              <p className="agent-run-drawer-empty">
                {deliveryPending
                  ? "执行已结束，结果正文暂存于受保护的恢复区；产出登记完成后才会显示。"
                  : active
                    ? "执行尚未结束，结果以服务端完成记录为准。"
                    : "此次执行没有可用文本产出。"}
              </p>
            )}
          </section>
        </>
      ) : null}
    </Modal>
  );
}
