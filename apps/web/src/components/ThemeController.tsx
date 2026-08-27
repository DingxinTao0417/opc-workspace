import { useLayoutEffect } from "react";
import { type AppearanceTheme, useSettingsStore } from "../store/settings";

export function applyTheme(theme: AppearanceTheme) {
  document.documentElement.dataset.theme = theme;
  document.documentElement.style.colorScheme = theme;
}

export function applyReduceMotion(enabled: boolean) {
  document.documentElement.dataset.reduceMotion = String(enabled);
}

export function ThemeController() {
  const theme = useSettingsStore(
    (state) => state.preview?.theme ?? state.theme,
  );
  const reduceMotion = useSettingsStore(
    (state) => state.preview?.general.reduceMotion ?? state.reduceMotion,
  );

  useLayoutEffect(() => {
    applyTheme(theme);
  }, [theme]);

  useLayoutEffect(() => {
    applyReduceMotion(reduceMotion);
  }, [reduceMotion]);

  return null;
}
