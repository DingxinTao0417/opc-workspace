import {
  Activity,
  AlertCircle,
  Bot,
  CheckCircle2,
  Copy,
  DatabaseBackup,
  Download,
  Focus,
  FolderOpen,
  ImagePlus,
  Info,
  LoaderCircle,
  Minus,
  Moon,
  Palette,
  Plus,
  RefreshCw,
  RotateCcw,
  Settings2,
  Sun,
  Trash2,
  UserRound,
  UsersRound,
  Zap,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useEffect, useRef, useState, type ChangeEvent } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { ApiError, getAppSetting } from "../api/client";
import {
  getRuntimeDiagnostics,
  openDesktopLogDirectory,
  setCloseToTrayEnabled,
  type RuntimeDiagnostics,
} from "../api/desktop";
import {
  useAppSettingsQuery,
  useCommitAppSettingsWithAvatar,
  useHealthQuery,
  useDownloadDiagnosticPackage,
  useStorageCapacityHistoryQuery,
  useStorageCapacityQuery,
  useUpdateAppSettings,
} from "../api/hooks";
import {
  committedSettingsFromServer,
  workspaceAvatarUrlFromServer,
} from "../settings/bootstrap";
import {
  DEFAULT_FOCUS_SETTINGS,
  DEFAULT_GENERAL_SETTINGS,
  DEFAULT_PROFILE_SETTINGS,
  DEFAULT_THEME,
  getAppearanceTheme,
  getFocusSettings,
  getGeneralSettings,
  getProfileSettings,
  sanitizeFocusSettings,
  sanitizeGeneralSettings,
  sanitizeProfileSettings,
  type AppearanceTheme,
  type DefaultRoute,
  type FocusSettings,
  type GeneralSettings,
  type ProfileSettings,
  useSettingsStore,
} from "../store/settings";
import { useUiStore, type SettingsModule } from "../store/ui";
import type {
  AppSettingUpdate,
  StorageCapacityHistoryPoint,
  StorageCapacityScope,
} from "../types/models";
import { ActorSettings } from "./ActorSettings";
import { AgentAdapterSettings } from "./AgentAdapterSettings";
import { AutomationSettings } from "./AutomationSettings";
import { BackupSettings } from "./BackupSettings";
import { Modal } from "./Modal";

interface SettingsModalProps {
  onSettingsSaved?: (next: FocusSettings, previous: FocusSettings) => void;
}

const modules: { id: SettingsModule; label: string; icon: LucideIcon }[] = [
  { id: "profile", label: "个人资料", icon: UserRound },
  { id: "general", label: "通用", icon: Settings2 },
  { id: "appearance", label: "外观", icon: Palette },
  { id: "focus", label: "专注", icon: Focus },
  { id: "actors", label: "人员与责任", icon: UsersRound },
  { id: "agent", label: "本地 Agent", icon: Bot },
  { id: "automation", label: "自动化", icon: Zap },
  { id: "data", label: "数据与备份", icon: DatabaseBackup },
  { id: "diagnostics", label: "运行诊断", icon: Activity },
  { id: "about", label: "关于", icon: Info },
];

const MAX_AVATAR_FILE_BYTES = 2 * 1024 * 1024;
const DEFAULT_LOW_SPACE_THRESHOLD_GIB = 1;
const supportedAvatarTypes = new Set(["image/jpeg", "image/png", "image/webp"]);

function readFileAsDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.addEventListener("load", () => {
      if (typeof reader.result === "string") {
        resolve(reader.result);
      } else {
        reject(new Error("无法读取头像文件"));
      }
    });
    reader.addEventListener("error", () => reject(reader.error));
    reader.readAsDataURL(file);
  });
}

function formatHealthError(error: unknown): string {
  if (error instanceof ApiError) {
    return `${error.message}${error.requestId ? ` · 请求 ${error.requestId}` : ""}`;
  }
  return "无法连接本地服务，请确认 Sidecar 已启动后重试。";
}

function formatSettingsSaveError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === "SETTINGS_VERSION_CONFLICT") {
      return "设置已在另一个窗口改变，已请求最新版本。请确认当前预览后再次保存。";
    }
    return `${error.message}${error.requestId ? ` · 请求 ${error.requestId}` : ""}`;
  }
  return "设置保存失败，请保留当前草稿并重试。";
}

function sameValue(left: unknown, right: unknown): boolean {
  return JSON.stringify(left) === JSON.stringify(right);
}

function displayVersion(version: string): string {
  return version.startsWith("v") ? version : `v${version}`;
}

function displayCommit(commit: string): string {
  if (commit === "unknown") return "开发构建（未注入）";
  const dirty = commit.endsWith("-dirty");
  const revision = dirty ? commit.slice(0, -"-dirty".length) : commit;
  return `${revision.slice(0, 12)}${dirty ? "-dirty" : ""}`;
}

function formatStorageBytes(value: number): string {
  const gib = value / (1024 * 1024 * 1024);
  if (gib >= 1) return `${gib.toFixed(gib >= 10 ? 0 : 1)} GiB`;
  return `${(value / (1024 * 1024)).toFixed(0)} MiB`;
}

const storageLocationLabels = {
  database: "本地数据库",
  artifacts: "受控文件",
  backups: "本地备份",
} as const;

function storageScopeLabel(scope: StorageCapacityScope): string {
  return scope
    .split("+")
    .map(
      (kind) =>
        storageLocationLabels[kind as keyof typeof storageLocationLabels],
    )
    .join(" + ");
}

function storageTrendLine(points: StorageCapacityHistoryPoint[]): string {
  if (points.length === 0) return "";
  return points
    .map((point, index) => {
      const x = points.length === 1 ? 50 : (index / (points.length - 1)) * 100;
      const ratio = Math.max(
        0,
        Math.min(1, point.availableBytes / point.totalBytes),
      );
      return `${x.toFixed(2)},${(36 - ratio * 32).toFixed(2)}`;
    })
    .join(" ");
}

function groupStorageCapacityPoints(points: StorageCapacityHistoryPoint[]) {
  const groups = new Map<StorageCapacityScope, StorageCapacityHistoryPoint[]>();
  for (const point of points) {
    const group = groups.get(point.scope) ?? [];
    group.push(point);
    groups.set(point.scope, group);
  }
  return [...groups.entries()];
}

const defaultRouteOptions: { value: DefaultRoute; label: string }[] = [
  { value: "today", label: "今日工作台" },
  { value: "tasks", label: "任务" },
  { value: "projects", label: "项目" },
  { value: "clients", label: "客户" },
  { value: "focus", label: "专注" },
];

