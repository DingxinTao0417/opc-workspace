import { useEffect, useRef, useState } from "react";
import { ApiError, downloadApprovedFinancialCSV } from "../api/client";
import type { AiActionProposal } from "../api/aiWorkspaceActions";

export function AiFinanceExportDetails({
  proposal,
}: {
  proposal: AiActionProposal;
}) {
  const p = proposal.preview.after;
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  async function download() {
    if (request.current) return;
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const file = await downloadApprovedFinancialCSV(
        {
          id: proposal.id,
          fingerprint: proposal.fingerprint,
          sha256: String(p.sha256),
          sizeBytes: Number(p.size_bytes),
        },
        controller.signal,
      );
      if (controller.signal.aborted || request.current !== controller) return;
      const url = URL.createObjectURL(file.blob),
        anchor = document.createElement("a");
      try {
        anchor.href = url;
        anchor.download = file.fileName;
        document.body.append(anchor);
        anchor.click();
      } finally {
        anchor.remove();
        setTimeout(() => URL.revokeObjectURL(url), 0);
      }
      setNotice("已交给浏览器下载，请在下载列表确认保存结果。");
    } catch (e) {
      if (!controller.signal.aborted && request.current === controller)
        setError(
          e instanceof ApiError &&
            ["AI_EXPORT_CHANGED", "EXPORT_TOO_LARGE"].includes(e.code ?? "")
            ? "匹配数据已变化或超出导出上限，请让智能体重新提议并核对；不会下载其他数据。"
            : e instanceof ApiError && e.code === "AI_ACTION_NOT_FOUND"
              ? "导出审批已不存在，无法下载。"
              : "下载未完成，请重试；不会改用当前筛选或重新批准。",
        );
    } finally {
      if (request.current === controller) {
        request.current = null;
        setBusy(false);
      }
    }
  }
  const types: Record<string, string> = {
    all: "全部收支",
    income: "收入",
    expense: "支出",
  };
  const statuses: Record<string, string> = {
    active: "待确认 + 已确认（不含作废）",
    all: "全部（含作废）",
    pending: "待确认",
    confirmed: "已确认",
    voided: "仅作废",
  };
  return (
    <section className="ai-split-preview" aria-label="财务 CSV 导出范围">
      <dl>
        <div>
          <dt>币种</dt>
          <dd>{p.currency}</dd>
        </div>
        <div>
          <dt>发生日期（含首尾）</dt>
          <dd>
            {p.date_from} 至 {p.date_to}
          </dd>
        </div>
        <div>
          <dt>收支类型</dt>
          <dd>{types[String(p.entry_type)]}</dd>
        </div>
        <div>
          <dt>记录状态</dt>
          <dd>{statuses[String(p.export_status)]}</dd>
        </div>
        <div>
          <dt>分类（精确匹配）</dt>
          <dd>{p.category || "全部分类"}</dd>
        </div>
        <div>
          <dt>客户 ID</dt>
          <dd>{p.client_id || "全部客户（含未关联）"}</dd>
        </div>
        <div>
          <dt>项目 ID</dt>
          <dd>{p.project_id || "全部项目（含未关联）"}</dd>
        </div>
        <div>
          <dt>全部匹配记录</dt>
          <dd>
            {p.row_count} 条 · {Number(p.size_bytes).toLocaleString()} 字节
          </dd>
        </div>
      </dl>
      <p className="ai-action-note">
        导出全部匹配记录，不限当前页。按发生日期倒序、创建时间倒序、ID
        正序排列，零条时仅导出表头。
      </p>
      <p className="ai-action-note">
        包含
        ID、收支类型、状态、最小货币单位金额、币种、发生日期、分类、客户名称、项目名称、发票编号、完整备注和创建/更新时间。备注和名称可能含敏感信息，CSV
        不自动发送给模型或外部。易被表格识别为公式的文本会加单引号，不修改原始记录。
      </p>
      <p className="ai-action-note">
        确认只批准这里展示的范围与内容指纹，不修改账本，也不代表已下载。下载前重新核验；数据变化需重新提议。本应用不保存历史
        CSV 正文，删除会话会移除对应审批。
      </p>
      <details>
        <summary>内容校验信息</summary>
        <p>SHA-256：{p.sha256}</p>
      </details>
      {proposal.status === "confirmed" ? (
        <button
          className="button button-secondary"
          type="button"
          disabled={busy}
          onClick={() => void download()}
        >
          {busy ? "正在核验并下载…" : "下载已批准的 CSV"}
        </button>
      ) : null}
      {error ? <p role="alert">{error}</p> : null}
      {notice ? <p role="status">{notice}</p> : null}
    </section>
  );
}
