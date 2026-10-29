import { useEffect, useLayoutEffect } from "react";
import { setInterfaceZoom } from "../api/desktop";
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
  const fontScale = useSettingsStore(
    (state) => state.preview?.fontScale ?? state.fontScale,
  );

  useLayoutEffect(() => {
    applyTheme(theme);
  }, [theme]);

  useLayoutEffect(() => {
    applyReduceMotion(reduceMotion);
  }, [reduceMotion]);

  useEffect(() => {
    // A refused or unavailable zoom leaves the view as it is; the stored
    // preference is unaffected.
    void setInterfaceZoom(fontScale).catch(() => undefined);
  }, [fontScale]);

  return null;
}
