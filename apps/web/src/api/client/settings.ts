import type {
  AppearanceSettingValue,
  AppSettingItem,
  AppSettingKey,
  AppSettingsResult,
  AppSettingUpdate,
  FocusSettingValue,
  GeneralSettingValue,
  StorageSettingValue,
  WorkspaceSettingValue,
} from "../../types/models";
import {
  ARTIFACT_TRANSFER_TIMEOUT_MS,
  ApiError,
  apiFetch,
  apiRequest,
  hasExactKeys,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
} from "./core";

const MAX_WORKSPACE_AVATAR_BYTES = 2 * 1_024 * 1_024;

const appSettingKeys: AppSettingKey[] = [
  "workspace",
  "general",
  "appearance",
  "focus",
  "storage",
];

const controlledAvatarReference =
  /^avatars\/[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.(?:png|jpg|webp)$/;

function normalizeAppSettingValue(key: AppSettingKey, value: unknown) {
  if (!isRecord(value)) return invalidResponse(`设置 ${key} 响应格式无效`);
  switch (key) {
    case "workspace": {
      if (!hasExactKeys(value, ["display_name", "avatar_ref"])) {
        return invalidResponse("工作区设置响应字段无效");
      }
      const displayName = value.display_name;
      const avatarRef = value.avatar_ref;
      if (
        typeof displayName !== "string" ||
        Array.from(displayName).length < 1 ||
        Array.from(displayName).length > 32 ||
        (avatarRef !== null &&
          (typeof avatarRef !== "string" ||
            !controlledAvatarReference.test(avatarRef)))
      ) {
        return invalidResponse("工作区设置响应值无效");
      }
      return { displayName, avatarRef };
    }
    case "general": {
      if (
        !hasExactKeys(value, [
          "default_route",
          "show_right_overview",
          "reduce_motion",
          "close_to_tray",
        ]) ||
        (value.default_route !== "today" &&
          value.default_route !== "tasks" &&
          value.default_route !== "projects" &&
          value.default_route !== "clients" &&
          value.default_route !== "focus") ||
        typeof value.show_right_overview !== "boolean" ||
        typeof value.reduce_motion !== "boolean" ||
        typeof value.close_to_tray !== "boolean"
      ) {
        return invalidResponse("通用设置响应值无效");
      }
      return {
        defaultRoute: value.default_route,
        showRightOverview: value.show_right_overview,
        reduceMotion: value.reduce_motion,
        closeToTray: value.close_to_tray,
      };
    }
    case "appearance":
      if (
        !hasExactKeys(value, ["theme", "font_scale"]) ||
        (value.theme !== "light" && value.theme !== "dark") ||
        (value.font_scale !== 90 &&
          value.font_scale !== 100 &&
          value.font_scale !== 110 &&
          value.font_scale !== 125)
      ) {
        return invalidResponse("外观设置响应值无效");
      }
      return { theme: value.theme, fontScale: value.font_scale };
    case "focus": {
      if (
        !hasExactKeys(value, [
          "focus_minutes",
          "break_minutes",
          "cycles",
          "auto_start_break",
          "auto_start_focus",
          "sound_enabled",
        ]) ||
        typeof value.focus_minutes !== "number" ||
        !Number.isInteger(value.focus_minutes) ||
        value.focus_minutes < 5 ||
        value.focus_minutes > 120 ||
        value.focus_minutes % 5 !== 0 ||
        typeof value.break_minutes !== "number" ||
        !Number.isInteger(value.break_minutes) ||
        value.break_minutes < 5 ||
        value.break_minutes > 30 ||
        value.break_minutes % 5 !== 0 ||
        typeof value.cycles !== "number" ||
        !Number.isInteger(value.cycles) ||
        value.cycles < 1 ||
        value.cycles > 8 ||
        typeof value.auto_start_break !== "boolean" ||
        typeof value.auto_start_focus !== "boolean" ||
        typeof value.sound_enabled !== "boolean"
      ) {
        return invalidResponse("专注设置响应值无效");
      }
      return {
        focusMinutes: value.focus_minutes,
        breakMinutes: value.break_minutes,
        cycles: value.cycles,
        autoStartBreak: value.auto_start_break,
        autoStartFocus: value.auto_start_focus,
        soundEnabled: value.sound_enabled,
      };
    }
    case "storage": {
      if (
        !hasExactKeys(value, ["low_space_threshold_gib"]) ||
        typeof value.low_space_threshold_gib !== "number" ||
        !Number.isInteger(value.low_space_threshold_gib) ||
        value.low_space_threshold_gib < 1 ||
        value.low_space_threshold_gib > 100
      ) {
        return invalidResponse("存储设置响应值无效");
      }
      return { lowSpaceThresholdGiB: value.low_space_threshold_gib };
    }
  }
}

function normalizeAppSettingItem(
  value: unknown,
  expectedKey: AppSettingKey,
): AppSettingItem {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, [
      "key",
      "value",
      "schema_version",
      "version",
      "stored",
      "updated_by_actor_id",
      "updated_at",
    ]) ||
    value.key !== expectedKey ||
    value.schema_version !== 2 ||
    typeof value.stored !== "boolean"
  ) {
    return invalidResponse(`设置 ${expectedKey} 响应字段无效`);
  }
  const version = nonNegativeInteger(value.version, `设置 ${expectedKey} 版本`);
  const actorID = value.updated_by_actor_id;
  const updatedAt = value.updated_at;
  if (
    (value.stored &&
      (version < 1 ||
        typeof actorID !== "string" ||
        !actorID ||
        typeof updatedAt !== "string" ||
        !updatedAt ||
        Number.isNaN(Date.parse(updatedAt)))) ||
    (!value.stored && (version !== 0 || actorID !== null || updatedAt !== null))
  ) {
    return invalidResponse(`设置 ${expectedKey} 存储元数据无效`);
  }
  const base = {
    key: expectedKey,
    schemaVersion: 2 as const,
    version,
    stored: value.stored,
    updatedByActorId: value.stored ? (actorID as string) : null,
    updatedAt: value.stored ? (updatedAt as string) : null,
  };
  const normalizedValue = normalizeAppSettingValue(expectedKey, value.value);
  switch (expectedKey) {
    case "workspace":
      return {
        ...base,
        key: expectedKey,
        value: normalizedValue as WorkspaceSettingValue,
      };
    case "general":
      return {
        ...base,
        key: expectedKey,
        value: normalizedValue as GeneralSettingValue,
      };
    case "appearance":
      return {
        ...base,
        key: expectedKey,
        value: normalizedValue as AppearanceSettingValue,
      };
    case "focus":
      return {
        ...base,
        key: expectedKey,
        value: normalizedValue as FocusSettingValue,
      };
    case "storage":
      return {
        ...base,
        key: expectedKey,
        value: normalizedValue as StorageSettingValue,
      };
  }
}

