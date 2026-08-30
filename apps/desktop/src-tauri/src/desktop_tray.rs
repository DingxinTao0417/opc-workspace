use std::sync::atomic::{AtomicBool, Ordering};

use serde::Serialize;
use tauri::{
    App, AppHandle, Manager, State,
    image::Image,
    menu::{Menu, MenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
};

use crate::{desktop_log::DesktopEvent, sidecar::SidecarManager};

const MAIN_WINDOW_LABEL: &str = "main";
const SHOW_MAIN_WINDOW_ID: &str = "tray_show_main_window";
const EXIT_APPLICATION_ID: &str = "tray_exit_application";

pub struct DesktopTrayState {
    available: AtomicBool,
    close_to_tray: AtomicBool,
}

impl Default for DesktopTrayState {
    fn default() -> Self {
        Self {
            available: AtomicBool::new(false),
            close_to_tray: AtomicBool::new(true),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum DesktopCapabilityAvailability {
    Available,
    Unavailable,
    NotImplemented,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DesktopCapabilities {
    tray: DesktopCapabilityAvailability,
    native_notifications: DesktopCapabilityAvailability,
    autostart: DesktopCapabilityAvailability,
    native_file_dialogs: DesktopCapabilityAvailability,
    offline_updates: DesktopCapabilityAvailability,
}

impl DesktopTrayState {
    fn mark_available(&self) {
        self.available.store(true, Ordering::Release);
    }

    fn is_available(&self) -> bool {
        self.available.load(Ordering::Acquire)
    }

    fn set_close_to_tray_enabled(&self, enabled: bool) {
        self.close_to_tray.store(enabled, Ordering::Release);
    }

    fn should_hide_on_close(&self) -> bool {
        self.is_available() && self.close_to_tray.load(Ordering::Acquire)
    }

    fn capabilities(&self) -> DesktopCapabilities {
        DesktopCapabilities {
            tray: if self.is_available() {
                DesktopCapabilityAvailability::Available
            } else {
                DesktopCapabilityAvailability::Unavailable
            },
            native_notifications: DesktopCapabilityAvailability::NotImplemented,
            autostart: DesktopCapabilityAvailability::NotImplemented,
            native_file_dialogs: DesktopCapabilityAvailability::NotImplemented,
            offline_updates: DesktopCapabilityAvailability::NotImplemented,
        }
    }
}

#[tauri::command]
pub fn desktop_capabilities(state: State<'_, DesktopTrayState>) -> DesktopCapabilities {
    state.capabilities()
}

#[tauri::command]
pub fn set_close_to_tray_enabled(enabled: bool, state: State<'_, DesktopTrayState>) {
    state.set_close_to_tray_enabled(enabled);
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum TrayMenuAction {
    ShowMainWindow,
    ExitApplication,
}

fn menu_action(id: &str) -> Option<TrayMenuAction> {
    match id {
        SHOW_MAIN_WINDOW_ID => Some(TrayMenuAction::ShowMainWindow),
        EXIT_APPLICATION_ID => Some(TrayMenuAction::ExitApplication),
        _ => None,
    }
}

fn should_show_from_click(button: MouseButton, state: MouseButtonState) -> bool {
    button == MouseButton::Left && state == MouseButtonState::Up
}

fn log(app: &AppHandle, event: DesktopEvent) {
    app.state::<SidecarManager>().log_event(event);
}

pub fn show_main_window(app: &AppHandle) -> bool {
    let Some(window) = app.get_webview_window(MAIN_WINDOW_LABEL) else {
        return false;
    };
    if window.show().is_err() {
        return false;
    }
    let _ = window.unminimize();
    let _ = window.set_focus();
    log(app, DesktopEvent::MainWindowShownFromTray);
    true
}

pub fn hide_main_window_to_tray(app: &AppHandle) -> bool {
    if !app.state::<DesktopTrayState>().should_hide_on_close() {
        return false;
    }
    let Some(window) = app.get_webview_window(MAIN_WINDOW_LABEL) else {
        return false;
    };
    if window.hide().is_err() {
        return false;
    }
    log(app, DesktopEvent::MainWindowHiddenToTray);
    true
}

pub fn install_desktop_tray(app: &mut App) {
    if try_install_desktop_tray(app).is_ok() {
        app.state::<DesktopTrayState>().mark_available();
        log(app.handle(), DesktopEvent::TrayReady);
    } else {
        log(app.handle(), DesktopEvent::TrayUnavailable);
    }
}

fn try_install_desktop_tray(app: &mut App) -> tauri::Result<()> {
    let show = MenuItem::with_id(
        app,
        SHOW_MAIN_WINDOW_ID,
        "显示 opc-workspace",
        true,
        None::<&str>,
    )?;
    let exit = MenuItem::with_id(
        app,
        EXIT_APPLICATION_ID,
        "退出 opc-workspace",
        true,
        None::<&str>,
    )?;
    let menu = Menu::with_items(app, &[&show, &exit])?;
    let default_icon = app
        .default_window_icon()
        .cloned()
        .unwrap_or_else(fallback_tray_icon);

    TrayIconBuilder::with_id("opc-workspace-main")
        .icon(default_icon)
        .tooltip("opc-workspace")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| match menu_action(event.id().as_ref()) {
            Some(TrayMenuAction::ShowMainWindow) => {
                show_main_window(app);
            }
            Some(TrayMenuAction::ExitApplication) => {
                log(app, DesktopEvent::TrayExitRequested);
                app.exit(0);
            }
            None => {}
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button,
                button_state,
                ..
            } = event
            {
                if should_show_from_click(button, button_state) {
                    show_main_window(tray.app_handle());
                }
            }
        })
        .build(app)?;

    Ok(())
}

fn fallback_tray_icon() -> Image<'static> {
    const SIZE: usize = 32;
    let mut rgba = vec![0_u8; SIZE * SIZE * 4];
    for y in 0..SIZE {
        for x in 0..SIZE {
            let index = (y * SIZE + x) * 4;
            let center = SIZE as isize / 2;
            let dx = x as isize - center;
            let dy = y as isize - center;
            let radius_squared = dx * dx + dy * dy;
            let (red, green, blue, alpha) = if radius_squared <= 14 * 14 {
                if (7 * 7..=11 * 11).contains(&radius_squared) {
                    (244, 244, 247, 255)
                } else {
                    (109, 94, 252, 255)
                }
            } else {
                (0, 0, 0, 0)
            };
            rgba[index..index + 4].copy_from_slice(&[red, green, blue, alpha]);
        }
    }
    Image::new_owned(rgba, SIZE as u32, SIZE as u32)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_accepts_owned_tray_menu_actions() {
        assert_eq!(
            menu_action(SHOW_MAIN_WINDOW_ID),
            Some(TrayMenuAction::ShowMainWindow)
        );
        assert_eq!(
            menu_action(EXIT_APPLICATION_ID),
            Some(TrayMenuAction::ExitApplication)
        );
        assert_eq!(menu_action("open_external_url"), None);
    }

    #[test]
    fn only_reopens_for_a_completed_left_click() {
        assert!(should_show_from_click(
            MouseButton::Left,
            MouseButtonState::Up
        ));
        assert!(!should_show_from_click(
            MouseButton::Left,
            MouseButtonState::Down
        ));
        assert!(!should_show_from_click(
            MouseButton::Right,
            MouseButtonState::Up
        ));
    }

    #[test]
    fn fallback_icon_has_the_expected_rgba_shape() {
        let icon = fallback_tray_icon();
        assert_eq!(icon.width(), 32);
        assert_eq!(icon.height(), 32);
        assert_eq!(icon.rgba().len(), 32 * 32 * 4);
        assert!(icon.rgba().chunks_exact(4).any(|pixel| pixel[3] == 255));
        assert!(icon.rgba().chunks_exact(4).any(|pixel| pixel[3] == 0));
    }

    #[test]
    fn tray_availability_starts_disabled_and_can_be_published() {
        let state = DesktopTrayState::default();
        assert!(!state.is_available());
        assert!(!state.should_hide_on_close());
        state.mark_available();
        assert!(state.is_available());
        assert!(state.should_hide_on_close());
    }

    #[test]
    fn close_to_tray_can_be_previewed_without_removing_the_tray() {
        let state = DesktopTrayState::default();
        state.mark_available();
        state.set_close_to_tray_enabled(false);
        assert!(state.is_available());
        assert!(!state.should_hide_on_close());
        state.set_close_to_tray_enabled(true);
        assert!(state.should_hide_on_close());
    }

    #[test]
    fn capability_snapshot_distinguishes_runtime_and_planned_integrations() {
        let state = DesktopTrayState::default();
        let before = state.capabilities();
        assert_eq!(before.tray, DesktopCapabilityAvailability::Unavailable);
        assert_eq!(
            before.native_notifications,
            DesktopCapabilityAvailability::NotImplemented
        );
        assert_eq!(
            before.native_file_dialogs,
            DesktopCapabilityAvailability::NotImplemented
        );

        state.mark_available();
        assert_eq!(
            state.capabilities().tray,
            DesktopCapabilityAvailability::Available
        );
    }
}
