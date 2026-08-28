import {
  ApiError,
  getAppSetting,
  getAppSettings,
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
  if (updates.length > 0) {
    try {
      settings = await updateAppSettings(updates);
    } catch (error) {
      if (!canVerifyAmbiguousMigration(error)) throw error;
      const latest = await getAppSettings();
      if (
        !updates.every((update) => getAppSetting(latest, update.key).stored)
      ) {
        throw error;
      }
      settings = latest;
    }
  }
  return {
    settings,
    committed: committedSettingsFromServer(
      settings,
      localAvatar.exists
        ? localAvatar.avatarDataUrl
        : legacy.profile.avatarDataUrl,
    ),
    legacyExists: legacy.exists,
    migratedKeys: updates.map((update) => update.key),
  };
}
