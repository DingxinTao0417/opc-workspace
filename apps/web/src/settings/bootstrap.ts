import {
  ApiError,
  commitAppSettingsWithAvatar,
  getAppSetting,
  getAppSettings,
  getWorkspaceAvatarBlob,
  updateAppSettings,
} from "../api/client";
import type { AppSettingsResult, AppSettingUpdate } from "../types/models";
import {
  readLocalAvatarSnapshot,
  readLegacySettingsSnapshot,
  type LegacySettingsSnapshot,
  type SettingsPreview,
} from "../store/settings";

export interface SettingsBootstrapResult {
  settings: AppSettingsResult;
  committed: SettingsPreview;
  legacyExists: boolean;
  migratedKeys: AppSettingUpdate["key"][];
}

export function committedSettingsFromServer(
  settings: AppSettingsResult,
  avatarDataUrl: string | null,
): SettingsPreview {
  return {
    profile: {
      displayName: getAppSetting(settings, "workspace").value.displayName,
      avatarDataUrl,
    },
    general: getAppSetting(settings, "general").value,
    theme: getAppSetting(settings, "appearance").value.theme,
    focus: getAppSetting(settings, "focus").value,
  };
}

export async function workspaceAvatarUrlFromServer(
  settings: AppSettingsResult,
): Promise<string | null> {
  if (!getAppSetting(settings, "workspace").value.avatarRef) return null;
  try {
    const blob = await getWorkspaceAvatarBlob();
    if (typeof URL.createObjectURL === "function") {
      return URL.createObjectURL(blob);
    }
    return await new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.addEventListener("load", () =>
        typeof reader.result === "string"
          ? resolve(reader.result)
          : reject(new Error("头像读取失败")),
      );
      reader.addEventListener("error", () => reject(reader.error));
      reader.readAsDataURL(blob);
    });
  } catch (error) {
    if (
      error instanceof ApiError &&
      ["AVATAR_NOT_FOUND", "AVATAR_MISSING", "AVATAR_INVALID"].includes(
        error.code,
      )
    ) {
      return null;
    }
    throw error;
  }
}

function dataUrlToAvatarFile(dataUrl: string): File {
  const match =
    /^data:(image\/(?:png|jpeg|webp));base64,([A-Za-z0-9+/=]+)$/i.exec(dataUrl);
  if (!match) {
    throw new ApiError("旧头像格式无效，请重新选择头像", {
      code: "VALIDATION_ERROR",
      status: 422,
    });
  }
  const binary = atob(match[2]);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  const extension =
    match[1].toLowerCase() === "image/jpeg" ? "jpg" : match[1].split("/")[1];
  return new File([bytes], `workspace-avatar.${extension}`, {
    type: match[1].toLowerCase(),
  });
}

export function legacySettingUpdates(
  settings: AppSettingsResult,
  legacy: LegacySettingsSnapshot,
): AppSettingUpdate[] {
  if (!legacy.exists) return [];
  const updates: AppSettingUpdate[] = [];
  const workspace = getAppSetting(settings, "workspace");
  if (!workspace.stored) {
    updates.push({
      key: "workspace",
      expectedVersion: 0,
      value: {
        displayName: legacy.profile.displayName,
        avatarRef: workspace.value.avatarRef,
      },
    });
  }
  const general = getAppSetting(settings, "general");
  if (!general.stored) {
    updates.push({
      key: "general",
      expectedVersion: 0,
      value: legacy.general,
    });
  }
  const appearance = getAppSetting(settings, "appearance");
  if (!appearance.stored) {
    updates.push({
      key: "appearance",
      expectedVersion: 0,
      value: { theme: legacy.theme },
    });
  }
  const focus = getAppSetting(settings, "focus");
  if (!focus.stored) {
    updates.push({
      key: "focus",
      expectedVersion: 0,
      value: legacy.focus,
    });
  }
  return updates;
}

function canVerifyAmbiguousMigration(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.code === "SETTINGS_VERSION_CONFLICT" ||
      error.code === "NETWORK_ERROR" ||
      error.code === "TIMEOUT")
  );
}

export async function bootstrapAppSettings(): Promise<SettingsBootstrapResult> {
  const initial = await getAppSettings();
  const legacy = readLegacySettingsSnapshot();
  const localAvatar = readLocalAvatarSnapshot();
  const updates = legacySettingUpdates(initial, legacy);
  let settings = initial;
  const workspace = getAppSetting(initial, "workspace");
  const legacyAvatar = localAvatar.exists
    ? localAvatar.avatarDataUrl
    : legacy.profile.avatarDataUrl;
  const migrateAvatar =
    workspace.value.avatarRef === null && legacyAvatar !== null;
  if (migrateAvatar && !updates.some((update) => update.key === "workspace")) {
    updates.unshift({
      key: "workspace",
      expectedVersion: workspace.version,
      value: workspace.value,
    });
  }
  if (updates.length > 0 || migrateAvatar) {
    try {
      settings = migrateAvatar
        ? await commitAppSettingsWithAvatar(
            "replace",
            updates,
            dataUrlToAvatarFile(legacyAvatar),
          )
        : await updateAppSettings(updates);
    } catch (error) {
      if (!canVerifyAmbiguousMigration(error)) throw error;
      const latest = await getAppSettings();
      if (
        !updates.every((update) => getAppSetting(latest, update.key).stored) ||
        (migrateAvatar && !getAppSetting(latest, "workspace").value.avatarRef)
      ) {
        throw error;
      }
      settings = latest;
    }
  }
  const avatarUrl = await workspaceAvatarUrlFromServer(settings);
  return {
    settings,
    committed: committedSettingsFromServer(settings, avatarUrl),
    legacyExists: legacy.exists,
    migratedKeys: updates.map((update) => update.key),
  };
}
