import type { WorkspaceFileRecoveryPreview } from "../api/workspace";
export function recoveryBytes(value: string): Uint8Array {
  if (value.length > 349528) throw new Error("恢复内容超过完整预览上限");
  const raw = atob(value);
  if (btoa(raw) !== value || raw.length > 256 * 1024)
    throw new Error("恢复内容编码无效");
  return Uint8Array.from(raw, (c) => c.charCodeAt(0));
}
function hex(bytes: Uint8Array) {
  const rows: string[] = [];
  for (let i = 0; i < bytes.length; i += 16)
    rows.push(
      `${i.toString(16).padStart(8, "0")}: ${Array.from(bytes.subarray(i, i + 16), (v) => v.toString(16).padStart(2, "0")).join(" ")}\n`,
    );
  return rows.join("");
}
export function recoveryDiff(
  preview: Pick<
    WorkspaceFileRecoveryPreview,
    "currentBase64" | "originalBase64"
  >,
) {
  const current = recoveryBytes(preview.currentBase64);
  const original = recoveryBytes(preview.originalBase64);
  try {
    if (current.includes(0) || original.includes(0)) throw new Error("binary");
    const decoder = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true });
    return {
      before: decoder.decode(current),
      after: decoder.decode(original),
      binary: false,
    };
  } catch {
    return { before: hex(current), after: hex(original), binary: true };
  }
}
