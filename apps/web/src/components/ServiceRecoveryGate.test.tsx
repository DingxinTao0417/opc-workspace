import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RuntimeDiagnostics } from "../api/desktop";
import { ServiceRecoveryGate } from "./ServiceRecoveryGate";

const ready: RuntimeDiagnostics = {
  environment: "desktop",
  phase: "ready",
  generation: 1,
  startupStage: null,
  nativeShortcuts: null,
  appVersion: "0.1.0",
  apiVersion: "v1",
  schemaVersion: "29",
};

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
}

function renderWithQueryClient(
  children: ReactNode,
  queryClient = createQueryClient(),
) {
  return {
    queryClient,
    ...render(
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>,
    ),
  };
}

function MountProbe({ onMount }: { onMount: () => void }) {
  useEffect(() => onMount(), [onMount]);
  return <p>业务页面</p>;
}

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("ServiceRecoveryGate", () => {
  it("leaves browser development outside the desktop gate", () => {
    const loadRuntime = vi.fn();
    const resetConnection = vi.fn();
    const queryClient = createQueryClient();
    queryClient.setQueryData(["browser-data"], "kept");
    const cancelQueries = vi.spyOn(queryClient, "cancelQueries");
    renderWithQueryClient(
      <ServiceRecoveryGate
        desktop={false}
        loadRuntime={loadRuntime}
        resetConnection={resetConnection}
      >
        <p>业务页面</p>
      </ServiceRecoveryGate>,
      queryClient,
    );

    expect(screen.getByText("业务页面")).toBeVisible();
    expect(loadRuntime).not.toHaveBeenCalled();
    expect(resetConnection).not.toHaveBeenCalled();
    expect(cancelQueries).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(["browser-data"])).toBe("kept");
  });

  it("keeps a ready external Sidecar and its cache untouched", async () => {
    vi.useFakeTimers();
    const loadRuntime = vi.fn(async (): Promise<RuntimeDiagnostics> => ({
      ...ready,
      generation: null,
    }));
    const resetConnection = vi.fn();
    const queryClient = createQueryClient();
    queryClient.setQueryData(["external-data"], "kept");
    const cancelQueries = vi.spyOn(queryClient, "cancelQueries");

    renderWithQueryClient(
      <ServiceRecoveryGate
        desktop
        loadRuntime={loadRuntime}
        resetConnection={resetConnection}
      >
        <p>业务页面</p>
      </ServiceRecoveryGate>,
      queryClient,
    );

    await act(async () => undefined);
    expect(screen.getByText("业务页面")).toBeVisible();
    await act(async () => vi.advanceTimersByTimeAsync(3_000));
    expect(resetConnection).not.toHaveBeenCalled();
    expect(cancelQueries).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(["external-data"])).toBe("kept");
  });

  it("holds business pages while starting and releases them when ready", async () => {
    vi.useFakeTimers();
    const loadRuntime = vi
      .fn<() => Promise<RuntimeDiagnostics>>()
      .mockResolvedValueOnce({ ...ready, phase: "starting", generation: 0 })
      .mockResolvedValueOnce(ready);
    renderWithQueryClient(
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

  it("shows the bounded restore stage without exposing raw startup details", async () => {
    const loadRuntime = vi
      .fn<() => Promise<RuntimeDiagnostics>>()
      .mockResolvedValue({
        ...ready,
        phase: "starting",
        generation: 1,
        startupStage: "verifying_restore_package",
      });
    renderWithQueryClient(
      <ServiceRecoveryGate
        desktop
        loadRuntime={loadRuntime}
        pollIntervalMs={50}
      >
        <div>业务页面</div>
      </ServiceRecoveryGate>,
    );

    expect(await screen.findByRole("status")).toHaveTextContent(
      "正在验证待恢复备份",
    );
    expect(screen.queryByText("业务页面")).toBeNull();
  });

  it("clears one stale query epoch across ready, restarting and ready", async () => {
    vi.useFakeTimers();
    const loadRuntime = vi
      .fn<() => Promise<RuntimeDiagnostics>>()
      .mockResolvedValueOnce(ready)
      .mockResolvedValueOnce({ ...ready, phase: "restarting" })
      .mockResolvedValueOnce({ ...ready, phase: "restarting" })
      .mockResolvedValueOnce({ ...ready, generation: 2 });
    const resetConnection = vi.fn();
    const mounted = vi.fn();
    const queryClient = createQueryClient();
    queryClient.setQueryData(["stale-runtime-query"], { generation: 1 });
    const cancelQueries = vi.spyOn(queryClient, "cancelQueries");

    renderWithQueryClient(
      <ServiceRecoveryGate
        desktop
        loadRuntime={loadRuntime}
        pollIntervalMs={50}
        resetConnection={resetConnection}
      >
        <MountProbe onMount={mounted} />
      </ServiceRecoveryGate>,
      queryClient,
    );

    await act(async () => undefined);
    expect(screen.getByText("业务页面")).toBeVisible();
    expect(mounted).toHaveBeenCalledTimes(1);
    expect(queryClient.getQueryData(["stale-runtime-query"])).toEqual({
      generation: 1,
    });

    await act(async () => vi.advanceTimersByTimeAsync(3_000));
    expect(screen.getByRole("status")).toHaveTextContent("正在恢复本地服务");
    expect(screen.queryByText("业务页面")).toBeNull();
    expect(queryClient.getQueryData(["stale-runtime-query"])).toBeUndefined();
    expect(cancelQueries).toHaveBeenCalledTimes(1);
    expect(resetConnection).toHaveBeenCalledTimes(1);

    await act(async () => vi.advanceTimersByTimeAsync(50));
    expect(screen.getByRole("status")).toHaveTextContent("正在恢复本地服务");
    expect(cancelQueries).toHaveBeenCalledTimes(1);
    expect(resetConnection).toHaveBeenCalledTimes(1);

    await act(async () => vi.advanceTimersByTimeAsync(50));
    expect(screen.getByText("业务页面")).toBeVisible();
    expect(mounted).toHaveBeenCalledTimes(2);
    expect(cancelQueries).toHaveBeenCalledTimes(1);
    expect(resetConnection).toHaveBeenCalledTimes(1);
  });

  it("reconnects and remounts when a ready generation changes", async () => {
    vi.useFakeTimers();
    const loadRuntime = vi
      .fn<() => Promise<RuntimeDiagnostics>>()
      .mockResolvedValueOnce(ready)
      .mockResolvedValueOnce({ ...ready, generation: 2 });
    const resetConnection = vi.fn();
    const mounted = vi.fn();
    const queryClient = createQueryClient();
    queryClient.setQueryData(["generation-one-query"], "stale");
    const cancelQueries = vi.spyOn(queryClient, "cancelQueries");

    renderWithQueryClient(
      <ServiceRecoveryGate
        desktop
        loadRuntime={loadRuntime}
        resetConnection={resetConnection}
      >
        <MountProbe onMount={mounted} />
      </ServiceRecoveryGate>,
      queryClient,
    );

    await act(async () => undefined);
    expect(mounted).toHaveBeenCalledTimes(1);

    await act(async () => vi.advanceTimersByTimeAsync(3_000));

    expect(screen.getByText("业务页面")).toBeVisible();
    expect(mounted).toHaveBeenCalledTimes(2);
    expect(queryClient.getQueryData(["generation-one-query"])).toBeUndefined();
    expect(cancelQueries).toHaveBeenCalledTimes(1);
    expect(resetConnection).toHaveBeenCalledTimes(1);
  });

  it("clears cached runtime data once when a ready service errors", async () => {
    vi.useFakeTimers();
    const loadRuntime = vi
      .fn<() => Promise<RuntimeDiagnostics>>()
      .mockResolvedValueOnce(ready)
      .mockResolvedValue({ ...ready, phase: "error" });
    const resetConnection = vi.fn();
    const queryClient = createQueryClient();
    queryClient.setQueryData(["ready-query"], "stale");
    const cancelQueries = vi.spyOn(queryClient, "cancelQueries");

    renderWithQueryClient(
      <ServiceRecoveryGate
        desktop
        loadRuntime={loadRuntime}
        resetConnection={resetConnection}
      >
        <p>业务页面</p>
      </ServiceRecoveryGate>,
      queryClient,
    );

    await act(async () => undefined);
    await act(async () => vi.advanceTimersByTimeAsync(3_000));
    expect(screen.getByRole("alert")).toHaveTextContent("本地服务未能正常启动");
    expect(queryClient.getQueryData(["ready-query"])).toBeUndefined();
    expect(cancelQueries).toHaveBeenCalledTimes(1);
    expect(resetConnection).toHaveBeenCalledTimes(1);

    await act(async () => vi.advanceTimersByTimeAsync(3_000));
    expect(cancelQueries).toHaveBeenCalledTimes(1);
    expect(resetConnection).toHaveBeenCalledTimes(1);
  });

  it("shows only safe recovery actions and never renders a raw failure", async () => {
    const loadRuntime = vi
      .fn<() => Promise<RuntimeDiagnostics>>()
      .mockRejectedValue(new Error("C:\\private\\workspace.db token-secret"));
    const openLogs = vi.fn(async () => true);
    const restart = vi.fn(async () => true);
    renderWithQueryClient(
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
    renderWithQueryClient(
      <ServiceRecoveryGate desktop loadRuntime={loadRuntime}>
        <p>业务页面</p>
      </ServiceRecoveryGate>,
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "应用 0.1.0 · API v1 · Schema 29",
    );
  });
});
