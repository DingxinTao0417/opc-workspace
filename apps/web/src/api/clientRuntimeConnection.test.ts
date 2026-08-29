import { afterEach, describe, expect, it, vi } from "vitest";
import { getHealth, resetRuntimeConnection } from "./client";

const tauri = vi.hoisted(() => ({
  invoke: vi.fn(),
}));

vi.mock("@tauri-apps/api/core", () => ({
  invoke: tauri.invoke,
}));

afterEach(() => {
  Reflect.deleteProperty(window, "__TAURI_INTERNALS__");
  tauri.invoke.mockReset();
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("desktop runtime connection", () => {
  it("waits through restarting and uses the next ready connection", async () => {
    Object.defineProperty(window, "__TAURI_INTERNALS__", {
      configurable: true,
      value: {},
    });
    tauri.invoke
      .mockResolvedValueOnce({ phase: "restarting", generation: 2 })
      .mockResolvedValueOnce({
        phase: "ready",
        generation: 2,
        baseUrl: "http://127.0.0.1:49152",
        sessionToken: "new-session-token",
      });
    const fetchMock = vi.fn(
      async (_url: string | URL | Request, _init?: RequestInit) =>
        new Response(
          JSON.stringify({
            status: "ok",
            app: { name: "opc-workspace", version: "0.1.0", commit: "abc123" },
            api: { version: "v1" },
            schema: { version: 29 },
          }),
          { headers: { "Content-Type": "application/json" } },
        ),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(getHealth()).resolves.toMatchObject({ status: "ok" });

    expect(tauri.invoke).toHaveBeenNthCalledWith(1, "sidecar_status");
    expect(tauri.invoke).toHaveBeenNthCalledWith(2, "sidecar_status");
    expect(fetchMock).toHaveBeenCalledWith(
      "http://127.0.0.1:49152/health",
      expect.objectContaining({
        headers: expect.any(Headers),
      }),
    );
    const headers = fetchMock.mock.calls[0]?.[1]?.headers;
    expect(new Headers(headers).get("Authorization")).toBe(
      "Bearer new-session-token",
    );
  });
});
