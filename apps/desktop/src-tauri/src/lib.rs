mod desktop_log;
mod desktop_tray;
mod developer_workspace;
mod embedded_browser;
mod external_browser;
#[cfg(windows)]
mod file_helper_image;
#[cfg(windows)]
mod file_helper_transport;
mod file_operation_contract;
#[cfg(windows)]
mod file_operation_disk;
#[cfg(windows)]
#[path = "workspace_file_snapshot/replacement_metadata.rs"]
mod file_operation_metadata;
#[cfg(windows)]
#[path = "workspace_file_snapshot/windows.rs"]
mod file_operation_read;
mod file_operation_review;
#[cfg(windows)]
#[path = "workspace_file_snapshot/replacement_security.rs"]
mod file_operation_security;
#[cfg(windows)]
mod file_recovery_record;
mod global_shortcuts;
mod host_shell;
mod sidecar;
mod startup_restore;
mod workspace_file_snapshot;
mod workspace_terminal;

use desktop_log::{DesktopEvent, DesktopLogger};
use desktop_tray::{
    DesktopTrayState, desktop_capabilities, hide_main_window_to_tray, install_desktop_tray,
    set_close_to_tray_enabled,
};
use developer_workspace::{
    WorkspaceState, workspace_choose_root, workspace_git_review, workspace_list, workspace_preview,
};
use embedded_browser::{
    BrowserState, browser_action, browser_activate_tab, browser_capture_page_text,
    browser_close_tab, browser_create_tab, browser_navigate, browser_set_layout, browser_snapshot,
};
use external_browser::{close_external_browser, open_external_browser};
use global_shortcuts::{
    DesktopShortcutRegistry, desktop_shortcut_status, handle_global_shortcut,
    register_global_shortcuts,
};
use sidecar::{
    SidecarManager, initialize_sidecar, open_log_directory, restart_application, sidecar_status,
};
use startup_restore::{list_startup_restore_choices, schedule_startup_restore};
use tauri::Manager;
use workspace_file_snapshot::{
    FileEditState, FileRecoveryReviewState, FileSnapshotState, workspace_file_edit_apply,
    workspace_file_operation_cancel, workspace_file_recovery_apply, workspace_file_recovery_list,
    workspace_file_recovery_preview, workspace_file_replacement_apply,
    workspace_file_replacement_preview, workspace_file_snapshot, workspace_file_snapshot_release,
    workspace_file_snapshot_validate, workspace_file_write_capability,
};
use workspace_file_snapshot::{
    workspace_file_replacement_review, workspace_file_replacement_review_release,
};
use workspace_terminal::{
    TerminalState, workspace_terminal_close, workspace_terminal_read, workspace_terminal_resize,
    workspace_terminal_start, workspace_terminal_write,
};

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let sidecar = SidecarManager::new();
    let setup_sidecar = sidecar.clone();

    let app = tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            if let Some(window) = app.get_window("main") {
                let _ = window.show();
                let _ = window.unminimize();
                let _ = window.set_focus();
            }
        }))
        .plugin(
            tauri_plugin_global_shortcut::Builder::new()
                .with_handler(handle_global_shortcut)
                .build(),
        )
        .plugin(host_shell::init())
        .manage(sidecar)
        .manage(DesktopTrayState::default())
        .manage(DesktopShortcutRegistry::default())
        .manage(BrowserState::default())
        .manage(WorkspaceState::default())
        .manage(FileSnapshotState::default())
        .manage(FileEditState::default())
        .manage(FileRecoveryReviewState::default())
        .manage(TerminalState::default())
        .invoke_handler(|invoke| {
            if !embedded_browser::trusted_command_caller(invoke.message.webview_ref().label()) {
                invoke
                    .resolver
                    .reject("Only the trusted workspace view may call application commands");
                return true;
            }
            let handler: fn(tauri::ipc::Invoke) -> bool = tauri::generate_handler![
                sidecar_status,
                restart_application,
                open_log_directory,
                list_startup_restore_choices,
                schedule_startup_restore,
                desktop_shortcut_status,
                desktop_capabilities,
                set_close_to_tray_enabled,
                open_external_browser,
                close_external_browser,
                browser_snapshot,
                browser_create_tab,
                browser_activate_tab,
                browser_close_tab,
                browser_navigate,
                browser_action,
                browser_capture_page_text,
                browser_set_layout,
                workspace_choose_root,
                workspace_list,
                workspace_preview,
                workspace_file_snapshot,
                workspace_file_snapshot_validate,
                workspace_file_snapshot_release,
                workspace_file_write_capability,
                workspace_file_edit_apply,
                workspace_file_operation_cancel,
                workspace_file_recovery_list,
                workspace_file_recovery_preview,
                workspace_file_replacement_preview,
                workspace_file_replacement_review,
                workspace_file_replacement_review_release,
                workspace_file_replacement_apply,
                workspace_file_recovery_apply,
                workspace_git_review,
                workspace_terminal_start,
                workspace_terminal_read,
                workspace_terminal_write,
                workspace_terminal_resize,
                workspace_terminal_close
            ];
            handler(invoke)
        })
        .setup(move |app| {
            let logger = app
                .path()
                .app_log_dir()
                .map(|directory| DesktopLogger::open(&directory))
                .unwrap_or_else(|_| DesktopLogger::stderr_only());
            logger.event(DesktopEvent::AppSetupStarted);
            setup_sidecar.attach_logger(logger.clone());
            install_desktop_tray(app);
            register_global_shortcuts(app.handle());
            initialize_sidecar(app.handle(), setup_sidecar.clone());
            logger.event(DesktopEvent::AppSetupCompleted);
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("failed to build opc-workspace desktop application");

    app.run(|app_handle, event| {
        if let tauri::RunEvent::WindowEvent {
            label,
            event: tauri::WindowEvent::CloseRequested { api, .. },
            ..
        } = &event
        {
            if label == "main" && hide_main_window_to_tray(app_handle) {
                api.prevent_close();
            }
        }
        if matches!(
            event,
            tauri::RunEvent::ExitRequested { .. } | tauri::RunEvent::Exit
        ) {
            let manager = app_handle.state::<SidecarManager>();
            app_handle.state::<TerminalState>().shutdown();
            manager.log_event(DesktopEvent::ApplicationExitRequested);
            manager.shutdown();
        }
    });
}