interface StepperProps {
  label: string;
  description: string;
  value: number;
  unit: string;
  min: number;
  max: number;
  step?: number;
  onChange: (value: number) => void;
}

function Stepper({
  label,
  description,
  value,
  unit,
  min,
  max,
  step = 1,
  onChange,
}: StepperProps) {
  return (
    <div className="settings-stepper">
      <div>
        <div className="settings-label">{label}</div>
        <div className="settings-description">{description}</div>
      </div>
      <div className="settings-stepper-control">
        <button
          aria-label={`减少${label}`}
          className="settings-stepper-button"
          disabled={value <= min}
          onClick={() => onChange(Math.max(min, value - step))}
          type="button"
        >
          <Minus size={14} />
        </button>
        <output
          aria-label={`${label}当前值`}
          className="settings-stepper-value"
        >
          {value}
        </output>
        <span className="settings-stepper-unit">{unit}</span>
        <button
          aria-label={`增加${label}`}
          className="settings-stepper-button"
          disabled={value >= max}
          onClick={() => onChange(Math.min(max, value + step))}
          type="button"
        >
          <Plus size={14} />
        </button>
      </div>
    </div>
  );
}

function Toggle({
  label,
  description,
  checked,
  onChange,
}: {
  label: string;
  description?: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <div className="settings-toggle-row">
      <div>
        <div className="settings-label settings-toggle-label">{label}</div>
        {description ? (
          <div className="settings-description">{description}</div>
        ) : null}
      </div>
      <button
        aria-checked={checked}
        aria-label={label}
        className="settings-toggle"
        data-checked={checked}
        onClick={() => onChange(!checked)}
        role="switch"
        type="button"
      >
        <span />
      </button>
    </div>
  );
}

