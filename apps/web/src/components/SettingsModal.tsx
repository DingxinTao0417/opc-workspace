import {
  Focus,
  ImagePlus,
  Info,
  Minus,
  Moon,
  Palette,
  Plus,
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
import { ActorSettings } from "./ActorSettings";
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
  const commitPreview = useSettingsStore((state) => state.commitPreview);
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
    previewSettings(
      DEFAULT_FOCUS_SETTINGS,
      DEFAULT_GENERAL_SETTINGS,
      DEFAULT_PROFILE_SETTINGS,
      DEFAULT_THEME,
    );
    navigate(`/${DEFAULT_GENERAL_SETTINGS.defaultRoute}`);
  };

  const save = () => {
    const previousFocus = getFocusSettings();
    const nextFocus = sanitizeFocusSettings(focusDraft);
    const nextGeneral = sanitizeGeneralSettings(generalDraft);
    const nextProfile = sanitizeProfileSettings(profileDraft);
    previewSettings(nextFocus, nextGeneral, nextProfile, themeDraft);
    commitPreview();
    onSettingsSaved?.(nextFocus, previousFocus);
    setOpen(false);
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
            头像和名称仅保存在本机，修改后会立即在左上角预览。
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

    return (
      <>
        <header className="settings-content-header">
          <h3>关于</h3>
          <p>opc-workspace 当前版本与本地数据边界。</p>
        </header>
        <div className="settings-about">
          <div className="settings-about-row">
            <span>应用版本</span>
            <strong>v0.1.0</strong>
          </div>
          <div className="settings-about-row">
            <span>数据存储</span>
            <strong>本地 SQLite</strong>
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
        <p className="settings-inline-note">
          当前核心数据保存在本机，不依赖 Docker 或远程云服务。
        </p>
      </>
    );
  })();

  return (
    <Modal
      footer={
        activeModule === "actors" ? (
          <>
            <span className="settings-actor-footer-note">
              人员资料通过模块内按钮单独保存
            </span>
            <button
              className="button button-secondary"
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
              onClick={restoreDefaults}
              type="button"
            >
              <RotateCcw size={14} />
              恢复默认
            </button>
            <button
              className="button button-secondary"
              onClick={close}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-primary"
              onClick={save}
              type="button"
            >
              保存
            </button>
          </>
        )
      }
      onClose={close}
      open={open}
      title="设置"
      width="760px"
    >
      <div className="settings-layout">
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
        </section>
      </div>
    </Modal>
  );
}
