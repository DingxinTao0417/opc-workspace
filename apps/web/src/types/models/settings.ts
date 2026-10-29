import type { FocusSettingValue } from "./focus";

export type AppSettingKey =
  "workspace" | "general" | "appearance" | "focus" | "storage";

export interface WorkspaceSettingValue {
  displayName: string;
  avatarRef: string | null;
}

export interface GeneralSettingValue {
  defaultRoute: "today" | "tasks" | "projects" | "clients" | "focus";
  showRightOverview: boolean;
  reduceMotion: boolean;
  closeToTray: boolean;
}

/** Interface zoom in percent; the desktop shell scales the main view. */
export type AppearanceFontScale = 90 | 100 | 110 | 125;

export interface AppearanceSettingValue {
  theme: "light" | "dark";
  fontScale: AppearanceFontScale;
}

export interface StorageSettingValue {
  lowSpaceThresholdGiB: number;
}

interface AppSettingItemBase {
  schemaVersion: 2;
  version: number;
  stored: boolean;
  updatedByActorId: string | null;
  updatedAt: string | null;
}

export type AppSettingItem = AppSettingItemBase &
  (
    | { key: "workspace"; value: WorkspaceSettingValue }
    | { key: "general"; value: GeneralSettingValue }
    | { key: "appearance"; value: AppearanceSettingValue }
    | { key: "focus"; value: FocusSettingValue }
    | { key: "storage"; value: StorageSettingValue }
  );

export interface AppSettingsResult {
  schemaVersion: 2;
  items: AppSettingItem[];
}

export type AppSettingUpdate =
  | {
      key: "workspace";
      expectedVersion: number;
      value: WorkspaceSettingValue;
    }
  | {
      key: "general";
      expectedVersion: number;
      value: GeneralSettingValue;
    }
  | {
      key: "appearance";
      expectedVersion: number;
      value: AppearanceSettingValue;
    }
  | {
      key: "focus";
      expectedVersion: number;
      value: FocusSettingValue;
    }
  | {
      key: "storage";
      expectedVersion: number;
      value: StorageSettingValue;
    };

export interface HealthResponse {
  status: string;
  app: {
    name: string;
    version: string;
    commit: string;
  };
  api: {
    version: string;
  };
  schema: {
    version: number;
  };
}
