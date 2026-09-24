import { useEffect, useRef, useState } from "react";
import {
  workspaceApi,
  workspaceError,
  type WorkspaceRoot,
  type WorkspaceFileRecoveryList,
  type WorkspaceFileReplacementPreview,
  type ReplacementFileState,
  type WorkspaceFileReplacementReview as ReviewTicket,
  type FileRecoveryMode,
} from "../api/workspace";
import { recoveryBytes, recoveryDiff } from "./workspaceFileRecoveryDiff";
import { fileReviewDiff, visibleFileLine } from "./aiFileReviewDiff";
import { useProjectFileWriteCapability } from "../lib/projectFileWriteGate";
import { useWorkspaceFileChanges } from "../store/workspaceFileChanges";
import { useFileOperationReminders } from "../store/fileOperationReminders";

const states: Record<ReplacementFileState, string> = {
  missing: "路径不存在（不是空文件）",
  original: "原对象与记录原文相符",
  candidate: "候选对象与候选全文相符",
  changed: "记录对象的正文已变化",
  foreign: "被其他文件占用，未读取其正文",
  unavailable: "无法核对（占用、权限或不支持的文件）",
};
const observations = {
  source_present: "原对象仍在目标位置",
  target_missing: "目标路径缺失，停放原对象和候选仍在",
  candidate_present: "候选对象位于目标位置，原对象仍被保留",
  conflict: "存在冲突或无法完整核对",
};
function validate(
  value: WorkspaceFileReplacementPreview,
  record: WorkspaceFileRecoveryList["records"][number],
) {
  if (
    value.id !== record.id ||
    value.path !== record.path ||
    !Object.hasOwn(observations, value.observation) ||
    ![value.target, value.parked, value.staged].every((s) =>
      Object.hasOwn(states, s),
    )
  )
    throw new Error("替换记录预览身份或状态无效");
  recoveryBytes(value.originalBase64);
  recoveryBytes(value.candidateBase64);
  const readable = ["original", "candidate", "changed"].includes(value.target);
  if (
    readable !== (typeof value.currentBase64 === "string") ||
    (!readable && value.currentBase64 !== null)
  )
    throw new Error("缺失或不可读的目标不能显示为空文件");
  if (typeof value.currentBase64 === "string")
    recoveryBytes(value.currentBase64);
  if (
    (value.target === "original" &&
      value.currentBase64 !== value.originalBase64) ||
    (value.target === "candidate" &&
      value.currentBase64 !== value.candidateBase64)
  )
    throw new Error("目标状态与完整正文不一致");
  const actual =
    value.target === "original" &&
    value.parked === "missing" &&
    value.staged === "candidate"
      ? "source_present"
      : value.target === "missing" &&
          value.parked === "original" &&
          value.staged === "candidate"
        ? "target_missing"
        : value.target === "candidate" &&
            value.parked === "original" &&
            value.staged === "missing"
          ? "candidate_present"
          : "conflict";
  if (actual !== value.observation) throw new Error("替换记录观察状态不一致");
}
function FullDiff({
  before,
  after,
  label,
}: {
  before: string;
  after: string;
  label: string;
}) {
  const diff = recoveryDiff({ currentBase64: before, originalBase64: after });
  return (
    <>
      <h5>{label}</h5>
      <p>
        {diff.binary
          ? "完整十六进制差异，未使用有损解码。"
          : "完整转义文本差异，保留 BOM 和换行差异。"}
      </p>
      <pre
        className="ws-source ai-project-file-source ai-file-review-diff"
        aria-label={label}
      >
        {fileReviewDiff(diff.before, diff.after).map((part, i) => (
          <span key={i} className={`ai-file-diff-${part.kind}`}>
            {part.lines
              .map(
                (line) =>
                  `${part.kind === "added" ? "+" : part.kind === "removed" ? "-" : " "} ${visibleFileLine(line)}\n`,
              )
              .join("")}
          </span>
        ))}
      </pre>
    </>
  );
}

