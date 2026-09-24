import { useEffect, useState } from "react";
import {
  workspaceApi,
  type WorkspaceFileWriteCapability,
} from "../api/workspace";

// Legacy v1 in-place recovery remains closed. New replacement writes use the
// runtime capability below and still require native review plus per-use UAC.
export const projectFileWriteGate = {
  enabled: false,
  reason:
    "旧版原地恢复保持关闭；版本 2 替换记录可在独立安全辅助程序可用时执行受控恢复。",
};

const unavailable: WorkspaceFileWriteCapability = {
  available: false,
  reason: "正在核验本机单文件安全辅助程序…",
};

export function useProjectFileWriteCapability() {
  const [capability, setCapability] =
    useState<WorkspaceFileWriteCapability>(unavailable);
  useEffect(() => {
    let active = true;
    void workspaceApi
      .fileWriteCapability()
      .then((value) => {
        if (
          !active ||
          typeof value.available !== "boolean" ||
          typeof value.reason !== "string" ||
          !value.reason
        )
          return;
        setCapability(value);
      })
      .catch(() => {
        if (active)
          setCapability({
            available: false,
            reason:
              "当前桌面构建未提供可核验的单文件安全辅助程序；仍可只读审查差异。",
          });
      });
    return () => {
      active = false;
    };
  }, []);
  return capability;
}
