import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RuntimeDiagnostics } from "../api/desktop";
import { ServiceRecoveryGate } from "./ServiceRecoveryGate";

const ready: RuntimeDiagnostics = {
  environment: "desktop",
  phase: "ready",
  appVersion: "0.1.0",
  apiVersion: "v1",
  schemaVersion: "29",
};

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("ServiceRecoveryGate", () => {
  it("leaves browser development outside the desktop gate", () => {
    const loadRuntime = vi.fn();
    render(
      <ServiceRecoveryGate desktop={false} loadRuntime={loadRuntime}>
        <p>业务页面</p>
      </ServiceRecoveryGate>,
    );

    expect(screen.getByText("业务页面")).toBeVisible();
    expect(loadRuntime).not.toHaveBeenCalled();
  });

  it("holds business pages while starting and releases them when ready", async () => {
    vi.useFakeTimers();
    const loadRuntime = vi
      .fn<() => Promise<RuntimeDiagnostics>>()
      .mockResolvedValueOnce({ ...ready, phase: "starting" })
      .mockResolvedValueOnce(ready);
    render(
      <ServiceRecoveryGate
        desktop
        loadRuntime={loadRuntime}
        pollIntervalMs={50}
      >
        <p>业务页面</p>
      </ServiceRecoveryGate>,
    );

    await act(async () => undefined);
    expect(screen.getByRole("status")).toHaveTextContent("正在启动本地服务");
    expect(screen.queryByText("业务页面")).toBeNull();
    await act(async () => vi.advanceTimersByTimeAsync(50));
    expect(screen.getByText("业务页面")).toBeVisible();
  });

  it("shows only safe recovery actions and never renders a raw failure", async () => {
    const loadRuntime = vi
      .fn<() => Promise<RuntimeDiagnostics>>()
      .mockRejectedValue(new Error("C:\\private\\workspace.db token-secret"));
    const openLogs = vi.fn(async () => true);
    const restart = vi.fn(async () => true);
    render(
      <ServiceRecoveryGate
        desktop
        loadRuntime={loadRuntime}
        openLogs={openLogs}
        restart={restart}
      >
        <p>业务页面</p>
      </ServiceRecoveryGate>,
    );

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("本地服务未能正常启动");
    expect(alert).not.toHaveTextContent("workspace.db");
    expect(alert).not.toHaveTextContent("token-secret");
    expect(screen.queryByText("业务页面")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "打开日志目录" }));
    expect(openLogs).toHaveBeenCalledTimes(1);
    await act(async () => undefined);
    fireEvent.click(screen.getByRole("button", { name: "重启并重试" }));
    expect(restart).toHaveBeenCalledTimes(1);
  });

  it("shows only sanitized version facts from an error status", async () => {
    const loadRuntime = vi
      .fn<() => Promise<RuntimeDiagnostics>>()
      .mockResolvedValue({ ...ready, phase: "error" });
    render(
      <ServiceRecoveryGate desktop loadRuntime={loadRuntime}>
        <p>业务页面</p>
      </ServiceRecoveryGate>,
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "应用 0.1.0 · API v1 · Schema 29",
    );
  });
});