// Observing creates no ticket. A condition check freezes one exact request;
// applying it still requires an independent native review and per-use UAC.
type ReviewProps = {
  root: WorkspaceRoot;
  record: WorkspaceFileRecoveryList["records"][number];
  onClose: () => void;
};
export function WorkspaceFileReplacementReview({
  root,
  record,
  onClose,
}: ReviewProps) {
  // A new selection/record owns a new lifecycle immediately, not only after an
  // effect clears old data. Late callbacks keep their original release scope.
  return (
    <ScopedReplacementReview
      key={JSON.stringify([root.id, root.path, record.id, record.path])}
      root={root}
      record={record}
      onClose={onClose}
    />
  );
}
function ScopedReplacementReview({ root, record, onClose }: ReviewProps) {
  const [value, setValue] = useState<WorkspaceFileReplacementPreview | null>(
    null,
  );
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(true);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    setValue(null);
    setError("");
    setBusy(true);
    void workspaceApi
      .fileReplacementPreview(root.id, record.id)
      .then((data) => {
        if (!active) return;
        validate(data, record);
        setValue(data);
        timer = setTimeout(() => {
          if (active) {
            setValue(null);
            setError("只读观察已过期，请重新核对");
          }
        }, 600000);
      })
      .catch((e: unknown) => {
        if (active) setError(workspaceError(e));
      })
      .finally(() => {
        if (active) setBusy(false);
      });
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [root.id, record.id, record.path, revision]);
  return (
    <section aria-label="替换记录核对">
      <h4>替换记录 v2 · {record.path}</h4>
      <p>
        初步观察仅核对文件身份与主文；可另行冻结一次恢复条件审查，但仍不代表写入成功或可安全恢复。
        各路径可能继续变化；内容不会向 AI 发送，只有后续独立原生确认和本次
        Windows 系统授权都通过，才会处理这一份文件。
      </p>
      {busy && <p role="status">正在只读核对替换记录…</p>}
      {error && <p role="alert">{error}</p>}
      {value && (
        <>
          <p role="status">{observations[value.observation]}</p>
          <ul>
            <li>目标路径：{states[value.target]}</li>
            <li>停放原对象：{states[value.parked]}</li>
            <li>候选暂存：{states[value.staged]}</li>
          </ul>
          {(value.observation === "target_missing" ||
            value.observation === "candidate_present") && (
            <section aria-label="待审查的恢复方向">
              <h5>
                {value.observation === "target_missing"
                  ? "缺失恢复：缺失路径 → 原对象"
                  : "原样撤销：当前候选 → 原对象"}
              </h5>
              <p>以下仅解释拟恢复的方向，不是执行许可；当前不会修改文件。</p>
              {value.observation === "target_missing" ? (
                <>
                  <p>
                    将把保留的原对象移回 <code>{value.path}</code>
                    ；目标当前不存在，不是空文件。候选对象仍会保留，不安装候选内容。
                  </p>
                  {value.originalBase64 === "" && (
                    <p>原对象是空文件；恢复后路径会存在，大小为 0 字节。</p>
                  )}
                  <FullDiff
                    before=""
                    after={value.originalBase64}
                    label="缺失路径 → 记录原文（新增目标）"
                  />
                </>
              ) : (
                <>
                  <p>
                    候选先移回空闲暂存位置，再把原对象移回目标；两份内容都会保留，不覆盖后来新增的文件。
                  </p>
                  <FullDiff
                    before={value.currentBase64!}
                    after={value.originalBase64}
                    label="当前目标主文 → 记录原文"
                  />
                </>
              )}
            </section>
          )}
          <details>
            <summary>历史修改对照（不是本次恢复方向）</summary>
            <FullDiff
              before={value.originalBase64}
              after={value.candidateBase64}
              label="记录原文 → 候选全文"
            />
          </details>
          {value.observation !== "candidate_present" &&
            value.currentBase64 !== null &&
            (value.currentBase64 === value.originalBase64 ? (
              <p>当前目标主文与记录原文相同；这不证明元数据相同。</p>
            ) : (
              <FullDiff
                before={value.currentBase64}
                after={value.originalBase64}
                label="当前目标主文 → 记录原文"
              />
            ))}
          {(value.observation === "target_missing" ||
            value.observation === "candidate_present") && (
            <RecoveryConditionCheck
              key={revision}
              root={root}
              baseline={value}
              mode={
                value.observation === "target_missing"
                  ? "missing_target"
                  : "undo_installed"
              }
            />
          )}
        </>
      )}
      <button
        className="button button-quiet"
        type="button"
        disabled={busy}
        onClick={() => {
          setValue(null);
          setRevision((r) => r + 1);
        }}
      >
        重新核对替换状态
      </button>
      <button className="button button-quiet" type="button" onClick={onClose}>
        关闭替换记录预览
      </button>
    </section>
  );
}

