import {
  AlertCircle,
  CheckCircle2,
  DatabaseBackup,
  Focus,
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
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useEffect, useRef, useState, type ChangeEvent } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { ApiError, getAppSetting } from "../api/client";
import {
  useAppSettingsQuery,
  useHealthQuery,
  useUpdateAppSettings,
} from "../api/hooks";
import { committedSettingsFromServer } from "../settings/bootstrap";
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
import type { AppSettingUpdate } from "../types/models";
import { ActorSettings } from "./ActorSettings";
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
  { id: "data", label: "数据与备份", icon: DatabaseBackup },
  { id: "about", label: "关于", icon: Info },
];

const MAX_AVATAR_FILE_BYTES = 2 * 1024 * 1024;
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
  const [avatarError, setAvatarError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const settingsQuery = useAppSettingsQuery(open);
  const settingsMutation = useUpdateAppSettings();
  const healthQuery = useHealthQuery(open && activeModule === "about");

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
    setSaveError(null);
    settingsMutation.reset();
    beginPreview();

    return cancelPreview;
  }, [beginPreview, cancelPreview, open, requestedModule]);

  const previewSettings = (
    focus: FocusSettings,
    general: GeneralSettings,
    profile: ProfileSettings,
    theme: AppearanceTheme,
  ) => setPreview({ focus, general, profile, theme });

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
  };

  const updateProfileDraft = <Key extends keyof ProfileSettings>(
    key: Key,
    value: ProfileSettings[Key],
  ) => {
    const nextProfile = { ...profileDraft, [key]: value };
    setProfileDraft(nextProfile);
    previewSettings(focusDraft, generalDraft, nextProfile, themeDraft);
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
      const avatarDataUrl = await readFileAsDataUrl(file);
      setAvatarError(null);
      updateProfileDraft("avatarDataUrl", avatarDataUrl);
    } catch {
      setAvatarError("头像读取失败，请重新选择图片。");
    }
  };

  const close = () => {
    if (settingsMutation.isPending) return;
    cancelPreview();
    navigate(initialLocation.current);
    setOpen(false);
  };

  const previewTheme = (theme: AppearanceTheme) => {
    setThemeDraft(theme);
    previewSettings(focusDraft, generalDraft, profileDraft, theme);
  };

  const restoreDefaults = () => {
    setFocusDraft(DEFAULT_FOCUS_SETTINGS);
    setGeneralDraft(DEFAULT_GENERAL_SETTINGS);
    setProfileDraft(DEFAULT_PROFILE_SETTINGS);
    setThemeDraft(DEFAULT_THEME);
    setAvatarError(null);
    setSaveError(null);
    previewSettings(
      DEFAULT_FOCUS_SETTINGS,
      DEFAULT_GENERAL_SETTINGS,
      DEFAULT_PROFILE_SETTINGS,
      DEFAULT_THEME,
    );
    navigate(`/${DEFAULT_GENERAL_SETTINGS.defaultRoute}`);
  };

  const save = async () => {
    if (!settingsQuery.data || settingsMutation.isPending) return;
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
    const updates: AppSettingUpdate[] = [];
    const nextWorkspace = {
      displayName: nextProfile.displayName,
      avatarRef: currentWorkspace.value.avatarRef,
    };
    if (!sameValue(nextWorkspace, currentWorkspace.value)) {
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

    try {
      const saved =
        updates.length > 0
          ? await settingsMutation.mutateAsync(updates)
          : settingsQuery.data;
      replaceCommitted(
        committedSettingsFromServer(saved, nextProfile.avatarDataUrl),
      );
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
                      setAvatarError(null);
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
          </div>
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

    if (activeModule === "data") {
      return <BackupSettings />;
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
        activeModule === "data" ||
        activeModule === "about" ? (
          <>
            {activeModule === "actors" ? (
              <span className="settings-actor-footer-note">
                人员资料通过模块内按钮单独保存
              </span>
            ) : null}
            <button
              className="button button-secondary"
              disabled={settingsMutation.isPending}
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
              disabled={settingsMutation.isPending}
              onClick={restoreDefaults}
              type="button"
            >
              <RotateCcw size={14} />
              恢复默认
            </button>
            <button
              className="button button-secondary"
              disabled={settingsMutation.isPending}
              onClick={close}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-primary"
              disabled={
                settingsMutation.isPending ||
                settingsQuery.isFetching ||
                !settingsQuery.data
              }
              onClick={() => void save()}
              type="button"
            >
              {settingsMutation.isPending ? (
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
        aria-busy={settingsMutation.isPending}
        className="settings-layout"
        disabled={settingsMutation.isPending}
      >
        <nav aria-label="设置模块" className="settings-module-nav">
          {modules.map(({ id, label, icon: Icon }) => (
            <button
              aria-current={activeModule === id ? "page" : undefined}
              className="settings-module-item"
              data-active={activeModule === id}
              key={id}
              onClick={() => setActiveModule(id)}
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
