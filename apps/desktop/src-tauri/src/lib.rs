mod sidecar;

use sidecar::{SidecarManager, initialize_sidecar, sidecar_status};
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
        .invoke_handler(tauri::generate_handler![sidecar_status])
        .setup(move |app| {
            initialize_sidecar(app.handle(), setup_sidecar.clone());
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("failed to build opc-workspace desktop application");

    app.run(|app_handle, event| {
        if matches!(
            event,
            tauri::RunEvent::ExitRequested { .. } | tauri::RunEvent::Exit
        ) {
            app_handle.state::<SidecarManager>().shutdown();
        }
    });
}
