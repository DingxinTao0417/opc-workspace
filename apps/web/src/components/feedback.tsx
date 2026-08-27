import { AlertCircle, Inbox, LoaderCircle, RefreshCw } from "lucide-react";

interface ErrorStateProps {
  title?: string;
  message: string;
  onRetry?: () => void;
  compact?: boolean;
}

export function ErrorState({
  title = "本地服务暂不可用",
  message,
  onRetry,
  compact = false,
}: ErrorStateProps) {
  return (
    <div
      className={
        compact
          ? "feedback feedback-compact feedback-error"
          : "feedback feedback-error"
      }
    >
      <span className="feedback-icon" aria-hidden="true">
        <AlertCircle size={16} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="feedback-title">{title}</div>
        <div className="feedback-copy">{message}</div>
      </div>
      {onRetry ? (
        <button
          className="button button-quiet shrink-0"
          onClick={onRetry}
          type="button"
        >
          <RefreshCw size={14} />
          重试
        </button>
      ) : null}
    </div>
  );
}

export function LoadingState({
  label = "正在读取本地数据…",
}: {
  label?: string;
}) {
  return (
    <div className="feedback feedback-loading" role="status">
      <LoaderCircle className="animate-spin" size={16} />
      <span>{label}</span>
    </div>
  );
}

export function SkeletonRows({ count = 4 }: { count?: number }) {
  return (
    <div className="skeleton-stack" aria-label="正在加载" role="status">
      {Array.from({ length: count }).map((_, index) => (
        <div className="skeleton-row" key={index}>
          <span className="skeleton-dot" />
          <span
            className="skeleton-line"
            style={{ width: `${58 - index * 4}%` }}
          />
          <span className="skeleton-line skeleton-line-short" />
        </div>
      ))}
    </div>
  );
}

export function EmptyState({
  title,
  message,
  action,
}: {
  title: string;
  message: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="empty-state">
      <span className="empty-icon" aria-hidden="true">
        <Inbox size={20} />
      </span>
      <h2>{title}</h2>
      <p>{message}</p>
      {action}
    </div>
  );
}
