mod desktop_log;
mod sidecar;

use desktop_log::{DesktopEvent, DesktopLogger};
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
        .plugin(tauri_plugin_shell::init())
        .manage(sidecar)
        .invoke_handler(tauri::generate_handler![
            sidecar_status,
            restart_application,
            open_log_directory
        ])
        .setup(move |app| {
            let logger = app
                .path()
                .app_log_dir()
                .map(|directory| DesktopLogger::open(&directory))
                .unwrap_or_else(|_| DesktopLogger::stderr_only());
            logger.event(DesktopEvent::AppSetupStarted);
            setup_sidecar.attach_logger(logger.clone());
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
