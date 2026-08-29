mod desktop_log;
mod global_shortcuts;
mod sidecar;

use desktop_log::{DesktopEvent, DesktopLogger};
use global_shortcuts::{
    DesktopShortcutRegistry, desktop_shortcut_status, handle_global_shortcut,
    register_global_shortcuts,
};
use sidecar::{
    SidecarManager, initialize_sidecar, open_log_directory, restart_application, sidecar_status,
};
use tauri::Manager;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let sidecar = SidecarManager::new();
    let setup_sidecar = sidecar.clone();

    let app = tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            if let Some(window) = app.get_webview_window("main") {
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
        .plugin(tauri_plugin_shell::init())
        .manage(sidecar)
        .manage(DesktopShortcutRegistry::default())
        .invoke_handler(tauri::generate_handler![
            sidecar_status,
            restart_application,
            open_log_directory,
            desktop_shortcut_status
        ])
        .setup(move |app| {
            let logger = app
                .path()
                .app_log_dir()
                .map(|directory| DesktopLogger::open(&directory))
                .unwrap_or_else(|_| DesktopLogger::stderr_only());
            logger.event(DesktopEvent::AppSetupStarted);
            setup_sidecar.attach_logger(logger.clone());
            register_global_shortcuts(app.handle());
            initialize_sidecar(app.handle(), setup_sidecar.clone());
            logger.event(DesktopEvent::AppSetupCompleted);
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("failed to build opc-workspace desktop application");

    app.run(|app_handle, event| {
        if matches!(
            event,
            tauri::RunEvent::ExitRequested { .. } | tauri::RunEvent::Exit
        ) {
            let manager = app_handle.state::<SidecarManager>();
            manager.log_event(DesktopEvent::ApplicationExitRequested);
            manager.shutdown();
        }
    });
}