export function normalizeAppSettingsResponse(
  value: unknown,
): AppSettingsResult {
  if (!isRecord(value) || !hasExactKeys(value, ["data"])) {
    return invalidResponse("设置响应格式无效");
  }
  const data = value.data;
  if (
    !isRecord(data) ||
    !hasExactKeys(data, ["schema_version", "items"]) ||
    data.schema_version !== 2 ||
    !Array.isArray(data.items) ||
    data.items.length !== appSettingKeys.length
  ) {
    return invalidResponse("设置响应格式无效");
  }
  const items = data.items;
  return {
    schemaVersion: 2,
    items: appSettingKeys.map((key, index) =>
      normalizeAppSettingItem(items[index], key),
    ),
  };
}

export function getAppSetting<K extends AppSettingKey>(
  settings: AppSettingsResult,
  key: K,
): Extract<AppSettingItem, { key: K }> {
  const item = settings.items.find(
    (candidate): candidate is Extract<AppSettingItem, { key: K }> =>
      candidate.key === key,
  );
  if (!item) return invalidResponse(`设置 ${key} 缺失`);
  return item;
}

export async function getAppSettings(): Promise<AppSettingsResult> {
  return normalizeAppSettingsResponse(
    await apiRequest<unknown>("/api/v1/settings"),
  );
}

