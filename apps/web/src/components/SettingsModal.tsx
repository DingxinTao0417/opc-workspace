import { Minus, Plus, RotateCcw } from "lucide-react";
import { useEffect, useState } from "react";
import {
  DEFAULT_FOCUS_SETTINGS,
  getFocusSettings,
  sanitizeFocusSettings,
  type FocusSettings,
  useSettingsStore,
} from "../store/settings";
import { useUiStore } from "../store/ui";
import { Modal } from "./Modal";

interface SettingsModalProps {
  onSettingsSaved?: (next: FocusSettings, previous: FocusSettings) => void;
}

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
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <div className="settings-toggle-row">
      <span className="settings-label settings-toggle-label">{label}</span>
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
  const open = useUiStore((state) => state.settingsOpen);
  const setOpen = useUiStore((state) => state.setSettingsOpen);
  const setSettings = useSettingsStore((state) => state.setSettings);
  const [draft, setDraft] = useState<FocusSettings>(DEFAULT_FOCUS_SETTINGS);

  useEffect(() => {
    if (open) setDraft(getFocusSettings());
  }, [open]);

  const updateDraft = <Key extends keyof FocusSettings>(
    key: Key,
    value: FocusSettings[Key],
  ) => setDraft((current) => ({ ...current, [key]: value }));

  const close = () => setOpen(false);

  const save = () => {
    const previous = getFocusSettings();
    const next = sanitizeFocusSettings(draft);
    setSettings(next);
    onSettingsSaved?.(next, previous);
    close();
  };

  return (
    <Modal
      footer={
        <>
          <button
            className="button button-quiet settings-reset"
            onClick={() => setDraft(DEFAULT_FOCUS_SETTINGS)}
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
      }
      onClose={close}
      open={open}
      title="专注模式"
      width="460px"
    >
      <>
        <div className="focus-settings">
          <Stepper
            description="每个专注块"
            label="专注时长"
            max={120}
            min={5}
            onChange={(value) => updateDraft("focusMinutes", value)}
            step={5}
            unit="分钟"
            value={draft.focusMinutes}
          />
          <Stepper
            description="专注块之间"
            label="休息时长"
            max={30}
            min={5}
            onChange={(value) => updateDraft("breakMinutes", value)}
            step={5}
            unit="分钟"
            value={draft.breakMinutes}
          />
          <Stepper
            description="本轮专注"
            label="循环次数"
            max={8}
            min={1}
            onChange={(value) => updateDraft("cycles", value)}
            unit="次"
            value={draft.cycles}
          />
          <Toggle
            checked={draft.autoStartBreak}
            label="自动开始休息"
            onChange={(value) => updateDraft("autoStartBreak", value)}
          />
          <Toggle
            checked={draft.autoStartFocus}
            label="自动开始专注"
            onChange={(value) => updateDraft("autoStartFocus", value)}
          />
          <Toggle
            checked={draft.soundEnabled}
            label="结束后提示音"
            onChange={(value) => updateDraft("soundEnabled", value)}
          />
        </div>
        <p className="settings-note">保存设置会暂停并重置当前计时。</p>
      </>
    </Modal>
  );
}
