import { useTaskAgentRunsQuery, useTaskQuery } from "../api/hooks";
import { useUiStore } from "../store/ui";
import type { AgentRun } from "../types/models";
import { Modal } from "./Modal";

const statusLabels: Record<AgentRun["status"], string> = {
  queued: "排队中",
  running: "执行中",
  succeeded: "执行成功",
  failed: "执行失败",
  cancelled: "已取消",
  interrupted: "已中断",
};

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
  const drawer = useUiStore((state) => state.agentRunDrawer);
  const closeDrawer = useUiStore((state) => state.closeAgentRunDrawer);
  const openDrawer = useUiStore((state) => state.openAgentRunDrawer);
  const setTaskDetailId = useUiStore((state) => state.setTaskDetailId);
  const taskId = drawer?.taskId ?? null;
  const taskQuery = useTaskQuery(taskId);
  const runsQuery = useTaskAgentRunsQuery(taskId, Boolean(drawer));
  const runs = runsQuery.data ?? [];
  const selectedRun = drawer?.runId
    ? runs.find((run) => run.id === drawer.runId)
    : runs[0];
  const taskTitle = taskQuery.data?.title ?? "任务执行";
  const active =
    selectedRun?.status === "queued" || selectedRun?.status === "running";

  return (
    <Modal
      open={Boolean(drawer)}
      onClose={closeDrawer}
      title="执行过程"
      closeLabel="收起执行过程"
      width="480px"
      panelClassName="agent-run-drawer"
      bodyClassName="agent-run-drawer-body"
      footer={
        <>
          <button
            className="button button-secondary"
            disabled={!taskId}
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
            onClick={closeDrawer}
            type="button"
          >
            收起
          </button>
        </>
      }
    >
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
      {runs.length > 0 ? (
        <div className="agent-run-drawer-history">
          <label htmlFor="agent-run-history">执行记录</label>
          <select
            id="agent-run-history"
            value={selectedRun?.id ?? ""}
            onChange={(event) => {
              if (taskId) openDrawer(taskId, event.currentTarget.value);
            }}
          >
            {!selectedRun ? <option value="">选择执行记录…</option> : null}
            {runs.map((run) => (
              <option key={run.id} value={run.id}>
                第 {run.attempt} 次 · {statusLabels[run.status]} · {run.model}
              </option>
            ))}
          </select>
        </div>
      ) : null}
      {!runsQuery.isPending &&
      !runsQuery.isError &&
      drawer?.runId &&
      !selectedRun &&
      runs.length > 0 ? (
        <p className="agent-run-drawer-empty" role="status">
          所选执行记录已不可用，请选择其他历史记录。
        </p>
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
                  data-status={selectedRun.status}
                >
                  {statusLabels[selectedRun.status]}
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
                  <code>{selectedRun.errorCode}</code>
                ) : (
                  "无"
                )}
              </dd>
            </div>
          </dl>
          <section className="agent-run-drawer-result" aria-label="执行结果">
            <h3>执行结果</h3>
            {selectedRun.resultText !== null ? (
              <>
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
                {active
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