function serializeAppSettingUpdate(update: AppSettingUpdate) {
  const base = { key: update.key, expected_version: update.expectedVersion };
  switch (update.key) {
    case "workspace":
      return {
        ...base,
        value: {
          display_name: update.value.displayName,
          avatar_ref: update.value.avatarRef,
        },
      };
    case "general":
      return {
        ...base,
        value: {
          default_route: update.value.defaultRoute,
          show_right_overview: update.value.showRightOverview,
          reduce_motion: update.value.reduceMotion,
          close_to_tray: update.value.closeToTray,
        },
      };
    case "appearance":
      return {
        ...base,
        value: {
          theme: update.value.theme,
          font_scale: update.value.fontScale,
        },
      };
    case "focus":
      return {
        ...base,
        value: {
          focus_minutes: update.value.focusMinutes,
          break_minutes: update.value.breakMinutes,
          cycles: update.value.cycles,
          auto_start_break: update.value.autoStartBreak,
          auto_start_focus: update.value.autoStartFocus,
          sound_enabled: update.value.soundEnabled,
        },
      };
    case "storage":
      return {
        ...base,
        value: {
          low_space_threshold_gib: update.value.lowSpaceThresholdGiB,
        },
      };
  }
}

export async function updateAppSettings(
  updates: AppSettingUpdate[],
): Promise<AppSettingsResult> {
  return normalizeAppSettingsResponse(
    await apiRequest<unknown>("/api/v1/settings", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        updates: updates.map(serializeAppSettingUpdate),
      }),
    }),
  );
}

export async function commitAppSettingsWithAvatar(
  operation: "replace" | "remove",
  updates: AppSettingUpdate[],
  file?: File,
): Promise<AppSettingsResult> {
  if (!updates.some((update) => update.key === "workspace")) {
    throw new ApiError("头像变更必须同时提交工作区设置", {
      code: "VALIDATION_ERROR",
      status: 422,
    });
  }
  if (operation === "replace") {
    if (!file || file.size < 1) {
      throw new ApiError("请选择头像图片", {
        code: "VALIDATION_ERROR",
        status: 422,
      });
    }
    if (file.size > MAX_WORKSPACE_AVATAR_BYTES) {
      throw new ApiError("头像图片不能超过 2 MB", {
        code: "AVATAR_FILE_TOO_LARGE",
        status: 413,
      });
    }
    if (!["image/png", "image/jpeg", "image/webp"].includes(file.type)) {
      throw new ApiError("请选择 PNG、JPG 或 WebP 图片", {
        code: "VALIDATION_ERROR",
        status: 422,
      });
    }
  } else if (file) {
    throw new ApiError("移除头像时不能附带文件", {
      code: "VALIDATION_ERROR",
      status: 422,
    });
  }
  const form = new FormData();
  form.append(
    "manifest",
    JSON.stringify({
      operation,
      updates: updates.map(serializeAppSettingUpdate),
    }),
  );
  if (file) form.append("file", file, file.name || "workspace-avatar");
  return normalizeAppSettingsResponse(
    await apiRequest<unknown>(
      "/api/v1/settings/avatar",
      { method: "POST", body: form },
      ARTIFACT_TRANSFER_TIMEOUT_MS,
    ),
  );
}

export async function getWorkspaceAvatarBlob(): Promise<Blob> {
  return apiFetch(
    "/api/v1/settings/avatar/content",
    async (response) => {
      const contentType = response.headers.get("Content-Type")?.split(";")[0];
      if (
        !contentType ||
        !["image/png", "image/jpeg", "image/webp"].includes(contentType)
      ) {
        return invalidResponse("头像响应格式无效");
      }
      const blob = await response.blob();
      if (blob.size < 1 || blob.size > MAX_WORKSPACE_AVATAR_BYTES) {
        return invalidResponse("头像响应大小无效");
      }
      return blob;
    },
    undefined,
    "image/png,image/jpeg,image/webp",
    ARTIFACT_TRANSFER_TIMEOUT_MS,
  );
}
