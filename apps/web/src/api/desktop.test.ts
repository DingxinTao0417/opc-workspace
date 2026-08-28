import { afterEach, describe, expect, it, vi } from "vitest";
import { isDesktopRuntime, requestApplicationRestart } from "./desktop";

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
