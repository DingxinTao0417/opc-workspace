//! Keep native sidecar process management without the shell plugin's global
//! JavaScript link interceptor. That interceptor cancels every `_blank` link,
//! including in untrusted browser tabs where shell IPC is intentionally denied.

use tauri::{
    AppHandle, RunEvent, Runtime, Url, Webview, Window, ipc::Invoke, plugin::Plugin,
    webview::PageLoadPayload,
};

struct HostShell<R: Runtime> {
    // Owning the original plugin also preserves its normal Drop implementation.
    inner: Box<dyn Plugin<R>>,
}

pub fn init<R: Runtime>() -> impl Plugin<R> {
    HostShell {
        inner: Box::new(tauri_plugin_shell::init::<R>()),
    }
}

impl<R: Runtime> Plugin<R> for HostShell<R> {
    fn name(&self) -> &'static str {
        self.inner.name()
    }

    fn initialize(
        &mut self,
        app: &AppHandle<R>,
        config: serde_json::Value,
    ) -> Result<(), Box<dyn std::error::Error>> {
        self.inner.initialize(app, config)
    }

    // Intentionally use the trait's None defaults for both initialization-script
    // hooks. No frontend consumes shell APIs; native ShellExt and exit cleanup
    // still come from the original plugin below, with the existing ACL unchanged.

    fn window_created(&mut self, window: Window<R>) {
        self.inner.window_created(window);
    }

    fn webview_created(&mut self, webview: Webview<R>) {
        self.inner.webview_created(webview);
    }

    fn on_navigation(&mut self, webview: &Webview<R>, url: &Url) -> bool {
        self.inner.on_navigation(webview, url)
    }

    fn on_page_load(&mut self, webview: &Webview<R>, payload: &PageLoadPayload<'_>) {
        self.inner.on_page_load(webview, payload);
    }

    fn on_event(&mut self, app: &AppHandle<R>, event: &RunEvent) {
        self.inner.on_event(app, event);
    }

    fn extend_api(&mut self, invoke: Invoke<R>) -> bool {
        self.inner.extend_api(invoke)
    }
}
