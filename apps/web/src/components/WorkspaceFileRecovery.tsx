import { useEffect, useRef, useState } from "react";
import {
  workspaceApi,
  workspaceError,
  type WorkspaceRoot,
  type WorkspaceFileRecoveryList,
  type WorkspaceFileRecoveryPreview,
} from "../api/workspace";
import { useWorkspaceFileChanges } from "../store/workspaceFileChanges";
import { useFileOperationReminders } from "../store/fileOperationReminders";
import { fileReviewDiff, visibleFileLine } from "./aiFileReviewDiff";
import { projectFileWriteGate } from "../lib/projectFileWriteGate";

import { recoveryDiff } from "./workspaceFileRecoveryDiff";
export { recoveryBytes, recoveryDiff } from "./workspaceFileRecoveryDiff";
import { WorkspaceFileReplacementReview } from "./WorkspaceFileReplacementReview";

export function WorkspaceFileRecovery({ root }: { root: WorkspaceRoot }) {
  const [list, setList] = useState<WorkspaceFileRecoveryList | null>(null);
  const [replacement, setReplacement] = useState<
    WorkspaceFileRecoveryList["records"][number] | null
  >(null);
  const [preview, setPreview] = useState<
    (WorkspaceFileRecoveryPreview & { expiresAt: number }) | null
  >(null);
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState("");
  const revision = useRef(0);
  const lock = useRef(false);
  const held = useRef<WorkspaceFileRecoveryPreview | null>(null);
  const release = (value: WorkspaceFileRecoveryPreview | null) => {
    if (value)
      void workspaceApi
        .releaseFileSnapshot(root.id, value.snapshotId)
        .catch(() => {});
  };
  const clearPreview = () => {
    setReplacement(null);
    release(held.current);
    held.current = null;
    setPreview(null);
    setConfirmed(false);
  };
  const load = async () => {
    if (lock.current) return;
    const token = ++revision.current;
    setError("");
    setList(null);
    setResult("");
    clearPreview();
    setBusy(true);
    lock.current = true;
    try {
      const data = await workspaceApi.fileRecoveryList(root.id);
      if (token === revision.current) setList(data);
    } catch (e) {
      if (token === revision.current) setError(workspaceError(e));
    } finally {
      if (token === revision.current) {
        setBusy(false);
        lock.current = false;
      }
    }
  };
  useEffect(() => {
    void load();
    return () => {
      ++revision.current;
      release(held.current);
      held.current = null;
      lock.current = false;
    };
  }, [root.id]);
  useEffect(() => {
    if (!preview) return;
    const timer = window.setTimeout(
      () => {
        if (held.current === preview && !lock.current) {
          clearPreview();
          setError("恢复预览已过期，请重新核对");
        }
      },
      Math.max(0, preview.expiresAt - Date.now()),
    );
    return () => window.clearTimeout(timer);
  }, [preview]);
  const select = async (
    record: WorkspaceFileRecoveryList["records"][number],
  ) => {
    if (lock.current) return;
    const token = ++revision.current;
    clearPreview();
    setError("");
    setResult("");
    if (record.version === 2) {
      setReplacement(record);
      return;
    }
    if (record.version !== undefined && record.version !== 1) {
      setError("不支持此恢复记录版本");
      return;
    }
    setBusy(true);
    lock.current = true;
    let value: WorkspaceFileRecoveryPreview | undefined;
    try {
      value = await workspaceApi.fileRecoveryPreview(root.id, record.id);
      if (token !== revision.current) {
        release(value);
        return;
      }
      if (
        value.id !== record.id ||
        value.path !== record.path ||
        !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(
          value.snapshotId,
        ) ||
        !Number.isInteger(value.expiresInSeconds) ||
        value.expiresInSeconds < 1 ||
        value.expiresInSeconds > 600
      )
        throw new Error("恢复预览身份无效");
      recoveryDiff(value); // Validate both complete byte payloads before review.
      const next = {
        ...value,
        expiresAt: Date.now() + value.expiresInSeconds * 1000,
      };
      held.current = next;
      setPreview(next);
    } catch (e) {
      if (value) release(value);
      if (token === revision.current) setError(workspaceError(e));
    } finally {
      if (token === revision.current) {
        setBusy(false);
        lock.current = false;
      }
    }
  };
  const restore = async () => {
    if (!projectFileWriteGate.enabled) return;
    if (!preview || held.current !== preview || !confirmed || lock.current)
      return;
    if (preview.expiresAt <= Date.now()) {
      clearPreview();
      setError("恢复预览已过期，请重新核对");
      return;
    }
    const token = ++revision.current;
    const operationId = crypto.randomUUID();
    setConfirmed(false);
    setBusy(true);
    lock.current = true;
    const reminder = useFileOperationReminders
      .getState()
      .begin("file_recovery");
    try {
      const outcome = await workspaceApi.applyFileRecovery(
        root.id,
        preview.id,
        preview.snapshotId,
        operationId,
      );
      if (outcome.id !== operationId) throw new Error("恢复回执身份不一致");
      useFileOperationReminders
        .getState()
        .settle(
          reminder,
          outcome.status === "applied" ? "confirmed" : "uncertain",
        );
      if (token !== revision.current) return;
      setResult(
        outcome.status === "applied"
          ? `恢复写入及完整核验成功。恢复前的内容也已备份：${outcome.backupPath}`
          : "恢复结果需核对；请刷新记录并重新读取文件，不要重复提交。",
      );
    } catch {
      useFileOperationReminders.getState().settle(reminder, "uncertain");
      if (token === revision.current)
        setError(
          "恢复被拒绝或结果未确定。请刷新恢复记录和文件，重新读取并审查后再操作；不会自动重试。",
        );
    } finally {
      useWorkspaceFileChanges.getState().changed();
      if (token === revision.current) {
        clearPreview();
        setBusy(false);
        lock.current = false;
      }
    }
  };
  const diff = preview ? recoveryDiff(preview) : null;
  return (
    <section className="ai-workbench-handoff" aria-label="文件恢复记录">
      <strong>文件恢复 · {root.name}</strong>
      <p>
        记录是实验性写入前备份，不代表写入成功。仅显示所选目录的记录，点击后只读核对完整差异，不调用
        AI。记录含未加密的全文和路径，请保护此目录；每个版本目录最多 64
        项，不自动删除，也不包含在业务数据库备份内。版本 2 仅可只读检查。
      </p>
      {!projectFileWriteGate.enabled && (
        <p role="status">{projectFileWriteGate.reason}</p>
      )}
      {list && (
        <p>
          本地恢复目录：<code>{list.directory}</code>
          {list.replacementDirectory && (
            <>
              ；替换记录目录：<code>{list.replacementDirectory}</code>
            </>
          )}
        </p>
      )}
      {list && list.damaged > 0 && (
        <p role="alert">
          发现 {list.damaged}{" "}
          个无法识别或损坏的记录，不能自动恢复；原文件不会因此被修改。
        </p>
      )}
      <button
        type="button"
        className="button button-quiet"
        disabled={busy}
        onClick={() => void load()}
      >
        刷新恢复记录
      </button>
      {busy && <p role="status">正在处理，请勿重复操作…</p>}
      {error && <p role="alert">{error}</p>}
      {result && <p role="status">{result}</p>}
      {list?.records.length === 0 && <p>所选目录暂无可读取的恢复记录。</p>}
      {list?.records.map((record) => (
        <div key={`${record.version ?? 1}:${record.id}`}>
          <button
            type="button"
            className="button button-secondary"
            disabled={busy}
            onClick={() => void select(record)}
          >
            核对恢复 {record.path} · {record.id.slice(0, 8)}
            {record.version === 2 ? " · 替换记录 v2" : ""}
          </button>
        </div>
      ))}
      {replacement && (
        <WorkspaceFileReplacementReview
          key={`${root.id}:${replacement.id}`}
          root={root}
          record={replacement}
          onClose={() => setReplacement(null)}
        />
      )}
      {preview && diff && (
        <>
          <h4>当前文件 → 恢复原文：{preview.path}</h4>
          <p>
            {diff.binary
              ? "内容包含非 UTF-8 或二进制字节，以下为完整十六进制差异；不会用替换字符覆盖原文。"
              : "以下显示完整转义文本差异，保留 BOM 和换行差异。"}
          </p>
          <pre
            className="ws-source ai-project-file-source ai-file-review-diff"
            aria-label="完整恢复差异"
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
          {preview.currentBase64 === preview.originalBase64 ? (
            <p>当前内容与原文相同，无需恢复。</p>
          ) : projectFileWriteGate.enabled ? (
            <>
              <label>
                <input
                  type="checkbox"
                  disabled={busy}
                  checked={confirmed}
                  onChange={(e) => setConfirmed(e.target.checked)}
                />
                我已核对完整差异，同意覆盖当前内容并备份恢复前版本
              </label>
              <button
                type="button"
                className="button button-primary"
                disabled={!confirmed || busy}
                onClick={() => void restore()}
              >
                确认恢复此文件
              </button>
            </>
          ) : null}
          <button
            type="button"
            className="button button-quiet"
            disabled={busy}
            onClick={clearPreview}
          >
            取消恢复预览
          </button>
        </>
      )}
    </section>
  );
}
