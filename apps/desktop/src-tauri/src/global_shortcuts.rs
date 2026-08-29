use std::sync::Mutex;

use serde::Serialize;
use tauri::{AppHandle, Emitter, Manager, Runtime, State};
use tauri_plugin_global_shortcut::{
    Code, GlobalShortcutExt, Modifiers, Shortcut, ShortcutEvent, ShortcutState,
};

pub const GLOBAL_SHORTCUT_EVENT: &str = "desktop-global-shortcut";

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
enum ShortcutAction {
    CommandPalette,
    NewTask,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum ShortcutRegistration {
    Registered,
    Unavailable,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DesktopShortcutStatus {
    command_palette: ShortcutRegistration,
    new_task: ShortcutRegistration,
}

impl Default for DesktopShortcutStatus {
    fn default() -> Self {
        Self {
            command_palette: ShortcutRegistration::Unavailable,
            new_task: ShortcutRegistration::Unavailable,
        }
    }
}

pub struct DesktopShortcutRegistry {
    status: Mutex<DesktopShortcutStatus>,
}

impl Default for DesktopShortcutRegistry {
    fn default() -> Self {
        Self {
            status: Mutex::new(DesktopShortcutStatus::default()),
        }
    }
}

impl DesktopShortcutRegistry {
    fn set(&self, action: ShortcutAction, registration: ShortcutRegistration) {
        let mut status = self
            .status
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        match action {
            ShortcutAction::CommandPalette => status.command_palette = registration,
            ShortcutAction::NewTask => status.new_task = registration,
        }
    }

    fn snapshot(&self) -> DesktopShortcutStatus {
        *self
            .status
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
    }
}

#[tauri::command]
pub fn desktop_shortcut_status(
    registry: State<'_, DesktopShortcutRegistry>,
) -> DesktopShortcutStatus {
    registry.snapshot()
}

pub fn register_global_shortcuts<R: Runtime>(app: &AppHandle<R>) {
    let registry = app.state::<DesktopShortcutRegistry>();
    for (action, shortcut) in shortcut_definitions() {
        let registration = if app.global_shortcut().register(shortcut).is_ok() {
            ShortcutRegistration::Registered
        } else {
            ShortcutRegistration::Unavailable
        };
        registry.set(action, registration);
    }
}

pub fn handle_global_shortcut<R: Runtime>(
    app: &AppHandle<R>,
    shortcut: &Shortcut,
    event: ShortcutEvent,
) {
    if event.state() != ShortcutState::Pressed {
        return;
    }
    let Some(action) = shortcut_action(shortcut) else {
        return;
    };
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
    let _ = app.emit_to("main", GLOBAL_SHORTCUT_EVENT, action);
}

fn shortcut_definitions() -> [(ShortcutAction, Shortcut); 2] {
    [
        (ShortcutAction::CommandPalette, command_palette_shortcut()),
        (ShortcutAction::NewTask, new_task_shortcut()),
    ]
}

fn shortcut_action(shortcut: &Shortcut) -> Option<ShortcutAction> {
    if shortcut == &command_palette_shortcut() {
        return Some(ShortcutAction::CommandPalette);
    }
    if shortcut == &new_task_shortcut() {
        return Some(ShortcutAction::NewTask);
    }
    None
}

fn command_palette_shortcut() -> Shortcut {
    Shortcut::new(Some(primary_modifier() | Modifiers::SHIFT), Code::KeyK)
}

fn new_task_shortcut() -> Shortcut {
    Shortcut::new(Some(primary_modifier() | Modifiers::SHIFT), Code::KeyN)
}

#[cfg(target_os = "macos")]
fn primary_modifier() -> Modifiers {
    Modifiers::SUPER
}

#[cfg(not(target_os = "macos"))]
fn primary_modifier() -> Modifiers {
    Modifiers::CONTROL
}

#[cfg(test)]
mod tests {
    use super::{
        DesktopShortcutRegistry, ShortcutAction, ShortcutRegistration, shortcut_action,
        shortcut_definitions,
    };
    use tauri_plugin_global_shortcut::{Code, Modifiers, Shortcut};

    #[test]
    fn exposes_two_bounded_global_shortcuts() {
        let definitions = shortcut_definitions();
        assert_eq!(definitions.len(), 2);
        assert_eq!(definitions[0].0, ShortcutAction::CommandPalette);
        assert_eq!(definitions[1].0, ShortcutAction::NewTask);
    }

    #[test]
    fn rejects_unregistered_shortcut_events() {
        let shortcut = Shortcut::new(Some(Modifiers::ALT), Code::KeyK);
        assert_eq!(shortcut_action(&shortcut), None);
    }

    #[test]
    fn reports_registration_without_exposing_platform_errors() {
        let registry = DesktopShortcutRegistry::default();
        registry.set(
            ShortcutAction::CommandPalette,
            ShortcutRegistration::Registered,
        );
        let status = registry.snapshot();
        assert_eq!(status.command_palette, ShortcutRegistration::Registered);
        assert_eq!(status.new_task, ShortcutRegistration::Unavailable);
    }
}
