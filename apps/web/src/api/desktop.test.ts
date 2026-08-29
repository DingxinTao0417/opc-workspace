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
      generation: null,
      startupStage: null,
      appVersion: null,
      apiVersion: null,
      schemaVersion: null,
    });
  });

  it("returns only sanitized desktop lifecycle and version facts", async () => {
    const invoke = vi.fn(async () => ({
      phase: "ready",
      generation: 7,
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
      generation: 7,
      startupStage: null,
      appVersion: "0.1.0",
      apiVersion: "v1",
      schemaVersion: "28",
    });
    expect(invoke).toHaveBeenCalledWith("sidecar_status");
  });

  it.each([null, 0, 8])(
    "accepts a restarting lifecycle with generation %s",
    async (generation) => {
      await expect(
        getRuntimeDiagnostics(async () => ({
          phase: "restarting",
          generation,
        })),
      ).resolves.toMatchObject({
        environment: "desktop",
        phase: "restarting",
        generation,
      });
    },
  );

  it.each([undefined, -1, 1.5, Number.NaN, "1"])(
    "rejects malformed desktop generation %s",
    async (generation) => {
      await expect(
        getRuntimeDiagnostics(async () => ({ phase: "ready", generation })),
      ).rejects.toThrow("Invalid desktop runtime generation");
    },
  );

  it("accepts only known bounded startup stages", async () => {
    await expect(
      getRuntimeDiagnostics(async () => ({
        phase: "starting",
        generation: 1,
        startupStage: "verifying_restore_package",
      })),
    ).resolves.toMatchObject({ startupStage: "verifying_restore_package" });
    await expect(
      getRuntimeDiagnostics(async () => ({
        phase: "starting",
        generation: 1,
        startupStage: "C:\\private\\workspace.db",
      })),
    ).rejects.toThrow("Invalid desktop startup stage");
  });

  it("rejects malformed desktop lifecycle data", async () => {
    await expect(
      getRuntimeDiagnostics(async () => ({ phase: "unknown", generation: 1 })),
    ).rejects.toThrow("Invalid desktop runtime phase");
  });
});
