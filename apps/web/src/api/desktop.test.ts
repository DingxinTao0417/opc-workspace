import { afterEach, describe, expect, it, vi } from "vitest";
import {
  getRuntimeDiagnostics,
  isDesktopRuntime,
  openDesktopLogDirectory,
  requestApplicationRestart,
} from "./desktop";

afterEach(() => vi.unstubAllGlobals());

describe("desktop application restart", () => {
  it("keeps browser development mode manual", async () => {
    expect(isDesktopRuntime()).toBe(false);
    expect(await requestApplicationRestart()).toBe(false);
  });

  it("invokes only the dedicated desktop restart command", async () => {
    const invoke = vi.fn(async () => undefined);
    await expect(requestApplicationRestart(invoke)).resolves.toBe(true);
    expect(invoke).toHaveBeenCalledWith("restart_application");
  });
});

describe("desktop log directory", () => {
  it("does not invent a log location in browser development", async () => {
    await expect(openDesktopLogDirectory()).resolves.toBe(false);
  });

  it("invokes only the pathless desktop command", async () => {
    const invoke = vi.fn(async () => undefined);
    await expect(openDesktopLogDirectory(invoke)).resolves.toBe(true);
    expect(invoke).toHaveBeenCalledWith("open_log_directory");
  });
});

describe("runtime diagnostics", () => {
  it("identifies browser development without inventing desktop facts", async () => {
    await expect(getRuntimeDiagnostics()).resolves.toEqual({
      environment: "browser",
      phase: "external",
      appVersion: null,
      apiVersion: null,
      schemaVersion: null,
    });
  });

  it("returns only sanitized desktop lifecycle and version facts", async () => {
    const invoke = vi.fn(async () => ({
      phase: "ready",
      baseUrl: "http://127.0.0.1:49152",
      sessionToken: "must-not-leak",
      message: "C:\\private\\path",
      appVersion: "0.1.0",
      apiVersion: "v1",
      schemaVersion: "28",
    }));

    await expect(getRuntimeDiagnostics(invoke)).resolves.toEqual({
      environment: "desktop",
      phase: "ready",
      appVersion: "0.1.0",
      apiVersion: "v1",
      schemaVersion: "28",
    });
    expect(invoke).toHaveBeenCalledWith("sidecar_status");
  });

  it("rejects malformed desktop lifecycle data", async () => {
    await expect(
      getRuntimeDiagnostics(async () => ({ phase: "unknown" })),
    ).rejects.toThrow("Invalid desktop runtime phase");
  });
});
