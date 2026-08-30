import { Download, FileText, LoaderCircle, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";
import { ApiError } from "../api/client";
import {
  useDownloadInvoicePdf,
  useGenerateInvoicePdf,
  useInvoicePdfQuery,
} from "../api/hooks";
import type { Invoice } from "../types/models";
import { ErrorState, LoadingState } from "./feedback";

interface InvoicePdfSectionProps {
  invoice: Invoice;
  onInvoiceConflict?: () => Promise<void> | void;
}

function formatTimestamp(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function formatFileSize(value: number): string {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MiB`;
}

function pdfErrorMessage(error: unknown, fallback: string): string {
  if (!(error instanceof ApiError)) return fallback;
  if (error.code === "INVOICE_PDF_STORAGE_UNAVAILABLE") {
    return "本地 PDF 存储暂不可用，请检查数据目录后重试。";
  }
  if (error.code === "INVOICE_PDF_NOT_FOUND") {
    return "本地 PDF 已不存在，请重新生成。";
  }
  if (error.code === "INVOICE_PDF_FILE_MISSING") {
    return "本地 PDF 文件已缺失，请重新生成。";
  }
  if (error.code === "INVOICE_PDF_INTEGRITY_MISMATCH") {
    return "本地 PDF 完整性校验失败，请重新生成。";
  }
  if (error.code === "IDEMPOTENCY_REPLAY_UNAVAILABLE") {
    return "上一次生成结果已被替换，请再次点击生成。";
  }
  return error.message || fallback;
}

export function InvoicePdfSection({
  invoice,
  onInvoiceConflict,
}: InvoicePdfSectionProps) {
  const query = useInvoicePdfQuery(invoice.id);
  const generateMutation = useGenerateInvoicePdf();
  const downloadMutation = useDownloadInvoicePdf();
  const [operationError, setOperationError] = useState<string | null>(null);
  const [operationNotice, setOperationNotice] = useState<string | null>(null);
  const metadata = query.data ?? null;
  const operationBusy =
    generateMutation.isPending || downloadMutation.isPending;
  const metadataKnown = query.data !== undefined;
  const canOperate =
    !query.isPending && (!query.isError || metadataKnown) && !query.isFetching;
  const generatedFromOldVersion =
    metadata !== null && metadata.generatedFromVersion !== invoice.version;
  const integrityUnavailable =
    metadata !== null && metadata.integrityStatus !== "verified";

  useEffect(() => {
    setOperationError(null);
    setOperationNotice(null);
  }, [invoice.id, invoice.version]);

  const generatePdf = async () => {
    if (!canOperate || operationBusy) return;
    setOperationError(null);
    setOperationNotice(null);
    try {
      const result = await generateMutation.mutateAsync({
        id: invoice.id,
        expectedVersion: invoice.version,
      });
      setOperationNotice(
        `${result.fileName} 已在本机生成；发票状态未改变，也不会自动发送给客户。`,
      );
    } catch (error) {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        setOperationNotice("发票版本已变化，正在读取最新详情…");
        try {
          await onInvoiceConflict?.();
        } catch {
          setOperationError("发票版本已变化，但最新详情读取失败，请重试。");
        }
        return;
      }
      setOperationError(pdfErrorMessage(error, "本地 PDF 生成失败，请重试。"));
    }
  };

  const downloadPdf = async () => {
    if (!metadata || integrityUnavailable || !canOperate || operationBusy)
      return;
    setOperationError(null);
    setOperationNotice(null);
    try {
      const result = await downloadMutation.mutateAsync({
        id: invoice.id,
        name: metadata.fileName,
      });
      if (typeof URL.createObjectURL !== "function") {
        setOperationError("当前运行环境不支持浏览器下载，请使用桌面应用重试。");
        return;
      }
      const url = URL.createObjectURL(result.blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = result.fileName;
      anchor.rel = "noopener";
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 0);
    } catch (error) {
      setOperationError(pdfErrorMessage(error, "本地 PDF 下载失败，请重试。"));
    }
  };

  const actions = canOperate ? (
    <div className="invoice-pdf-actions">
      <button
        className={`button ${
          !metadata || integrityUnavailable
            ? "button-primary"
            : "button-secondary"
        }`}
        disabled={operationBusy}
        onClick={() => void generatePdf()}
        type="button"
      >
        {generateMutation.isPending ? (
          <LoaderCircle className="animate-spin" size={13} />
        ) : (
          <RefreshCw size={13} />
        )}
        {generateMutation.isPending
          ? "正在生成…"
          : metadata
            ? "重新生成 PDF"
            : "生成 PDF"}
      </button>
      {metadata ? (
        <button
          className={`button ${
            integrityUnavailable ? "button-secondary" : "button-primary"
          }`}
          disabled={operationBusy || integrityUnavailable}
          onClick={() => void downloadPdf()}
          type="button"
        >
          {downloadMutation.isPending ? (
            <LoaderCircle className="animate-spin" size={13} />
          ) : (
            <Download size={13} />
          )}
          {downloadMutation.isPending ? "正在下载…" : "下载 PDF"}
        </button>
      ) : null}
    </div>
  ) : null;

  return (
    <section
      className="invoice-detail-panel invoice-pdf-panel"
      aria-labelledby="invoice-pdf-title"
    >
      <div className="invoice-detail-panel-heading">
        <div>
          <h2 id="invoice-pdf-title">本地 PDF</h2>
          <p>
            仅在本机生成和下载，不会发送或改变发票状态；这是业务账单，不代表税务电子发票。
          </p>
        </div>
        {actions}
      </div>

      <div className="invoice-pdf-content">
        {operationNotice ? (
          <div className="invoice-pdf-notice" role="status">
            {operationNotice}
          </div>
        ) : null}

        {operationError ? (
          <ErrorState compact message={operationError} title="PDF 操作失败" />
        ) : null}

        {query.isPending ? (
          <LoadingState label="正在检查本地 PDF…" />
        ) : query.isError && !metadataKnown ? (
          <ErrorState
            compact
            message={pdfErrorMessage(
              query.error,
              "无法读取本地 PDF 状态，请重试。",
            )}
            onRetry={() => void query.refetch()}
            title="PDF 状态不可用"
          />
        ) : (
          <>
            {query.isError ? (
              <ErrorState
                compact
                message="最新 PDF 状态读取失败，当前仍显示上一次已知内容。"
                onRetry={() => void query.refetch()}
                title="PDF 状态刷新失败"
              />
            ) : null}

            {metadata ? (
              <>
                <div className="invoice-pdf-file">
                  <span className="invoice-pdf-file-icon" aria-hidden="true">
                    <FileText size={17} />
                  </span>
                  <div>
                    <strong>{metadata.fileName}</strong>
                    <span>
                      {formatFileSize(metadata.sizeBytes)} · 生成于
                      {formatTimestamp(metadata.generatedAt)}
                    </span>
                  </div>
                  <span
                    className={
                      generatedFromOldVersion
                        ? "invoice-pdf-version invoice-pdf-version-stale"
                        : "invoice-pdf-version"
                    }
                  >
                    发票 v{metadata.generatedFromVersion}
                  </span>
                </div>
                {generatedFromOldVersion ? (
                  <div className="invoice-pdf-stale" role="status">
                    当前 PDF 基于旧版本，建议重新生成
                    {integrityUnavailable ? "。" : "；现有本地文件仍可下载。"}
                  </div>
                ) : null}
                {integrityUnavailable ? (
                  <div className="invoice-pdf-integrity-error" role="alert">
                    {metadata.integrityStatus === "missing"
                      ? "本地 PDF 文件已缺失，请重新生成后再下载。"
                      : "本地 PDF 完整性校验失败，文件可能已损坏，请重新生成后再下载。"}
                  </div>
                ) : null}
                <dl className="invoice-pdf-meta">
                  <div>
                    <dt>文件类型</dt>
                    <dd>{metadata.mimeType}</dd>
                  </div>
                  <div>
                    <dt>完整性</dt>
                    <dd>
                      {metadata.integrityStatus === "verified"
                        ? "校验通过"
                        : metadata.integrityStatus === "missing"
                          ? "文件缺失"
                          : "校验不匹配"}
                      <span className="invoice-pdf-checked-at">
                        {formatTimestamp(metadata.integrityCheckedAt)}
                      </span>
                    </dd>
                  </div>
                  <div>
                    <dt>SHA-256</dt>
                    <dd className="invoice-detail-code">{metadata.sha256}</dd>
                  </div>
                </dl>
              </>
            ) : (
              <div className="invoice-pdf-empty">
                <span className="invoice-pdf-file-icon" aria-hidden="true">
                  <FileText size={17} />
                </span>
                <div>
                  <strong>尚未生成本地 PDF</strong>
                  <span>可先生成草稿预览，确认后再由你手动发送。</span>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </section>
  );
}