export function SettingsModal({ onSettingsSaved }: SettingsModalProps) {
  const location = useLocation();
  const navigate = useNavigate();
  const open = useUiStore((state) => state.settingsOpen);
  const requestedModule = useUiStore((state) => state.settingsModule);
  const setOpen = useUiStore((state) => state.setSettingsOpen);
  const beginPreview = useSettingsStore((state) => state.beginPreview);
  const setPreview = useSettingsStore((state) => state.setPreview);
  const replaceCommitted = useSettingsStore((state) => state.replaceCommitted);
  const cancelPreview = useSettingsStore((state) => state.cancelPreview);
  const initialLocation = useRef("/today");
  const closeToTrayPreviewQueue = useRef<Promise<void>>(Promise.resolve());
  const activeModuleButtonRef = useRef<HTMLButtonElement | null>(null);
  const [activeModule, setActiveModule] = useState<SettingsModule>("general");
  const [focusDraft, setFocusDraft] = useState<FocusSettings>(
    DEFAULT_FOCUS_SETTINGS,
  );
  const [generalDraft, setGeneralDraft] = useState<GeneralSettings>(
    DEFAULT_GENERAL_SETTINGS,
  );
  const [profileDraft, setProfileDraft] = useState<ProfileSettings>(
    DEFAULT_PROFILE_SETTINGS,
  );
  const [themeDraft, setThemeDraft] = useState<AppearanceTheme>(DEFAULT_THEME);
  const [storageThresholdDraft, setStorageThresholdDraft] = useState(
    DEFAULT_LOW_SPACE_THRESHOLD_GIB,
  );
  const storageDraftInitialized = useRef(false);
  const [avatarError, setAvatarError] = useState<string | null>(null);
  const [avatarFile, setAvatarFile] = useState<File | null>(null);
  const [avatarOperation, setAvatarOperation] = useState<
    "unchanged" | "replace" | "remove"
  >("unchanged");
  const avatarPreviewUrl = useRef<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [runtimeDiagnostics, setRuntimeDiagnostics] =
    useState<RuntimeDiagnostics | null>(null);
  const [runtimeDiagnosticsError, setRuntimeDiagnosticsError] = useState(false);
  const [runtimeDiagnosticsPending, setRuntimeDiagnosticsPending] =
    useState(false);
  const [runtimeDiagnosticsSequence, setRuntimeDiagnosticsSequence] =
    useState(0);
  const [diagnosticCopyState, setDiagnosticCopyState] = useState<
    "idle" | "copied" | "error"
  >("idle");
  const [diagnosticPackageFeedback, setDiagnosticPackageFeedback] = useState<
    string | null
  >(null);
  const settingsQuery = useAppSettingsQuery(open);
  const settingsMutation = useUpdateAppSettings();
  const avatarMutation = useCommitAppSettingsWithAvatar();
  const saving = settingsMutation.isPending || avatarMutation.isPending;
  const healthQuery = useHealthQuery(
    open && (activeModule === "about" || activeModule === "diagnostics"),
  );
  const diagnosticPackageMutation = useDownloadDiagnosticPackage();
  const storageCapacityQuery = useStorageCapacityQuery(
    open && activeModule === "data",
  );
  const storageCapacityHistoryQuery = useStorageCapacityHistoryQuery(
    open && activeModule === "data" && storageCapacityQuery.isSuccess,
  );

  useEffect(() => {
    if (!open || activeModule !== "diagnostics") return;
    let cancelled = false;
    setRuntimeDiagnosticsPending(true);
    setRuntimeDiagnosticsError(false);
    setDiagnosticCopyState("idle");
    setDiagnosticPackageFeedback(null);
    void getRuntimeDiagnostics()
      .then((result) => {
        if (!cancelled) setRuntimeDiagnostics(result);
      })
      .catch(() => {
        if (!cancelled) {
          setRuntimeDiagnostics(null);
          setRuntimeDiagnosticsError(true);
        }
      })
      .finally(() => {
        if (!cancelled) setRuntimeDiagnosticsPending(false);
      });
    return () => {
      cancelled = true;
    };
  }, [activeModule, open, runtimeDiagnosticsSequence]);

  useEffect(() => {
    if (!open) return;
    const button = activeModuleButtonRef.current;
    if (!button) return;
    const keepActiveModuleVisible = () =>
      button.scrollIntoView?.({ block: "nearest", inline: "nearest" });
    keepActiveModuleVisible();
    const navigation = button.parentElement;
    if (!navigation || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(keepActiveModuleVisible);
    observer.observe(navigation);
    return () => observer.disconnect();
  }, [activeModule, open]);

  useEffect(() => {
    if (!open) return;

    const currentTheme = getAppearanceTheme();
    initialLocation.current = `${location.pathname}${location.search}${location.hash}`;
    setActiveModule(requestedModule);
    setFocusDraft(getFocusSettings());
    setGeneralDraft(getGeneralSettings());
    setProfileDraft(getProfileSettings());
    setThemeDraft(currentTheme);
    setAvatarError(null);
    setAvatarFile(null);
    setAvatarOperation("unchanged");
    setSaveError(null);
    settingsMutation.reset();
    avatarMutation.reset();
    beginPreview();

    return () => {
      if (
        avatarPreviewUrl.current &&
        typeof URL.revokeObjectURL === "function"
      ) {
        URL.revokeObjectURL(avatarPreviewUrl.current);
      }
      avatarPreviewUrl.current = null;
      cancelPreview();
    };
  }, [beginPreview, cancelPreview, open, requestedModule]);

  useEffect(() => {
    if (!open) {
      storageDraftInitialized.current = false;
      return;
    }
    if (storageDraftInitialized.current || !settingsQuery.data) return;
    setStorageThresholdDraft(
      getAppSetting(settingsQuery.data, "storage").value.lowSpaceThresholdGiB,
    );
    storageDraftInitialized.current = true;
  }, [open, settingsQuery.data]);

  const previewSettings = (
    focus: FocusSettings,
    general: GeneralSettings,
    profile: ProfileSettings,
    theme: AppearanceTheme,
  ) => setPreview({ focus, general, profile, theme });

  const queueCloseToTrayPreview = (enabled: boolean) => {
    closeToTrayPreviewQueue.current = closeToTrayPreviewQueue.current
      .catch(() => undefined)
      .then(async () => {
        await setCloseToTrayEnabled(enabled);
      })
      .catch(() => undefined);
  };

  const updateFocusDraft = <Key extends keyof FocusSettings>(
    key: Key,
    value: FocusSettings[Key],
  ) => {
    const nextFocus = { ...focusDraft, [key]: value };
    setFocusDraft(nextFocus);
    previewSettings(nextFocus, generalDraft, profileDraft, themeDraft);
  };

  const updateGeneralDraft = <Key extends keyof GeneralSettings>(
    key: Key,
    value: GeneralSettings[Key],
  ) => {
    const nextGeneral = { ...generalDraft, [key]: value };
    setGeneralDraft(nextGeneral);
    previewSettings(focusDraft, nextGeneral, profileDraft, themeDraft);

    if (key === "defaultRoute") {
      navigate(`/${String(value)}`);
    }
    if (key === "closeToTray") {
      queueCloseToTrayPreview(Boolean(value));
    }
  };

  const updateProfileDraft = <Key extends keyof ProfileSettings>(
    key: Key,
    value: ProfileSettings[Key],
  ) => {
    const nextProfile = { ...profileDraft, [key]: value };
    setProfileDraft(nextProfile);
    previewSettings(focusDraft, generalDraft, nextProfile, themeDraft);
  };

  const refreshDiagnostics = () => {
    setRuntimeDiagnosticsSequence((value) => value + 1);
    void healthQuery.refetch();
  };

  const copyDiagnosticSummary = async () => {
    if (!healthQuery.data || !runtimeDiagnostics) return;
    const health = healthQuery.data;
    const summary = [
      "opc-workspace 运行诊断",
      `environment=${runtimeDiagnostics.environment}`,
      `sidecar_phase=${runtimeDiagnostics.phase}`,
      `tray=${runtimeDiagnostics.desktopCapabilities?.tray ?? "unknown"}`,
      `app_version=${health.app.version}`,
      `app_commit=${health.app.commit}`,
      `api_version=${health.api.version}`,
      `schema_version=${health.schema.version}`,
      `health=${health.status}`,
    ].join("\n");
    try {
      if (!navigator.clipboard?.writeText)
        throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(summary);
      setDiagnosticCopyState("copied");
    } catch {
      setDiagnosticCopyState("error");
    }
  };

  const downloadDiagnostics = () => {
    setDiagnosticPackageFeedback(null);
    diagnosticPackageMutation.reset();
    diagnosticPackageMutation.mutate(undefined, {
      onSuccess: (result) => {
        if (typeof URL.createObjectURL !== "function") {
          setDiagnosticPackageFeedback(
            "当前运行环境不支持保存诊断包，请在桌面应用中重试。",
          );
          return;
        }
        const url = URL.createObjectURL(result.blob);
        const anchor = document.createElement("a");
        anchor.href = url;
        anchor.download = result.fileName;
        anchor.rel = "noopener";
        document.body.appendChild(anchor);
        anchor.click();
        anchor.remove();
        window.setTimeout(() => URL.revokeObjectURL(url), 0);
        setDiagnosticPackageFeedback(`诊断包已生成：${result.fileName}`);
      },
      onError: () => {
        setDiagnosticPackageFeedback("诊断包生成失败，请保留当前页面并重试。");
      },
    });
  };

  const openLogDirectory = async () => {
    setDiagnosticPackageFeedback(null);
    try {
      const opened = await openDesktopLogDirectory();
      setDiagnosticPackageFeedback(
        opened ? "已打开应用日志目录" : "浏览器开发模式不能打开桌面日志目录。",
      );
    } catch {
      setDiagnosticPackageFeedback("无法打开应用日志目录，请稍后重试。");
    }
  };

  const changeAvatar = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;

    if (!supportedAvatarTypes.has(file.type)) {
      setAvatarError("请选择 PNG、JPG 或 WebP 图片。");
      return;
    }
    if (file.size > MAX_AVATAR_FILE_BYTES) {
      setAvatarError("头像图片不能超过 2 MB。");
      return;
    }

    try {
      if (
        avatarPreviewUrl.current &&
        typeof URL.revokeObjectURL === "function"
      ) {
        URL.revokeObjectURL(avatarPreviewUrl.current);
      }
      const avatarDataUrl =
        typeof URL.createObjectURL === "function"
          ? URL.createObjectURL(file)
          : await readFileAsDataUrl(file);
      avatarPreviewUrl.current = avatarDataUrl.startsWith("blob:")
        ? avatarDataUrl
        : null;
      setAvatarError(null);
      setAvatarFile(file);
      setAvatarOperation("replace");
      updateProfileDraft("avatarDataUrl", avatarDataUrl);
    } catch {
      setAvatarError("头像读取失败，请重新选择图片。");
    }
  };

  const close = () => {
    if (saving) return;
    queueCloseToTrayPreview(getGeneralSettings().closeToTray);
    cancelPreview();
    navigate(initialLocation.current);
    setOpen(false);
  };

  const previewTheme = (theme: AppearanceTheme) => {
    setThemeDraft(theme);
    previewSettings(focusDraft, generalDraft, profileDraft, theme);
  };

  const restoreDefaults = () => {
    if (activeModule === "data") {
      setStorageThresholdDraft(DEFAULT_LOW_SPACE_THRESHOLD_GIB);
      setSaveError(null);
      return;
    }
    if (avatarPreviewUrl.current && typeof URL.revokeObjectURL === "function") {
      URL.revokeObjectURL(avatarPreviewUrl.current);
    }
    avatarPreviewUrl.current = null;
    setFocusDraft(DEFAULT_FOCUS_SETTINGS);
    setGeneralDraft(DEFAULT_GENERAL_SETTINGS);
    setProfileDraft(DEFAULT_PROFILE_SETTINGS);
    setThemeDraft(DEFAULT_THEME);
    setStorageThresholdDraft(DEFAULT_LOW_SPACE_THRESHOLD_GIB);
    setAvatarError(null);
    setAvatarFile(null);
    setAvatarOperation(
      settingsQuery.data &&
        getAppSetting(settingsQuery.data, "workspace").value.avatarRef
        ? "remove"
        : "unchanged",
    );
    setSaveError(null);
    previewSettings(
      DEFAULT_FOCUS_SETTINGS,
      DEFAULT_GENERAL_SETTINGS,
      DEFAULT_PROFILE_SETTINGS,
      DEFAULT_THEME,
    );
    queueCloseToTrayPreview(DEFAULT_GENERAL_SETTINGS.closeToTray);
    navigate(`/${DEFAULT_GENERAL_SETTINGS.defaultRoute}`);
  };

  const save = async () => {
    if (!settingsQuery.data || saving) return;
    const previousFocus = getFocusSettings();
    const nextFocus = sanitizeFocusSettings(focusDraft);
    const nextGeneral = sanitizeGeneralSettings(generalDraft);
    const nextProfile = sanitizeProfileSettings(profileDraft);
    previewSettings(nextFocus, nextGeneral, nextProfile, themeDraft);
    setSaveError(null);

    const currentWorkspace = getAppSetting(settingsQuery.data, "workspace");
    const currentGeneral = getAppSetting(settingsQuery.data, "general");
    const currentAppearance = getAppSetting(settingsQuery.data, "appearance");
    const currentFocus = getAppSetting(settingsQuery.data, "focus");
    const currentStorage = getAppSetting(settingsQuery.data, "storage");
    const updates: AppSettingUpdate[] = [];
    const nextWorkspace = {
      displayName: nextProfile.displayName,
      avatarRef: currentWorkspace.value.avatarRef,
    };
    if (
      avatarOperation !== "unchanged" ||
      !sameValue(nextWorkspace, currentWorkspace.value)
    ) {
      updates.push({
        key: "workspace",
        expectedVersion: currentWorkspace.version,
        value: nextWorkspace,
      });
    }
    if (!sameValue(nextGeneral, currentGeneral.value)) {
      updates.push({
        key: "general",
        expectedVersion: currentGeneral.version,
        value: nextGeneral,
      });
    }
    if (!sameValue({ theme: themeDraft }, currentAppearance.value)) {
      updates.push({
        key: "appearance",
        expectedVersion: currentAppearance.version,
        value: { theme: themeDraft },
      });
    }
    if (!sameValue(nextFocus, currentFocus.value)) {
      updates.push({
        key: "focus",
        expectedVersion: currentFocus.version,
        value: nextFocus,
      });
    }
    if (storageThresholdDraft !== currentStorage.value.lowSpaceThresholdGiB) {
      updates.push({
        key: "storage",
        expectedVersion: currentStorage.version,
        value: { lowSpaceThresholdGiB: storageThresholdDraft },
      });
    }

    try {
      const saved =
        avatarOperation !== "unchanged"
          ? await avatarMutation.mutateAsync({
              operation: avatarOperation,
              updates,
              file:
                avatarOperation === "replace"
                  ? (avatarFile ?? undefined)
                  : undefined,
            })
          : updates.length > 0
            ? await settingsMutation.mutateAsync(updates)
            : settingsQuery.data;
      let committedAvatarUrl = nextProfile.avatarDataUrl;
      if (avatarOperation !== "unchanged") {
        try {
          committedAvatarUrl = await workspaceAvatarUrlFromServer(saved);
        } catch {
          // The settings transaction is already committed. A transient content
          // read failure must not invite a stale multipart retry; the next app
          // bootstrap will load the authoritative controlled file again.
          committedAvatarUrl = null;
        }
      }
      replaceCommitted(committedSettingsFromServer(saved, committedAvatarUrl));
      queueCloseToTrayPreview(nextGeneral.closeToTray);
      onSettingsSaved?.(nextFocus, previousFocus);
      setOpen(false);
    } catch (error) {
      setSaveError(formatSettingsSaveError(error));
    }
  };

  const moduleContent = (() => {
    if (activeModule === "profile") {
      const profileInitial =
        Array.from(profileDraft.displayName.trim())[0]?.toUpperCase() || "O";

      return (
        <>
          <header className="settings-content-header">
            <h3>个人资料</h3>
            <p>修改左上角显示的头像和工作区名称。</p>
          </header>
          <div className="settings-group settings-profile-group">
            <div className="settings-avatar-row">
              <div className="settings-avatar-preview">
                {profileDraft.avatarDataUrl ? (
                  <img alt="头像预览" src={profileDraft.avatarDataUrl} />
                ) : (
                  profileInitial
                )}
              </div>
              <div className="settings-avatar-actions">
                <div className="settings-label">头像</div>
                <div className="settings-description">
                  支持 PNG、JPG、WebP，最大 2 MB
                </div>
                <div className="settings-avatar-buttons">
                  <label className="button button-secondary settings-avatar-upload">
                    <ImagePlus size={14} />
                    上传头像
                    <input
                      accept="image/png,image/jpeg,image/webp"
                      aria-label="上传头像"
                      onChange={(event) => void changeAvatar(event)}
                      type="file"
                    />
                  </label>
                  <button
                    className="button button-quiet"
                    disabled={!profileDraft.avatarDataUrl}
                    onClick={() => {
                      if (
                        avatarPreviewUrl.current &&
                        typeof URL.revokeObjectURL === "function"
                      ) {
                        URL.revokeObjectURL(avatarPreviewUrl.current);
                      }
                      avatarPreviewUrl.current = null;
                      setAvatarError(null);
                      setAvatarFile(null);
                      setAvatarOperation(
                        settingsQuery.data &&
                          getAppSetting(settingsQuery.data, "workspace").value
                            .avatarRef
                          ? "remove"
                          : "unchanged",
                      );
                      updateProfileDraft("avatarDataUrl", null);
                    }}
                    type="button"
                  >
                    <Trash2 size={14} />
                    移除
                  </button>
                </div>
                {avatarError ? (
                  <div className="settings-field-error" role="alert">
                    {avatarError}
                  </div>
                ) : null}
              </div>
            </div>
            <label className="settings-profile-name">
              <span className="settings-label">名称</span>
              <span className="settings-description">
                显示在应用左上角，最多 32 个字符
              </span>
              <input
                aria-label="名称"
                className="settings-text-input"
                maxLength={32}
                onChange={(event) =>
                  updateProfileDraft("displayName", event.target.value)
                }
                placeholder="输入名称"
                type="text"
                value={profileDraft.displayName}
              />
            </label>
          </div>
          <p className="settings-inline-note">
            名称保存到本机
            SQLite；头像在受控文件导入完成前保留在当前本机兼容存储。修改后都会立即预览。
          </p>
        </>
      );
    }

    if (activeModule === "general") {
      return (
        <>
          <header className="settings-content-header">
            <h3>通用</h3>
            <p>设置工作区启动位置、布局和交互偏好。</p>
          </header>
          <div className="settings-group">
            <label className="settings-select-row">
              <span>
                <span className="settings-label">默认首页</span>
                <span className="settings-description">
                  从应用根地址进入时首先打开的页面
                </span>
              </span>
              <select
                aria-label="默认首页"
                className="settings-select"
                onChange={(event) =>
                  updateGeneralDraft(
                    "defaultRoute",
                    event.target.value as DefaultRoute,
                  )
                }
                value={generalDraft.defaultRoute}
              >
                {defaultRouteOptions.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
            <Toggle
              checked={generalDraft.showRightOverview}
              description="在宽屏中显示专注、收入和客户概览"
              label="显示右侧概览"
              onChange={(value) =>
                updateGeneralDraft("showRightOverview", value)
              }
            />
            <Toggle
              checked={generalDraft.reduceMotion}
              description="缩短界面动画与过渡，减少视觉移动"
              label="减少动效"
              onChange={(value) => updateGeneralDraft("reduceMotion", value)}
            />
            <Toggle
              checked={generalDraft.closeToTray}
              description="关闭主窗口时保留桌面进程，可从系统托盘重新打开"
              label="关闭窗口时隐藏到托盘"
              onChange={(value) => updateGeneralDraft("closeToTray", value)}
            />
          </div>
          <p className="settings-inline-note">
            点击即预览桌面关闭行为；保存后持久生效，取消会恢复原设置。浏览器开发模式只保存偏好。
          </p>
        </>
      );
    }

    if (activeModule === "appearance") {
      return (
        <>
          <header className="settings-content-header">
            <h3>外观</h3>
            <p>选择工作区的颜色主题，点击选项可立即预览。</p>
          </header>
          <div className="settings-group settings-appearance-group">
            <div
              aria-label="主题模式"
              className="appearance-options"
              role="radiogroup"
            >
              <button
                aria-checked={themeDraft === "light"}
                className="appearance-option"
                data-selected={themeDraft === "light"}
                onClick={() => previewTheme("light")}
                role="radio"
                type="button"
              >
                <Sun size={18} />
                <span>
                  <strong>亮色</strong>
                  <small>明亮背景与深色文字</small>
                </span>
              </button>
              <button
                aria-checked={themeDraft === "dark"}
                className="appearance-option"
                data-selected={themeDraft === "dark"}
                onClick={() => previewTheme("dark")}
                role="radio"
                type="button"
              >
                <Moon size={18} />
                <span>
                  <strong>暗色</strong>
                  <small>Linear 风格深色界面</small>
                </span>
              </button>
            </div>
            <p className="settings-inline-note">
              点击后立即预览但不会写入本地；保存后生效，取消则恢复原主题。
            </p>
          </div>
        </>
      );
    }

    if (activeModule === "focus") {
      return (
        <>
          <header className="settings-content-header">
            <h3>专注</h3>
            <p>配置专注与休息节奏，以及自动衔接行为。</p>
          </header>
          <div className="settings-group focus-settings">
            <Stepper
              description="每个专注块"
              label="专注时长"
              max={120}
              min={5}
              onChange={(value) => updateFocusDraft("focusMinutes", value)}
              step={5}
              unit="分钟"
              value={focusDraft.focusMinutes}
            />
            <Stepper
              description="专注块之间"
              label="休息时长"
              max={30}
              min={5}
              onChange={(value) => updateFocusDraft("breakMinutes", value)}
              step={5}
              unit="分钟"
              value={focusDraft.breakMinutes}
            />
            <Stepper
              description="本轮专注"
              label="循环次数"
              max={8}
              min={1}
              onChange={(value) => updateFocusDraft("cycles", value)}
              unit="次"
              value={focusDraft.cycles}
            />
            <Toggle
              checked={focusDraft.autoStartBreak}
              description="专注阶段结束后立即开始休息计时"
              label="自动开始休息"
              onChange={(value) => updateFocusDraft("autoStartBreak", value)}
            />
            <Toggle
              checked={focusDraft.autoStartFocus}
              description="休息结束后立即开始下一轮专注"
              label="自动开始专注"
              onChange={(value) => updateFocusDraft("autoStartFocus", value)}
            />
            <Toggle
              checked={focusDraft.soundEnabled}
              description="阶段结束时播放本地提示音"
              label="结束后提示音"
              onChange={(value) => updateFocusDraft("soundEnabled", value)}
            />
          </div>
          <p className="settings-inline-note">
            调整后会立即同步到专注计时预览；保存确认，取消恢复原参数。
          </p>
        </>
      );
    }

    if (activeModule === "actors") {
      return <ActorSettings />;
    }

    if (activeModule === "agent") {
      return <AgentAdapterSettings />;
    }

    if (activeModule === "automation") {
      return <AutomationSettings />;
    }

    if (activeModule === "data") {
      const trendGroups = groupStorageCapacityPoints(
        storageCapacityHistoryQuery.data?.points ?? [],
      );
      return (
        <BackupSettings
          storageSettings={
            <div className="settings-group">
              <Stepper
                description="本地数据、受控文件或备份所在磁盘低于该值时创建维护提醒"
                label="低空间提醒阈值"
                max={100}
                min={1}
                onChange={setStorageThresholdDraft}
                unit="GiB"
                value={storageThresholdDraft}
              />
              <p className="settings-inline-note" aria-live="polite">
                当前预览：可用空间低于 {storageThresholdDraft} GiB
                时提醒；保存后从下一次容量检查起生效。
              </p>
              {storageCapacityQuery.isPending ? (
                <div className="settings-state" role="status">
                  <LoaderCircle className="animate-spin" size={16} />
                  正在检查受控位置容量…
                </div>
              ) : storageCapacityQuery.isError || !storageCapacityQuery.data ? (
                <div
                  className="settings-state settings-state-error"
                  role="alert"
                >
                  <AlertCircle size={16} />
                  <div>
                    <strong>容量检查未完成</strong>
                    <span>未读取或展示本机路径，可保留阈值草稿后重试。</span>
                  </div>
                  <button
                    className="button button-secondary"
                    onClick={() => void storageCapacityQuery.refetch()}
                    type="button"
                  >
                    <RefreshCw size={14} />
                    重试
                  </button>
                </div>
              ) : (
                <>
                  <div className="settings-about">
                    {storageCapacityQuery.data.locations.map((location) => (
                      <div className="settings-about-row" key={location.kind}>
                        <span>{storageLocationLabels[location.kind]}</span>
                        <strong
                          className="settings-health-state"
                          data-status={
                            location.status === "healthy" ? "ok" : "error"
                          }
                        >
                          {location.status === "unavailable"
                            ? "检查不可用"
                            : `${location.status === "low" ? "空间不足 · " : ""}${formatStorageBytes(location.availableBytes!)} 可用 / ${formatStorageBytes(location.totalBytes!)}${location.sharedVolume ? " · 与其他位置同卷" : ""}`}
                        </strong>
                      </div>
                    ))}
                  </div>
                  <div className="settings-about-actions">
                    <span className="settings-diagnostic-feedback">
                      本次按已保存阈值 {storageCapacityQuery.data.thresholdGiB}{" "}
                      GiB 检查，不展示路径
                    </span>
                    <button
                      className="button button-secondary"
                      disabled={storageCapacityQuery.isFetching}
                      onClick={() => {
                        void storageCapacityQuery
                          .refetch()
                          .then(() => storageCapacityHistoryQuery.refetch());
                      }}
                      type="button"
                    >
                      <RefreshCw
                        className={
                          storageCapacityQuery.isFetching
                            ? "animate-spin"
                            : undefined
                        }
                        size={14}
                      />
                      {storageCapacityQuery.isFetching
                        ? "检查中…"
                        : "重新检查容量"}
                    </button>
                  </div>
                  <div
                    className="settings-storage-trend"
                    aria-label="近 7 天容量趋势"
                  >
                    <div className="settings-storage-trend-header">
                      <div>
                        <strong>近 7 天容量趋势</strong>
                        <span>
                          每个物理卷最多每 15 分钟记录一次，保留 30 天
                        </span>
                      </div>
                    </div>
                    {storageCapacityHistoryQuery.isPending ? (
                      <div className="settings-state" role="status">
                        <LoaderCircle className="animate-spin" size={16} />
                        正在读取容量趋势…
                      </div>
                    ) : storageCapacityHistoryQuery.isError ? (
                      <div
                        className="settings-state settings-state-error"
                        role="alert"
                      >
                        <AlertCircle size={16} />
                        <div>
                          <strong>容量趋势读取失败</strong>
                          <span>
                            当前容量仍可使用；历史中不包含路径或卷标识。
                          </span>
                        </div>
                        <button
                          className="button button-secondary"
                          onClick={() =>
                            void storageCapacityHistoryQuery.refetch()
                          }
                          type="button"
                        >
                          <RefreshCw size={14} />
                          重试趋势
                        </button>
                      </div>
                    ) : trendGroups.length === 0 ? (
                      <div className="settings-state settings-state-empty">
                        <Info size={16} />
                        首次容量样本正在积累，下一次检查后会形成趋势。
                      </div>
                    ) : (
                      <div className="settings-storage-trend-grid">
                        {trendGroups.map(([scope, points]) => {
                          const latest = points.at(-1)!;
                          const first = points[0];
                          const delta =
                            latest.availableBytes - first.availableBytes;
                          return (
                            <div
                              className="settings-storage-trend-card"
                              key={scope}
                            >
                              <div>
                                <strong>{storageScopeLabel(scope)}</strong>
                                <span>
                                  {formatStorageBytes(latest.availableBytes)}{" "}
                                  可用
                                  {points.length > 1
                                    ? ` · ${delta >= 0 ? "+" : ""}${formatStorageBytes(Math.abs(delta))}${delta < 0 ? " 减少" : " 变化"}`
                                    : " · 首个样本"}
                                </span>
                              </div>
                              <svg aria-hidden="true" viewBox="0 0 100 40">
                                <polyline
                                  data-status={latest.status}
                                  fill="none"
                                  points={storageTrendLine(points)}
                                  vectorEffect="non-scaling-stroke"
                                />
                              </svg>
                            </div>
                          );
                        })}
                      </div>
                    )}
                  </div>
                </>
              )}
            </div>
          }
        />
      );
    }

    if (activeModule === "diagnostics") {
      if (
        healthQuery.isPending ||
        runtimeDiagnosticsPending ||
        (!runtimeDiagnostics && !runtimeDiagnosticsError)
      ) {
        return (
          <>
            <header className="settings-content-header">
              <h3>运行诊断</h3>
              <p>核对桌面壳、本地 API 与数据库版本事实。</p>
            </header>
            <div aria-live="polite" className="settings-state" role="status">
              <LoaderCircle className="animate-spin" size={16} />
              正在检查本地运行环境…
            </div>
          </>
        );
      }

      if ((healthQuery.isError && !healthQuery.data) || !runtimeDiagnostics) {
        return (
          <>
            <header className="settings-content-header">
              <h3>运行诊断</h3>
              <p>核对桌面壳、本地 API 与数据库版本事实。</p>
            </header>
            <div className="settings-state settings-state-error" role="alert">
              <AlertCircle size={16} />
              <div>
                <strong>运行诊断未完成</strong>
                <span>
                  {healthQuery.isError && !healthQuery.data
                    ? formatHealthError(healthQuery.error)
                    : "无法读取桌面生命周期状态；未展示底层错误或本地路径。"}
                </span>
              </div>
              <button
                className="button button-secondary"
                onClick={refreshDiagnostics}
                type="button"
              >
                <RefreshCw size={14} />
                重试
              </button>
            </div>
          </>
        );
      }

      const health = healthQuery.data!;
      const desktopVersionComplete =
        runtimeDiagnostics.environment === "desktop" &&
        runtimeDiagnostics.appVersion !== null &&
        runtimeDiagnostics.apiVersion !== null &&
        runtimeDiagnostics.schemaVersion !== null;
      const versionsMatch =
        desktopVersionComplete &&
        runtimeDiagnostics.appVersion === health.app.version &&
        runtimeDiagnostics.apiVersion === health.api.version &&
        runtimeDiagnostics.schemaVersion === String(health.schema.version);
      const phaseLabel =
        runtimeDiagnostics.phase === "ready"
          ? "就绪"
          : runtimeDiagnostics.phase === "starting"
            ? "启动中"
            : runtimeDiagnostics.phase === "restarting"
              ? "重启中"
              : runtimeDiagnostics.phase === "error"
                ? "异常"
                : "外部开发进程";
      const compatibilityLabel =
        runtimeDiagnostics.environment === "browser"
          ? "由 HTTP 健康检查确认"
          : versionsMatch
            ? "桌面握手与 API 一致"
            : desktopVersionComplete
              ? "桌面握手与 API 不一致"
              : "桌面握手版本不完整";
      const nativeShortcuts = runtimeDiagnostics.nativeShortcuts ?? null;
      const shortcutLabel =
        runtimeDiagnostics.environment !== "desktop"
          ? "浏览器内 Ctrl/Cmd+K、Ctrl/Cmd+N"
          : nativeShortcuts === null
            ? "状态未读取；保留应用内快捷键"
            : nativeShortcuts.commandPalette === "registered" &&
                nativeShortcuts.newTask === "registered"
              ? "已注册 ⌘/Ctrl+Shift+K、⌘/Ctrl+Shift+N"
              : "部分不可用；保留应用内快捷键";
      const trayLabel =
        runtimeDiagnostics.environment !== "desktop"
          ? "仅桌面应用提供"
          : runtimeDiagnostics.desktopCapabilities === null
            ? "状态未读取"
            : runtimeDiagnostics.desktopCapabilities.tray === "available"
              ? "可用 · 关闭窗口时隐藏"
              : "不可用 · 不拦截关闭窗口";

      return (
        <>
          <header className="settings-content-header">
            <h3>运行诊断</h3>
            <p>只展示可安全分享的版本和状态，不读取业务正文。</p>
          </header>
          <div className="settings-about">
            <div className="settings-about-row">
              <span>运行环境</span>
              <strong>
                {runtimeDiagnostics.environment === "desktop"
                  ? "Tauri 桌面"
                  : "浏览器开发模式"}
              </strong>
            </div>
            <div className="settings-about-row">
              <span>Sidecar 生命周期</span>
              <strong
                className="settings-health-state"
                data-status={
                  runtimeDiagnostics.phase === "ready" ||
                  runtimeDiagnostics.phase === "external"
                    ? "ok"
                    : runtimeDiagnostics.phase
                }
              >
                {runtimeDiagnostics.phase === "ready" ||
                runtimeDiagnostics.phase === "external" ? (
                  <CheckCircle2 size={13} />
                ) : (
                  <AlertCircle size={13} />
                )}
                {phaseLabel}
              </strong>
            </div>
            <div className="settings-about-row">
              <span>本地 API</span>
              <strong>
                {health.status === "ok" ? "健康检查通过" : health.status}
              </strong>
            </div>
            <div className="settings-about-row">
              <span>版本兼容</span>
              <strong>{compatibilityLabel}</strong>
            </div>
            <div className="settings-about-row">
              <span>应用 / API</span>
              <strong>
                {displayVersion(health.app.version)} · {health.api.version}
              </strong>
            </div>
            <div className="settings-about-row">
              <span>数据库 schema</span>
              <strong>v{health.schema.version}</strong>
            </div>
            <div className="settings-about-row">
              <span>全局快捷键</span>
              <strong>{shortcutLabel}</strong>
            </div>
            <div className="settings-about-row">
              <span>系统托盘</span>
              <strong>{trayLabel}</strong>
            </div>
          </div>
          {healthQuery.isError ? (
            <p
              className="settings-inline-note settings-inline-warning"
              role="alert"
            >
              最近一次 HTTP 检查失败，当前展示上一次成功结果：
              {formatHealthError(healthQuery.error)}
            </p>
          ) : null}
          <div className="settings-about-actions settings-diagnostic-actions">
            <span aria-live="polite" className="settings-diagnostic-feedback">
              {diagnosticPackageFeedback ??
                (diagnosticCopyState === "copied"
                  ? "脱敏摘要已复制"
                  : diagnosticCopyState === "error"
                    ? "浏览器未允许复制，请稍后重试"
                    : "")}
            </span>
            <button
              className="button button-secondary"
              disabled={diagnosticPackageMutation.isPending}
              onClick={downloadDiagnostics}
              type="button"
            >
              {diagnosticPackageMutation.isPending ? (
                <LoaderCircle className="animate-spin" size={14} />
              ) : (
                <Download size={14} />
              )}
              {diagnosticPackageMutation.isPending ? "正在生成…" : "生成诊断包"}
            </button>
            <button
              className="button button-secondary"
              onClick={() => void copyDiagnosticSummary()}
              type="button"
            >
              <Copy size={14} />
              复制脱敏摘要
            </button>
            <button
              className="button button-secondary"
              disabled={runtimeDiagnostics.environment !== "desktop"}
              onClick={() => void openLogDirectory()}
              title={
                runtimeDiagnostics.environment === "desktop"
                  ? "打开应用日志目录"
                  : "浏览器开发模式由外部终端管理日志"
              }
              type="button"
            >
              <FolderOpen size={14} />
              打开日志目录
            </button>
            <button
              className="button button-secondary"
              disabled={healthQuery.isFetching || runtimeDiagnosticsPending}
              onClick={refreshDiagnostics}
              type="button"
            >
              <RefreshCw
                className={
                  healthQuery.isFetching || runtimeDiagnosticsPending
                    ? "animate-spin"
                    : undefined
                }
                size={14}
              />
              {healthQuery.isFetching || runtimeDiagnosticsPending
                ? "检查中…"
                : "重新检查"}
            </button>
          </div>
          <p className="settings-inline-note">
            摘要与诊断包不会包含会话令牌、监听地址、本地路径、底层错误或业务数据；诊断包
            v1
            只包含版本、平台、数据库健康、迁移清单和系统维护错误码汇总，不包含原始日志。
          </p>
        </>
      );
    }

    if (healthQuery.isPending) {
      return (
        <>
          <header className="settings-content-header">
            <h3>关于</h3>
            <p>读取应用、本地 API 与数据库版本。</p>
          </header>
          <div aria-live="polite" className="settings-state" role="status">
            <LoaderCircle className="animate-spin" size={16} />
            正在检查本地服务…
          </div>
        </>
      );
    }

    if (healthQuery.isError && !healthQuery.data) {
      return (
        <>
          <header className="settings-content-header">
            <h3>关于</h3>
            <p>读取应用、本地 API 与数据库版本。</p>
          </header>
          <div className="settings-state settings-state-error" role="alert">
            <AlertCircle size={16} />
            <div>
              <strong>本地服务检查失败</strong>
              <span>{formatHealthError(healthQuery.error)}</span>
            </div>
            <button
              className="button button-secondary"
              onClick={() => void healthQuery.refetch()}
              type="button"
            >
              <RefreshCw size={14} />
              重试
            </button>
          </div>
        </>
      );
    }

    const health = healthQuery.data!;
    const commit = displayCommit(health.app.commit);
    return (
      <>
        <header className="settings-content-header">
          <h3>关于</h3>
          <p>真实运行版本、本地服务状态与数据边界。</p>
        </header>
        <div className="settings-about">
          <div className="settings-about-row">
            <span>本地服务</span>
            <strong
              className="settings-health-state"
              data-status={health.status}
            >
              {health.status === "ok" ? (
                <CheckCircle2 size={13} />
              ) : (
                <AlertCircle size={13} />
              )}
              {health.status === "ok" ? "就绪" : health.status}
            </strong>
          </div>
          <div className="settings-about-row">
            <span>应用</span>
            <strong>{health.app.name}</strong>
          </div>
          <div className="settings-about-row">
            <span>运行版本</span>
            <strong>{displayVersion(health.app.version)}</strong>
          </div>
          <div className="settings-about-row">
            <span>构建提交</span>
            <strong title={health.app.commit}>{commit}</strong>
          </div>
          <div className="settings-about-row">
            <span>本地 API</span>
            <strong>{health.api.version}</strong>
          </div>
          <div className="settings-about-row">
            <span>数据库 schema</span>
            <strong>v{health.schema.version}</strong>
          </div>
          <div className="settings-about-row">
            <span>数据存储</span>
            <strong>本地 SQLite · 可用</strong>
          </div>
          <div className="settings-about-row">
            <span>桌面架构</span>
            <strong>Tauri + Go Sidecar</strong>
          </div>
          <div className="settings-about-row">
            <span>云同步</span>
            <strong>未启用</strong>
          </div>
        </div>
        {healthQuery.isError ? (
          <p
            className="settings-inline-note settings-inline-warning"
            role="alert"
          >
            最近一次重新检查失败，当前展示上一次成功结果：
            {formatHealthError(healthQuery.error)}
          </p>
        ) : null}
        <div className="settings-about-actions">
          <button
            className="button button-secondary"
            disabled={healthQuery.isFetching}
            onClick={() => void healthQuery.refetch()}
            type="button"
          >
            <RefreshCw
              className={healthQuery.isFetching ? "animate-spin" : undefined}
              size={14}
            />
            {healthQuery.isFetching ? "检查中…" : "重新检查"}
          </button>
        </div>
        <p className="settings-inline-note">
          当前核心数据保存在本机，不依赖 Docker 或远程云服务。
        </p>
      </>
    );
  })();

  return (
    <Modal
      footer={
        activeModule === "actors" ||
        activeModule === "automation" ||
        activeModule === "diagnostics" ||
        activeModule === "about" ? (
          <>
            {activeModule === "actors" ? (
              <span className="settings-actor-footer-note">
                人员资料通过模块内按钮单独保存
              </span>
            ) : null}
            <button
              className="button button-secondary"
              disabled={saving}
              onClick={close}
              type="button"
            >
              关闭
            </button>
          </>
        ) : (
          <>
            <button
              className="button button-quiet settings-reset"
              disabled={saving}
              onClick={restoreDefaults}
              type="button"
            >
              <RotateCcw size={14} />
              恢复默认
            </button>
            <button
              className="button button-secondary"
              disabled={saving}
              onClick={close}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-primary"
              disabled={
                saving || settingsQuery.isFetching || !settingsQuery.data
              }
              onClick={() => void save()}
              type="button"
            >
              {saving ? (
                <>
                  <LoaderCircle className="animate-spin" size={14} />
                  保存中…
                </>
              ) : (
                "保存"
              )}
            </button>
          </>
        )
      }
      onClose={close}
      open={open}
      title="设置"
      width="760px"
    >
      <fieldset
        aria-busy={saving}
        className="settings-layout"
        disabled={saving}
      >
        <nav aria-label="设置模块" className="settings-module-nav">
          {modules.map(({ id, label, icon: Icon }) => (
            <button
              aria-current={activeModule === id ? "page" : undefined}
              className="settings-module-item"
              data-active={activeModule === id}
              key={id}
              onClick={() => setActiveModule(id)}
              ref={activeModule === id ? activeModuleButtonRef : undefined}
              type="button"
            >
              <Icon size={16} />
              <span>{label}</span>
            </button>
          ))}
        </nav>
        <section
          aria-label={`${modules.find((item) => item.id === activeModule)?.label}设置`}
          className="settings-module-content"
        >
          {moduleContent}
          {activeModule !== "actors" &&
          activeModule !== "automation" &&
          activeModule !== "data" &&
          activeModule !== "about" &&
          !settingsQuery.data &&
          settingsQuery.isError ? (
            <div className="settings-state-error" role="alert">
              <AlertCircle size={16} />
              <div>
                <strong>无法读取已保存设置</strong>
                <span>{formatSettingsSaveError(settingsQuery.error)}</span>
              </div>
            </div>
          ) : null}
          {activeModule !== "actors" &&
          activeModule !== "automation" &&
          activeModule !== "data" &&
          activeModule !== "about" &&
          saveError ? (
            <div className="settings-state-error" role="alert">
              <AlertCircle size={16} />
              <div>
                <strong>设置未保存</strong>
                <span>{saveError}</span>
              </div>
            </div>
          ) : null}
        </section>
      </fieldset>
    </Modal>
  );
}
