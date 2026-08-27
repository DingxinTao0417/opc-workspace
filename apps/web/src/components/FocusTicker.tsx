import { useEffect, useMemo } from "react";
import { useFocusStore, type FocusConfig } from "../store/focus";
import { useSettingsStore } from "../store/settings";

function playPhaseCompleteSound() {
  try {
    const AudioContextConstructor =
      window.AudioContext ??
      (
        window as typeof window & {
          webkitAudioContext?: typeof AudioContext;
        }
      ).webkitAudioContext;

    if (!AudioContextConstructor) {
      return;
    }

    const context = new AudioContextConstructor();
    const oscillator = context.createOscillator();
    const gain = context.createGain();

    oscillator.frequency.value = 660;
    gain.gain.setValueAtTime(0.08, context.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, context.currentTime + 0.14);
    oscillator.connect(gain);
    gain.connect(context.destination);
    oscillator.start();
    oscillator.stop(context.currentTime + 0.14);
    oscillator.addEventListener("ended", () => {
      void context.close().catch(() => undefined);
    });
  } catch {
    // Audio may be unavailable or blocked until the user interacts with the app.
  }
}

export function FocusTicker() {
  const focusMinutes = useSettingsStore((state) => state.focusMinutes);
  const breakMinutes = useSettingsStore((state) => state.breakMinutes);
  const cycles = useSettingsStore((state) => state.cycles);
  const autoStartBreak = useSettingsStore((state) => state.autoStartBreak);
  const autoStartFocus = useSettingsStore((state) => state.autoStartFocus);
  const soundEnabled = useSettingsStore((state) => state.soundEnabled);
  const running = useFocusStore((state) => state.running);
  const setDurationsOrSync = useFocusStore((state) => state.setDurationsOrSync);
  const tick = useFocusStore((state) => state.tick);

  const config = useMemo<FocusConfig>(
    () => ({
      focusMinutes,
      breakMinutes,
      cycles,
      autoStartBreak,
      autoStartFocus,
      soundEnabled,
    }),
    [
      autoStartBreak,
      autoStartFocus,
      breakMinutes,
      cycles,
      focusMinutes,
      soundEnabled,
    ],
  );

  useEffect(() => {
    setDurationsOrSync(config);
  }, [config, setDurationsOrSync]);

  useEffect(() => {
    if (!running) {
      return;
    }

    const interval = window.setInterval(() => {
      const state = useFocusStore.getState();
      const phaseWillEnd = state.running && state.remainingSeconds <= 1;

      state.tick();

      if (phaseWillEnd && state.soundEnabled) {
        playPhaseCompleteSound();
      }
    }, 1_000);

    return () => window.clearInterval(interval);
  }, [running, tick]);

  return null;
}
