import { CalendarClock, LoaderCircle } from "lucide-react";
import { useEffect, useState } from "react";
import { ApiError } from "../api/client";
import { useSetTaskPlannedDate } from "../api/hooks";
import type { Task } from "../types/models";
import { Modal } from "./Modal";

function shiftDateKey(dateKey: string, days: number): string {
  const [year, month, day] = dateKey.split("-").map(Number);
  const date = new Date(Date.UTC(year, month - 1, day + days));
  return date.toISOString().slice(0, 10);
}

function planErrorText(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "任务已被其他操作更新，列表已刷新。请关闭后基于最新日期重新安排。";
    }
    if (error.code === "NETWORK_ERROR" || error.code === "TIMEOUT") {
      return "无法确认改期结果，任务列表已刷新。请核对当前日期后再重试。";
    }
    return error.message;
  }
  return "无法保存计划日期，请重试。";
}

export function TaskPlanModal({
  open,
  task,
  selectedDate,
  onClose,
}: {
  open: boolean;
  task: Task | null;
  selectedDate: string;
  onClose: () => void;
}) {
  const mutation = useSetTaskPlannedDate();
  const [plannedDate, setPlannedDate] = useState("");

  useEffect(() => {
    if (!open || !task) return;
    setPlannedDate(task.plannedDate ?? "");
    mutation.reset();
  }, [open, task]);

  const close = () => {
    if (!mutation.isPending) onClose();
  };
  const save = async () => {
    if (!task || mutation.isPending) return;
    const nextDate = plannedDate || null;
    if (nextDate === task.plannedDate) {
      onClose();
      return;
    }
    try {
      await mutation.mutateAsync({
        taskId: task.id,
        expectedVersion: task.version,
        plannedDate: nextDate,
      });
      onClose();
    } catch {
      // Mutation state renders the actionable error while preserving the form.
    }
  };

  return (
    <Modal
      dismissible={!mutation.isPending}
      footer={
        <>
          <button
            className="button button-secondary"
            disabled={mutation.isPending}
            onClick={close}
            type="button"
          >
            取消
          </button>
          <button
            className="button button-primary"
            disabled={!task || mutation.isPending}
            onClick={() => void save()}
            type="button"
          >
            {mutation.isPending ? (
              <>
                <LoaderCircle className="animate-spin" size={14} />
                保存中…
              </>
            ) : (
              "保存计划"
            )}
          </button>
        </>
      }
      onClose={close}
      open={open}
      title="安排任务日期"
      width="460px"
    >
      {task ? (
        <div className="task-plan-modal">
          <div className="task-plan-summary">
            <CalendarClock size={16} />
            <div>
              <span>正在安排</span>
              <strong>{task.title}</strong>
            </div>
          </div>
          <div aria-label="日期快捷选择" className="task-plan-presets">
            <button
              aria-pressed={plannedDate === selectedDate}
              className="button button-secondary"
              onClick={() => {
                mutation.reset();
                setPlannedDate(selectedDate);
              }}
              type="button"
            >
              所选日期
            </button>
            <button
              aria-pressed={plannedDate === shiftDateKey(selectedDate, 1)}
              className="button button-secondary"
              onClick={() => {
                mutation.reset();
                setPlannedDate(shiftDateKey(selectedDate, 1));
              }}
              type="button"
            >
              后一天
            </button>
            <button
              aria-pressed={plannedDate === ""}
              className="button button-secondary"
              onClick={() => {
                mutation.reset();
                setPlannedDate("");
              }}
              type="button"
            >
              未排期
            </button>
          </div>
          <label className="form-label" htmlFor="task-plan-date">
            计划日期
          </label>
          <input
            autoFocus
            id="task-plan-date"
            onChange={(event) => {
              mutation.reset();
              setPlannedDate(event.target.value);
            }}
            type="date"
            value={plannedDate}
          />
          <p className="form-hint">
            留空表示移回未排期；改期会清除原计划组的手动顺序。
          </p>
          {mutation.isError ? (
            <p className="task-plan-error" role="alert">
              {planErrorText(mutation.error)}
            </p>
          ) : null}
        </div>
      ) : null}
    </Modal>
  );
}
