import { useLayoutEffect } from "react";
import { type AppearanceTheme, useSettingsStore } from "../store/settings";

export function applyTheme(theme: AppearanceTheme) {
  document.documentElement.dataset.theme = theme;
  document.documentElement.style.colorScheme = theme;
}

export function ThemeController() {
  const theme = useSettingsStore((state) => state.theme);

  useLayoutEffect(() => {
    applyTheme(theme);
  }, [theme]);

  return null;
}
