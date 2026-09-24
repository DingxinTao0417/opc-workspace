import { useEffect, useRef, useState } from "react";
import { Download } from "lucide-react";
import { ApiError, downloadInvoicePdf } from "../api/client";
import type { AiInvoicePdfResult as PdfResult } from "../api/aiInvoiceActions";

function downloadError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === "INVOICE_PDF_CHANGED")
      return "这份 PDF 已被替换，不能从旧卡下载另一份文件。请查看发票详情或请求新的生成建议。";
    if (
      [
        "INVOICE_NOT_FOUND",
        "INVOICE_PDF_NOT_FOUND",
        "INVOICE_PDF_FILE_MISSING",
      ].includes(error.code ?? "")
    )
      return "本次发票或 PDF 已不存在。历史生成记录保留，不会自动重新生成。";
    if (error.code === "INVOICE_PDF_INTEGRITY_MISMATCH")
      return "文件完整性校验失败，已阻止下载。请检查存储或请求新的生成建议。";
    if (error.code === "INVOICE_PDF_STORAGE_UNAVAILABLE")
      return "本地 PDF 存储暂不可用，请检查后重试。";
  }
  return "本次 PDF 下载失败，未保存文件；可以重试，不会重新生成或替换文件。";
}

export function AiInvoicePdfResult({ result }: { result: PdfResult }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const request = useRef<AbortController | null>(null);
  useEffect(
    () => () => {
      request.current?.abort();
      request.current = null;
    },
    [result.asset_id],
  );
  const download = async () => {
    if (request.current) return;
    setError("");
    setNotice("");
    if (typeof URL.createObjectURL !== "function") {
      setError("当前环境不支持文件下载，请在桌面或支持下载的浏览器中打开。");
      return;
    }
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    try {
      const file = await downloadInvoicePdf(
        result.invoice_id,
        result.file_name,
        {
          assetId: result.asset_id,
          sha256: result.sha256,
          sizeBytes: result.size_bytes,
        },
        controller.signal,
      );
      if (controller.signal.aborted || request.current !== controller) return;
      const url = URL.createObjectURL(file.blob);
      const anchor = document.createElement("a");
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
        setError(downloadError(e));
    } finally {
      if (request.current === controller) {
        request.current = null;
        setBusy(false);
      }
    }
  };
  return (
    <section className="ai-split-preview" aria-label="本次生成的发票 PDF">
      <p>
        <strong>{result.file_name}</strong>
      </p>
      <p className="ai-action-note">
        已在本机生成 · {result.size_bytes.toLocaleString()} 字节 · 发票版本{" "}
        {result.generated_from_version}
      </p>
      <p className="ai-action-note">
        生成于 {new Date(result.generated_at).toLocaleString()}
        。这是历史生成结果；下载时重新核对文件身份和完整性。发票状态未改变，未发送给客户或模型。
      </p>
      <details>
        <summary>文件校验信息</summary>
        <p className="ai-action-note">SHA-256：{result.sha256}</p>
      </details>
      <button
        type="button"
        className="button button-secondary"
        disabled={busy}
        onClick={() => void download()}
      >
        <Download aria-hidden="true" size={14} />
        {busy ? "正在核验并下载…" : "下载本次 PDF"}
      </button>
      {error ? (
        <p role="alert" className="ai-action-note">
          {error}
        </p>
      ) : null}
      {notice ? (
        <p role="status" className="ai-action-note">
          {notice}
        </p>
      ) : null}
    </section>
  );
}
