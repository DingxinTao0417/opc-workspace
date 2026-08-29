type InvokeCommand = (command: string) => Promise<unknown>;

export interface RuntimeDiagnostics {
  environment: "browser" | "desktop";
  phase: "external" | "starting" | "restarting" | "ready" | "error";
  generation: number | null;
  appVersion: string | null;
  apiVersion: string | null;
  schemaVersion: string | null;
}

export function isDesktopRuntime(): boolean {
  if (typeof window === "undefined") return false;
  const runtimeWindow = window as Window & {
    __TAURI__?: unknown;
    __TAURI_INTERNALS__?: unknown;
  };
  return Boolean(runtimeWindow.__TAURI__ || runtimeWindow.__TAURI_INTERNALS__);
}

export async function requestApplicationRestart(
  invokeCommand?: InvokeCommand,
): Promise<boolean> {
  if (!invokeCommand && !isDesktopRuntime()) return false;
  const invoke =
    invokeCommand ?? (await import("@tauri-apps/api/core")).invoke<unknown>;
  await invoke("restart_application");
  return true;
}

export async function openDesktopLogDirectory(
  invokeCommand?: InvokeCommand,
): Promise<boolean> {
  if (!invokeCommand && !isDesktopRuntime()) return false;
  const invoke =
    invokeCommand ?? (await import("@tauri-apps/api/core")).invoke<unknown>;
  await invoke("open_log_directory");
  return true;
}

function optionalVersion(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function runtimeGeneration(value: unknown): number | null {
  if (value === null) return null;
  if (typeof value === "number" && Number.isSafeInteger(value) && value >= 0) {
    return value;
  }
  throw new Error("Invalid desktop runtime generation");
}

export async function getRuntimeDiagnostics(
  invokeCommand?: InvokeCommand,
): Promise<RuntimeDiagnostics> {
  if (!invokeCommand && !isDesktopRuntime()) {
    return {
      environment: "browser",
      phase: "external",
      generation: null,
      appVersion: null,
      apiVersion: null,
      schemaVersion: null,
    };
  }

  const invoke =
    invokeCommand ?? (await import("@tauri-apps/api/core")).invoke<unknown>;
  const value = await invoke("sidecar_status");
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("Invalid desktop runtime status");
  }
  const record = value as Record<string, unknown>;
  const phase = record.phase;
  if (
    phase !== "starting" &&
    phase !== "restarting" &&
    phase !== "ready" &&
    phase !== "error"
  ) {
    throw new Error("Invalid desktop runtime phase");
  }

  return {
    environment: "desktop",
    phase,
    generation: runtimeGeneration(record.generation),
    appVersion: optionalVersion(record.appVersion),
    apiVersion: optionalVersion(record.apiVersion),
    schemaVersion: optionalVersion(record.schemaVersion),
  };
}
