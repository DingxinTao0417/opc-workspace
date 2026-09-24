import { Check, Copy } from "lucide-react";
import { useEffect, useState } from "react";

/** Copies only the displayed reply, never hidden model control blocks. */
export function AiCopyButton({ text }: { text: string }) {
  const [status, setStatus] = useState<"idle" | "copied" | "failed">("idle");
  useEffect(() => {
    setStatus("idle");
  }, [text]);
  useEffect(() => {
    if (status !== "copied") return;
    const timer = setTimeout(() => setStatus("idle"), 2000);
    return () => clearTimeout(timer);
  }, [status]);

  return (
    <div className="ai-message-actions">
      <button
        aria-label={status === "copied" ? "已复制回复" : "复制回复"}
        className="icon-button"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(text);
            setStatus("copied");
          } catch {
            setStatus("failed");
          }
        }}
        title={status === "copied" ? "已复制" : "复制回复"}
        type="button"
      >
        {status === "copied" ? <Check size={15} /> : <Copy size={15} />}
      </button>
      <span aria-live="polite">
        {status === "failed"
          ? "复制失败，请选中文字后复制"
          : status === "copied"
            ? "已复制"
            : ""}
      </span>
    </div>
  );
}
