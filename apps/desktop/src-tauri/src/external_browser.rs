//! External browser window management (desktop only).
//!
//! The workspace overview's browser tab embeds loopback previews in an iframe
//! but delegates arbitrary external pages to a dedicated native webview window
//! so rendering is stable and never subject to third-party frame blocking.

use tauri::{command, AppHandle, Manager, WebviewWindowBuilder, WebviewUrl};

const EXTERNAL_BROWSER_LABEL: &str = "external-browser";

/// Parses and restricts the URL to http/https so the native window can never be
/// pointed at file:, javascript: or other privileged schemes.
fn parse_external_url(raw: &str) -> Result<tauri::Url, String> {
    let trimmed = raw.trim();
    if !(trimmed.starts_with("http://") || trimmed.starts_with("https://")) {
        return Err("external browser only accepts http or https URLs".to_string());
    }
    trimmed.parse::<tauri::Url>().map_err(|error| error.to_string())
}

#[command]
pub fn open_external_browser(app: AppHandle, url: String) -> Result<(), String> {
    let parsed = parse_external_url(&url)?;
    if let Some(window) = app.get_webview_window(EXTERNAL_BROWSER_LABEL) {
        window.navigate(parsed).map_err(|error| error.to_string())?;
        window.set_focus().map_err(|error| error.to_string())?;
        return Ok(());
    }
    let host = parsed.host_str().unwrap_or("external").to_string();
    WebviewWindowBuilder::new(
        &app,
        EXTERNAL_BROWSER_LABEL,
        WebviewUrl::External(parsed),
    )
    .title(format!("opc-workspace 浏览器 - {host}"))
    .inner_size(1200.0, 800.0)
    .build()
    .map_err(|error| error.to_string())?;
    Ok(())
}

#[command]
pub fn close_external_browser(app: AppHandle) -> Result<(), String> {
    if let Some(window) = app.get_webview_window(EXTERNAL_BROWSER_LABEL) {
        window.close().map_err(|error| error.to_string())?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::parse_external_url;

    #[test]
    fn accepts_http_and_https_only() {
        assert!(parse_external_url("https://example.com/a").is_ok());
        assert!(parse_external_url("http://127.0.0.1:5173").is_ok());
        assert!(parse_external_url("file:///C:/secret").is_err());
        assert!(parse_external_url("javascript:alert(1)").is_err());
        assert!(parse_external_url("not a url").is_err());
    }
}
