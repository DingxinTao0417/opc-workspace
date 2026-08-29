import { useEffect, useMemo, useState, type FormEvent } from "react";
import { ApiError } from "../api/client";
import { useCreateProject, useUpdateProject } from "../api/hooks";
import type { Project } from "../types/models";
import { ClientSelect } from "./ClientSelect";
import { Modal } from "./Modal";

const projectColors = [
  "#6E7BF2",
  "#8B5CF6",
  "#2FAE83",
  "#E09A3E",
  "#E5484D",
  "#4B93E6",
];

function amountToInput(amountMinor: number | null): string {
  if (amountMinor === null) return "";
  const whole = Math.floor(amountMinor / 100);
  const fraction = amountMinor % 100;
  return fraction === 0
    ? String(whole)
    : `${whole}.${String(fraction).padStart(2, "0")}`;
}

function parseAmountMinor(value: string): number | null | undefined {
  const clean = value.trim();
  if (!clean) return null;
  if (!/^\d+(?:\.\d{1,2})?$/.test(clean)) return undefined;
  const [whole, fraction = ""] = clean.split(".");
  const amount = Number(whole) * 100 + Number(fraction.padEnd(2, "0"));
  return Number.isSafeInteger(amount) ? amount : undefined;
}

function apiErrorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "项目已在其他窗口修改，正在重新加载最新内容。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "项目保存失败，请重试。";
}

export function ProjectFormModal({
  open,
  project,
  onClose,
  onVersionConflict,
}: {
  open: boolean;
  project?: Project;
  onClose: () => void;
  onVersionConflict?: () => void;
}) {
  const createMutation = useCreateProject();
  const updateMutation = useUpdateProject();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [clientId, setClientId] = useState("");
  const [startDate, setStartDate] = useState("");
  const [dueDate, setDueDate] = useState("");
  const [amount, setAmount] = useState("");
  const [color, setColor] = useState(projectColors[0]);
  const [validationError, setValidationError] = useState<string | null>(null);
  const busy = createMutation.isPending || updateMutation.isPending;

  useEffect(() => {
    if (!open) return;
    setName(project?.name ?? "");
    setDescription(project?.description ?? "");
    setClientId(project?.clientId ?? "");
    setStartDate(project?.startDate ?? "");
    setDueDate(project?.dueDate ?? "");
    setAmount(amountToInput(project?.amountMinor ?? null));
    setColor(project?.color ?? projectColors[0]);
    setValidationError(null);
    createMutation.reset();
    updateMutation.reset();
  }, [open, project]);

  const errorMessage = useMemo(
    () =>
      validationError ??
      apiErrorMessage(createMutation.error) ??
      apiErrorMessage(updateMutation.error),
    [createMutation.error, updateMutation.error, validationError],
  );

  const close = () => {
    if (!busy) onClose();
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const cleanName = name.trim();
    if (cleanName.length < 2 || cleanName.length > 100) {
      setValidationError("项目名称需要 2–100 个字符。");
      return;
    }
    if (startDate && dueDate && startDate > dueDate) {
      setValidationError("截止日期不能早于开始日期。");
      return;
    }
    const amountMinor = parseAmountMinor(amount);
    if (amountMinor === undefined) {
      setValidationError("项目金额需为非负数，最多保留两位小数。");
      return;
    }
    setValidationError(null);
    const input = {
      name: cleanName,
      description,
      clientId: clientId || null,
      startDate: startDate || null,
      dueDate: dueDate || null,
      amountMinor,
      color,
    };
    if (project) {
      updateMutation.mutate(
        {
          id: project.id,
          input: { ...input, expectedVersion: project.version },
        },
        {
          onError: (error) => {
            if (
              error instanceof ApiError &&
              error.code === "VERSION_CONFLICT"
            ) {
              onVersionConflict?.();
            }
          },
          onSuccess: onClose,
        },
      );
    } else {
      createMutation.mutate(input, { onSuccess: onClose });
    }
  };

  return (
    <Modal
      footer={
        <>
          <button
            className="button button-secondary"
            disabled={busy}
            onClick={close}
            type="button"
          >
            取消
          </button>
          <button
            className="button button-primary"
            disabled={name.trim().length < 2 || busy}
            form="project-form"
            type="submit"
          >
            {busy ? "正在保存…" : project ? "保存修改" : "创建项目"}
          </button>
        </>
      }
      onClose={close}
      open={open}
      title={project ? "编辑项目" : "新建项目"}
      width="600px"
    >
      <form id="project-form" onSubmit={submit}>
        <label className="form-field">
          <span>项目名称</span>
          <input
            autoFocus
            maxLength={100}
            onChange={(event) => setName(event.target.value)}
            placeholder="例如：品牌官网改版"
            value={name}
          />
        </label>

        <label className="form-field">
          <span>描述</span>
          <textarea
            maxLength={10_000}
            onChange={(event) => setDescription(event.target.value)}
            placeholder="说明目标、范围和交付边界…"
            rows={4}
            value={description}
          />
        </label>

        <div className="form-grid">
          <label className="form-field">
            <span>开始日期</span>
            <input
              onChange={(event) => setStartDate(event.target.value)}
              type="date"
              value={startDate}
            />
          </label>
          <label className="form-field">
            <span>截止日期</span>
            <input
              onChange={(event) => setDueDate(event.target.value)}
              type="date"
              value={dueDate}
            />
          </label>
        </div>

        <div className="form-grid">
          <div className="form-field">
            <span>客户</span>
            <ClientSelect
              ariaLabel="客户"
              emptyLabel="不关联客户"
              onChange={setClientId}
              selectedName={
                clientId === project?.clientId ? project.clientName : undefined
              }
              value={clientId}
              variant="form"
            />
          </div>
          <label className="form-field">
            <span>合同金额</span>
            <div className="field-with-suffix">
              <input
                aria-label="合同金额"
                inputMode="decimal"
                onChange={(event) => setAmount(event.target.value)}
                placeholder="0.00"
                value={amount}
              />
              <span>元</span>
            </div>
          </label>
        </div>

        <fieldset className="form-field form-field-last">
          <legend>颜色标记</legend>
          <div className="project-color-picker">
            {projectColors.map((item) => (
              <button
                aria-label={`选择颜色 ${item}`}
                aria-pressed={color === item}
                className={
                  color === item
                    ? "project-color project-color-active"
                    : "project-color"
                }
                key={item}
                onClick={() => setColor(item)}
                style={{ backgroundColor: item }}
                type="button"
              />
            ))}
          </div>
        </fieldset>

        {errorMessage ? (
          <div className="form-error" role="alert">
            {errorMessage}
          </div>
        ) : null}
        <p className="form-note">
          金额仅作为项目合同信息保存在本地，不会生成收入或发票记录。
        </p>
      </form>
    </Modal>
  );
}