function RecoveryConditionCheck({
  root,
  baseline,
  mode,
}: {
  root: WorkspaceRoot;
  baseline: WorkspaceFileReplacementPreview;
  mode: FileRecoveryMode;
}) {
  const [busy, setBusy] = useState(false);
  const [applying, setApplying] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [reviewed, setReviewed] = useState(false);
  const [confirmed, setConfirmed] = useState(false);
  const [operationId, setOperationId] = useState<string | null>(null);
  const [result, setResult] = useState("");
  const [error, setError] = useState("");
  const capability = useProjectFileWriteCapability();
  const active = useRef(true);
  const locked = useRef(false);
  const held = useRef<ReviewTicket | null>(null);
  const activeOperation = useRef<string | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const changed = useWorkspaceFileChanges((state) => state.changed);
  const release = (ticket: ReviewTicket | null) => {
    if (ticket)
      void workspaceApi
        .releaseReplacementReview(root.id, ticket.reviewId)
        .catch(() => {});
  };
  const clear = () => {
    clearTimeout(timer.current);
    if (activeOperation.current)
      void workspaceApi
        .cancelFileOperation(root.id, activeOperation.current)
        .catch(() => {});
    release(held.current);
    held.current = null;
  };
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
      clear();
    };
  }, [root.id, baseline.id, mode]);
  const check = async () => {
    if (locked.current) return;
    locked.current = true;
    setBusy(true);
    clear();
    setReviewed(false);
    setConfirmed(false);
    setResult("");
    setError("");
    let result: ReviewTicket | null = null;
    try {
      result = await workspaceApi.reviewFileReplacement(
        root.id,
        baseline.id,
        mode,
      );
      if (!active.current) {
        release(result);
        return;
      }
      validate(result, {
        id: baseline.id,
        path: baseline.path,
        createdAt: null,
        restores: null,
      });
      if (
        result.mode !== mode ||
        result.observation !== baseline.observation ||
        result.originalBase64 !== baseline.originalBase64 ||
        result.candidateBase64 !== baseline.candidateBase64 ||
        result.currentBase64 !== baseline.currentBase64 ||
        !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(
          result.reviewId,
        ) ||
        !Number.isInteger(result.expiresInSeconds) ||
        result.expiresInSeconds < 1 ||
        result.expiresInSeconds > 600
      )
        throw new Error("审查结果与当前完整差异不一致，请重新查看替换记录");
      held.current = result;
      setReviewed(true);
      timer.current = setTimeout(() => {
        clear();
        if (active.current) {
          setReviewed(false);
          setError("恢复条件审查已过期，请重新复核");
        }
      }, result.expiresInSeconds * 1000);
    } catch (e) {
      release(result);
      if (active.current) setError(workspaceError(e));
    } finally {
      locked.current = false;
      if (active.current) setBusy(false);
    }
  };
  const apply = async () => {
    const ticket = held.current;
    if (
      locked.current ||
      applying ||
      !reviewed ||
      !confirmed ||
      !capability.available ||
      !ticket
    )
      return;
    locked.current = true;
    setApplying(true);
    setConfirmed(false);
    setError("");
    setResult("");
    clearTimeout(timer.current);
    const operation = crypto.randomUUID();
    activeOperation.current = operation;
    setOperationId(operation);
    const reminder = useFileOperationReminders
      .getState()
      .begin("file_replacement_restore");
    try {
      const outcome = await workspaceApi.applyFileReplacement(
        root.id,
        ticket.reviewId,
        operation,
      );
      if (
        outcome.id !== operation ||
        !["applied", "cancelled"].includes(outcome.status)
      )
        throw new Error("恢复回执身份或状态无效");
      useFileOperationReminders.getState().settle(reminder, "confirmed");
      if (!active.current) return;
      held.current = null;
      setReviewed(false);
      if (outcome.status === "applied") {
        setResult(
          mode === "missing_target"
            ? "原对象已恢复到缺失路径，并完成完整核验。"
            : "当前候选已撤回，原对象已恢复，并完成完整核验。",
        );
      } else {
        setError(
          "本次原生确认或系统授权已取消，旧审查已消费；请重新核对替换状态后再发起。",
        );
      }
    } catch (reason) {
      useFileOperationReminders.getState().settle(reminder, "uncertain");
      if (active.current) {
        setReviewed(false);
        setError(
          `恢复结果未确定，不要重复提交；请重新核对替换记录和当前文件。${workspaceError(reason) ? ` ${workspaceError(reason)}` : ""}`,
        );
      }
    } finally {
      changed();
      release(ticket);
      held.current = null;
      activeOperation.current = null;
      locked.current = false;
      if (active.current) {
        setApplying(false);
        setOperationId(null);
      }
    }
  };
  return (
    <div aria-label="本地恢复条件复核">
      <button
        className="button button-secondary"
        type="button"
        disabled={busy || applying}
        onClick={() => void check()}
      >
        {busy ? "正在复核恢复条件…" : "复核恢复条件（只读）"}
      </button>
      {error && <p role="alert">{error}</p>}
      {result && <p role="status">{result}</p>}
      {reviewed && (
        <>
          <p role="status">
            已冻结本次恢复审查，最多保留十分钟。网页不会直接写文件；下一步将由独立原生窗口复核精确方向，再由
            Windows
            为这一次操作请求授权。关闭或取消不会回滚已经开始的原生文件交换。
          </p>
          {!capability.available ? (
            <p role="status">{capability.reason}</p>
          ) : (
            <>
              <label>
                <input
                  type="checkbox"
                  checked={confirmed}
                  disabled={applying}
                  onChange={(event) => setConfirmed(event.target.checked)}
                />
                我已核对本次恢复方向，同意进入原生确认并按次请求系统授权
              </label>
              <button
                className="button button-primary"
                type="button"
                disabled={!confirmed || applying}
                onClick={() => void apply()}
              >
                {applying
                  ? "正在等待本次操作回执…"
                  : mode === "missing_target"
                    ? "确认恢复此文件"
                    : "确认撤销此文件"}
              </button>
            </>
          )}
        </>
      )}
      {applying && operationId && (
        <button
          className="button button-secondary"
          type="button"
          disabled={cancelling}
          onClick={() => {
            setCancelling(true);
            void workspaceApi
              .cancelFileOperation(root.id, operationId)
              .catch((reason: unknown) => setError(workspaceError(reason)))
              .finally(() => setCancelling(false));
          }}
        >
          {cancelling ? "正在请求取消…" : "取消本次操作"}
        </button>
      )}
    </div>
  );
}
