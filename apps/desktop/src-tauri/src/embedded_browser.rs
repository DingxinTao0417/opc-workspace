//! Embedded browsing is a native, untrusted WebView2 surface. Its profile, IPC,
//! event targets and navigation state are separate from the workspace webview.

use std::sync::Mutex;

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Emitter, EventTarget, Manager};

pub const MAX_TABS: usize = 12;
const STATE_EVENT: &str = "browser-state-changed";
const NOTICE_EVENT: &str = "browser-notice";

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct BrowserNotice<'a> {
    tab_id: &'a str,
    message: &'a str,
}

#[derive(Debug, Clone, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BrowserTab {
    id: String,
    url: String,
    title: String,
    loading: bool,
    can_go_back: bool,
    can_go_forward: bool,
    error: Option<String>,
    #[serde(skip)]
    navigation_id: u64,
}

fn same_browser_document(expected: &BrowserTab, current: &BrowserTab) -> bool {
    expected.url == current.url && expected.navigation_id == current.navigation_id
}

#[derive(Debug, Clone, Default, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BrowserSnapshot {
    tabs: Vec<BrowserTab>,
    active_tab_id: Option<String>,
    revision: u64,
}

const MAX_PAGE_TEXT_BYTES: usize = 8 * 1024;
const MAX_PAGE_LINKS: usize = 20;
const MAX_PAGE_LINK_BYTES: usize = 4 * 1024;
const MAX_PAGE_LINK_LABEL_CHARS: usize = 160;
const MAX_PAGE_LINK_URL_BYTES: usize = 512;
const MAX_PAGE_CAPTURE_RESULT_BYTES: usize = (MAX_PAGE_TEXT_BYTES + MAX_PAGE_LINK_BYTES) * 6 + 4096;
// Keep maxBytes/maxScanned/maxLinks/maxLinkBytes synchronized with the Rust
// validation above; all returned page content remains untrusted.
const BROWSER_PAGE_TEXT_SCRIPT: &str = r#"(() => {
  const source = document.body?.innerText ?? "";
  const maxBytes = 8192;
  const maxScanned = 100000;
  let text = "";
  let bytes = 0;
  let scanned = 0;
  let truncated = false;
  for (const value of source) {
    if (scanned >= maxScanned) { truncated = true; break; }
    scanned += 1;
    const code = value.codePointAt(0) ?? 0;
    const character = code >= 0xd800 && code <= 0xdfff ? "�" : value;
    const length = code <= 0x7f ? 1 : code <= 0x7ff ? 2 : code <= 0xffff ? 3 : 4;
    if (bytes + length > maxBytes) { truncated = true; break; }
    text += character;
    bytes += length;
  }
  const links = [];
  const seen = new Set();
  const maxLinks = 20;
  const maxLinkBytes = 4096;
  const maxLinkScanned = 1000;
  const anchors = document.links;
  let linkBytes = 0;
  let linksTruncated = false;
  const anchorCount = Math.min(anchors.length, maxLinkScanned);
  for (let index = 0; index < anchorCount; index += 1) {
    const anchor = anchors[index];
    if (!anchor || anchor.getClientRects().length === 0) continue;
    let hidden = false;
    for (let node = anchor; node; node = node.parentElement) {
      const style = getComputedStyle(node);
      if (node.getAttribute("aria-hidden") === "true" || style.display === "none" ||
          style.visibility === "hidden" || Number(style.opacity) === 0) {
        hidden = true;
        break;
      }
    }
    if (hidden) continue;
    let parsed;
    try { parsed = new URL(anchor.href); } catch { continue; }
    if ((parsed.protocol !== "http:" && parsed.protocol !== "https:") ||
        !parsed.hostname || parsed.username || parsed.password) continue;
    parsed.search = "";
    parsed.hash = "";
    const url = parsed.href;
    if (new TextEncoder().encode(url).length > 512 || seen.has(url)) continue;
    const rawLabel = String(anchor.innerText || anchor.getAttribute("aria-label") || "");
    const label = Array.from(rawLabel.replace(/[\u0000-\u001f\u007f]/g, " ")
      .replace(/\s+/g, " ").trim()).slice(0, 160).join("");
    const link = { label, url };
    const size = new TextEncoder().encode(JSON.stringify(link)).length +
      (links.length === 0 ? 0 : 1);
    if (links.length >= maxLinks || linkBytes + size > maxLinkBytes) {
      linksTruncated = true;
      break;
    }
    seen.add(url);
    links.push(link);
    linkBytes += size;
  }
  if (anchors.length > maxLinkScanned) linksTruncated = true;
  return { text, truncated, links, linksTruncated };
})()"#;

#[derive(Debug, Clone, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BrowserPageLink {
    pub label: String,
    pub url: String,
}

#[derive(Debug, Clone, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BrowserPageTextCapture {
    pub tab_id: String,
    pub url: String,
    pub title: String,
    pub text: String,
    pub truncated: bool,
    pub links: Vec<BrowserPageLink>,
    pub links_truncated: bool,
}

#[derive(Debug)]
struct BrowserPageTextPayload {
    text: String,
    truncated: bool,
    links: Vec<BrowserPageLink>,
    links_truncated: bool,
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
#[serde(rename_all = "camelCase")]
struct BrowserPageTextResult {
    text: String,
    truncated: bool,
    links: Vec<BrowserPageLinkPayload>,
    links_truncated: bool,
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct BrowserPageLinkPayload {
    label: String,
    url: String,
}

fn parse_browser_page_text_result(raw: &str) -> Result<BrowserPageTextPayload, String> {
    if raw.len() > MAX_PAGE_CAPTURE_RESULT_BYTES {
        return Err("BROWSER_CAPTURE_FAILED: 网页文本结果超过安全上限".into());
    }
    let payload: BrowserPageTextResult = serde_json::from_str(raw)
        .map_err(|_| "BROWSER_CAPTURE_FAILED: 无法读取网页文本".to_string())?;
    if payload.text.len() > MAX_PAGE_TEXT_BYTES {
        return Err("BROWSER_CAPTURE_FAILED: 网页文本超过安全上限".into());
    }
    if payload.links.len() > MAX_PAGE_LINKS {
        return Err("BROWSER_CAPTURE_FAILED: 可见链接超过安全上限".into());
    }
    let mut links = Vec::with_capacity(payload.links.len());
    let mut seen_urls = std::collections::HashSet::new();
    let mut link_bytes = 0usize;
    for item in payload.links {
        let label = item
            .label
            .chars()
            .map(|character| {
                if character.is_control() {
                    ' '
                } else {
                    character
                }
            })
            .take(MAX_PAGE_LINK_LABEL_CHARS)
            .collect::<String>()
            .split_whitespace()
            .collect::<Vec<_>>()
            .join(" ");
        let Some(url) = sanitize_page_link_url(&item.url) else {
            return Err("BROWSER_CAPTURE_FAILED: 网页链接地址无效".into());
        };
        if !seen_urls.insert(url.clone()) {
            continue;
        }
        let link = BrowserPageLink { label, url };
        link_bytes = link_bytes.saturating_add(
            serde_json::to_vec(&link)
                .map_err(|_| "BROWSER_CAPTURE_FAILED: 网页链接无效")?
                .len()
                + usize::from(!links.is_empty()),
        );
        if link_bytes > MAX_PAGE_LINK_BYTES {
            return Err("BROWSER_CAPTURE_FAILED: 网页链接超过安全上限".into());
        }
        links.push(link);
    }
    if payload.text.trim().is_empty() && links.is_empty() {
        return Err("BROWSER_CAPTURE_EMPTY: 当前页面没有可分享的可见文本或链接".into());
    }
    Ok(BrowserPageTextPayload {
        text: payload.text,
        truncated: payload.truncated,
        links,
        links_truncated: payload.links_truncated,
    })
}

fn sanitize_page_link_url(raw: &str) -> Option<String> {
    let mut parsed = tauri::Url::parse(raw).ok()?;
    if !matches!(parsed.scheme(), "http" | "https")
        || parsed.host_str().is_none()
        || !parsed.username().is_empty()
        || parsed.password().is_some()
    {
        return None;
    }
    parsed.set_query(None);
    parsed.set_fragment(None);
    let url = parsed.to_string();
    (url.len() <= MAX_PAGE_LINK_URL_BYTES).then_some(url)
}

#[derive(Debug, Clone, Copy, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct BrowserBounds {
    x: f64,
    y: f64,
    width: f64,
    height: f64,
}

impl BrowserBounds {
    fn validate(self) -> Result<Self, String> {
        let values = [self.x, self.y, self.width, self.height];
        if values
            .iter()
            .any(|v| !v.is_finite() || *v < 0.0 || *v > 32_768.0)
            || self.width < 1.0
            || self.height < 1.0
        {
            return Err("浏览区域尺寸无效".into());
        }
        Ok(self)
    }

    // DOM coordinates are CSS pixels. WebView2's ZoomFactor and the window DPI
    // convert them to native physical pixels; clipping prevents covering chrome.
    fn physical(self, zoom: f64, dpi: f64, width: f64, height: f64) -> Option<[f64; 4]> {
        let scale = zoom * dpi;
        if !scale.is_finite() || scale <= 0.0 {
            return None;
        }
        let x = (self.x * scale).min(width);
        let y = (self.y * scale).min(height);
        let w = (self.width * scale).min((width - x).max(0.0));
        let h = (self.height * scale).min((height - y).max(0.0));
        (w >= 1.0 && h >= 1.0).then_some([x, y, w, h])
    }
}

#[derive(Debug, Clone, Copy, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum BrowserAction {
    Back,
    Forward,
    Reload,
    Stop,
}

#[derive(Default)]
struct BrowserInner {
    snapshot: BrowserSnapshot,
    bounds: Option<BrowserBounds>,
}

#[derive(Default)]
pub struct BrowserState {
    inner: Mutex<BrowserInner>,
    // Never hold `inner` while waiting for the UI thread: native events also
    // update it. Only commands serialize here, including concurrent popups.
    operations: Mutex<()>,
}

impl BrowserState {
    fn snapshot(&self) -> BrowserSnapshot {
        self.inner
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .snapshot
            .clone()
    }
}

pub fn trusted_command_caller(label: &str) -> bool {
    label == "main"
}

fn parse_url(raw: &str) -> Result<tauri::Url, String> {
    let raw = raw.trim();
    if raw.len() > 8192 || raw.chars().any(char::is_control) {
        return Err("BROWSER_URL: 网址无效或过长".into());
    }
    let parsed =
        tauri::Url::parse(raw).map_err(|_| "BROWSER_URL: 请输入完整的 http 或 https 网址")?;
    if !matches!(parsed.scheme(), "http" | "https")
        || parsed.host_str().is_none()
        || matches!(
            parsed.host_str(),
            Some("ipc.localhost" | "tauri.localhost" | "asset.localhost")
        )
        || !parsed.username().is_empty()
        || parsed.password().is_some()
    {
        return Err("BROWSER_URL: 浏览器只支持不含账号密码的 http 或 https 网址".into());
    }
    Ok(parsed)
}

fn same_protected_origin(url: &tauri::Url, protected: &tauri::Url) -> bool {
    let loopback = |url: &tauri::Url| {
        url.host_str().is_some_and(|host| {
            host == "localhost"
                || host.ends_with(".localhost")
                || host
                    .trim_matches(['[', ']'])
                    .parse::<std::net::IpAddr>()
                    .is_ok_and(|ip| ip.is_loopback())
        })
    };
    url.origin() == protected.origin()
        || (loopback(url)
            && loopback(protected)
            && url.port_or_known_default() == protected.port_or_known_default())
}

fn protected_url(app: &AppHandle, url: &tauri::Url) -> bool {
    if matches!(
        url.host_str(),
        Some("ipc.localhost" | "tauri.localhost" | "asset.localhost")
    ) {
        return true;
    }
    #[cfg(debug_assertions)]
    if app
        .config()
        .build
        .dev_url
        .as_ref()
        .is_some_and(|dev| same_protected_origin(url, dev))
    {
        return true;
    }
    crate::sidecar::sidecar_status(app.state())
        .base_url
        .and_then(|base| tauri::Url::parse(&base).ok())
        .is_some_and(|base| same_protected_origin(url, &base))
}

fn snapshot_mutation(
    app: &AppHandle,
    update: impl FnOnce(&mut BrowserSnapshot),
) -> BrowserSnapshot {
    let state = app.state::<BrowserState>();
    let result = {
        let mut inner = state.inner.lock().unwrap_or_else(|e| e.into_inner());
        let before = inner.snapshot.clone();
        update(&mut inner.snapshot);
        if before == inner.snapshot {
            return before;
        }
        inner.snapshot.revision += 1;
        inner.snapshot.clone()
    };
    // A window target also matches its children. Target the trusted webview.
    let _ = app.emit_to(EventTarget::webview("main"), STATE_EVENT, &result);
    result
}

fn update_tab(app: &AppHandle, id: &str, update: impl FnOnce(&mut BrowserTab)) {
    snapshot_mutation(app, |snapshot| {
        if let Some(tab) = snapshot.tabs.iter_mut().find(|tab| tab.id == id) {
            update(tab);
        }
    });
}

fn popup_notice(app: &AppHandle, id: &str, message: &str) {
    // A failed popup is not a failed navigation of its opener. Keeping it out
    // of BrowserTab.error ensures the successfully loaded page stays visible.
    let _ = app.emit_to(
        EventTarget::webview("main"),
        NOTICE_EVENT,
        BrowserNotice {
            tab_id: id,
            message,
        },
    );
}

fn remove_tab(snapshot: &mut BrowserSnapshot, id: &str) {
    let Some(index) = snapshot.tabs.iter().position(|tab| tab.id == id) else {
        return;
    };
    snapshot.tabs.remove(index);
    if snapshot.active_tab_id.as_deref() == Some(id) {
        snapshot.active_tab_id = snapshot
            .tabs
            .get(index.min(snapshot.tabs.len().saturating_sub(1)))
            .map(|tab| tab.id.clone());
    }
}

fn require_tab(app: &AppHandle, id: &str) -> Result<(), String> {
    if app
        .state::<BrowserState>()
        .snapshot()
        .tabs
        .iter()
        .any(|tab| tab.id == id)
    {
        Ok(())
    } else {
        Err("该浏览器标签已关闭".into())
    }
}

fn capture_target(app: &AppHandle, id: &str) -> Result<BrowserTab, String> {
    let snapshot = app.state::<BrowserState>().snapshot();
    if snapshot.active_tab_id.as_deref() != Some(id) {
        return Err("BROWSER_TARGET_CHANGED: 只能读取当前活动标签".into());
    }
    let tab = snapshot
        .tabs
        .into_iter()
        .find(|tab| tab.id == id)
        .ok_or_else(|| "BROWSER_TARGET_CHANGED: 浏览器标签已关闭".to_string())?;
    if tab.loading || tab.error.is_some() {
        return Err("BROWSER_PAGE_UNAVAILABLE: 请等网页加载完成后重试".into());
    }
    let url = parse_url(&tab.url)?;
    if protected_url(app, &url) {
        return Err("BROWSER_URL: 不能读取应用内部页面".into());
    }
    Ok(tab)
}

async fn operation(
    app: AppHandle,
    execute: impl FnOnce(&AppHandle) -> Result<(), String> + Send + 'static,
) -> Result<BrowserSnapshot, String> {
    #[cfg(not(windows))]
    {
        let _ = (app, execute);
        Err("BROWSER_UNSUPPORTED: 内嵌 Chromium 浏览器当前仅支持 Windows 桌面版".into())
    }
    #[cfg(windows)]
    tauri::async_runtime::spawn_blocking(move || {
        let state = app.state::<BrowserState>();
        let _guard = state.operations.lock().unwrap_or_else(|e| e.into_inner());
        execute(&app)?;
        Ok(state.snapshot())
    })
    .await
    .map_err(|_| "浏览器操作未能完成".to_string())?
}

#[tauri::command]
pub async fn browser_snapshot(app: AppHandle) -> Result<BrowserSnapshot, String> {
    operation(app, |_| Ok(())).await
}

#[tauri::command]
pub async fn browser_create_tab(
    app: AppHandle,
    url: Option<String>,
) -> Result<BrowserSnapshot, String> {
    operation(app, move |app| native::create_tab(app, url.as_deref())).await
}

#[tauri::command]
pub async fn browser_activate_tab(
    app: AppHandle,
    tab_id: String,
) -> Result<BrowserSnapshot, String> {
    operation(app, move |app| {
        require_tab(app, &tab_id)?;
        snapshot_mutation(app, |snapshot| snapshot.active_tab_id = Some(tab_id));
        native::apply_layout(app)
    })
    .await
}

#[tauri::command]
pub async fn browser_close_tab(app: AppHandle, tab_id: String) -> Result<BrowserSnapshot, String> {
    operation(app, move |app| {
        require_tab(app, &tab_id)?;
        if let Some(view) = app.get_webview(&tab_id) {
            view.close().map_err(|_| "无法关闭浏览器标签")?;
        }
        snapshot_mutation(app, |snapshot| remove_tab(snapshot, &tab_id));
        native::apply_layout(app)
    })
    .await
}

#[tauri::command]
pub async fn browser_navigate(
    app: AppHandle,
    tab_id: String,
    url: String,
) -> Result<BrowserSnapshot, String> {
    operation(app, move |app| {
        require_tab(app, &tab_id)?;
        let url = parse_url(&url)?;
        native::navigate(app, &tab_id, url)
    })
    .await
}

#[tauri::command]
pub async fn browser_action(
    app: AppHandle,
    tab_id: String,
    action: BrowserAction,
) -> Result<BrowserSnapshot, String> {
    operation(app, move |app| {
        require_tab(app, &tab_id)?;
        native::action(app, &tab_id, action)
    })
    .await
}

#[tauri::command]
pub async fn browser_capture_page_text(
    app: AppHandle,
    tab_id: String,
) -> Result<BrowserPageTextCapture, String> {
    #[cfg(not(windows))]
    {
        let _ = (app, tab_id);
        Err("BROWSER_UNSUPPORTED: 网页文本捕获仅支持 Windows 桌面版".into())
    }
    #[cfg(windows)]
    tauri::async_runtime::spawn_blocking(move || {
        let state = app.state::<BrowserState>();
        let _guard = state.operations.lock().unwrap_or_else(|e| e.into_inner());
        let target = capture_target(&app, &tab_id)?;
        native::capture_page_text(&app, &tab_id, target)
    })
    .await
    .map_err(|_| "网页文本捕获未能完成".to_string())?
}

#[tauri::command]
pub async fn browser_set_layout(
    app: AppHandle,
    bounds: Option<BrowserBounds>,
) -> Result<(), String> {
    let bounds = bounds.map(BrowserBounds::validate).transpose()?;
    app.state::<BrowserState>()
        .inner
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .bounds = bounds;
    // Modal/route hiding must not queue behind a slow tab creation. Dispatch
    // hide immediately, while the serialized layout pass converges afterwards.
    if bounds.is_none() {
        for tab in app.state::<BrowserState>().snapshot().tabs {
            if let Some(view) = app.get_webview(&tab.id) {
                let _ = view.hide();
            }
        }
    }
    operation(app, move |app| native::apply_layout(app))
        .await
        .map(|_| ())
}

#[cfg(windows)]
mod native {
    use super::*;
    use std::{sync::mpsc, time::Duration};
    use tauri::{
        PhysicalPosition, PhysicalSize, Rect, Webview, WebviewUrl, webview::WebviewBuilder,
    };
    use webview2_com::Microsoft::Web::WebView2::Win32::{
        COREWEBVIEW2_WEB_ERROR_STATUS, COREWEBVIEW2_WEB_ERROR_STATUS_OPERATION_CANCELED,
        COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL, COREWEBVIEW2_WEB_RESOURCE_REQUEST_SOURCE_KINDS_ALL,
        ICoreWebView2, ICoreWebView2_22,
    };
    use webview2_com::{
        CoTaskMemPWSTR, DocumentTitleChangedEventHandler, ExecuteScriptCompletedHandler,
        HistoryChangedEventHandler, NavigationCompletedEventHandler,
        NavigationStartingEventHandler, NewWindowRequestedEventHandler, ProcessFailedEventHandler,
        SourceChangedEventHandler, WebResourceRequestedEventHandler,
    };
    use windows::core::{BOOL, Interface, PWSTR, w};

    fn view(app: &AppHandle, id: &str) -> Result<Webview, String> {
        app.get_webview(id)
            .ok_or_else(|| "浏览器视图不可用，请关闭标签后重试".into())
    }

    fn on_native<T: Send + 'static>(
        view: &Webview,
        action: impl FnOnce(tauri::webview::PlatformWebview) -> Result<T, String> + Send + 'static,
    ) -> Result<T, String> {
        let (sender, receiver) = mpsc::sync_channel(1);
        view.with_webview(move |native| {
            let _ = sender.send(action(native));
        })
        .map_err(|_| "无法访问内嵌浏览器")?;
        receiver
            .recv_timeout(Duration::from_secs(10))
            .map_err(|_| "BROWSER_TIMEOUT: 浏览器操作超时，请重试".to_string())?
    }

    unsafe fn read_string(
        getter: impl FnOnce(*mut PWSTR) -> windows::core::Result<()>,
    ) -> Option<String> {
        let mut value = PWSTR::null();
        let result = getter(&mut value);
        let owned = CoTaskMemPWSTR::from(value);
        result.ok().map(|_| owned.to_string())
    }

    fn refresh(app: &AppHandle, id: &str, core: &ICoreWebView2) {
        // COM strings are owned by CoTaskMemPWSTR and freed after copying.
        unsafe {
            let url = read_string(|value| core.Source(value));
            let title = read_string(|value| core.DocumentTitle(value));
            let mut back = BOOL::default();
            let mut forward = BOOL::default();
            let _ = core.CanGoBack(&mut back);
            let _ = core.CanGoForward(&mut forward);
            update_tab(app, id, |tab| {
                if let Some(url) = url {
                    tab.url = url;
                }
                if let Some(title) = title {
                    tab.title = if title.is_empty() || title == "about:blank" {
                        "新标签页".into()
                    } else {
                        title.chars().take(512).collect()
                    };
                }
                tab.can_go_back = back.as_bool();
                tab.can_go_forward = forward.as_bool();
            });
        }
    }

    fn install_events(app: &AppHandle, id: &str, child: &Webview) -> Result<(), String> {
        let app = app.clone();
        let id = id.to_owned();
        on_native(child, move |native| unsafe {
            let core = native
                .controller()
                .CoreWebView2()
                .map_err(|_| "无法初始化 WebView2")?;
            // Even an external page navigating onto an app origin must not get
            // access to Tauri commands or postMessage bridge.
            core.Settings()
                .and_then(|settings| settings.SetIsWebMessageEnabled(false))
                .map_err(|_| "无法隔离浏览器权限")?;
            let mut token = 0;
            let (event_app, event_id) = (app.clone(), id.clone());
            core.add_NavigationStarting(
                &NavigationStartingEventHandler::create(Box::new(move |_, args| {
                    let Some(args) = args else {
                        return Ok(());
                    };
                    let mut cancelled = BOOL::default();
                    args.Cancel(&mut cancelled)?;
                    if cancelled.as_bool() {
                        return Ok(());
                    }
                    let mut navigation_id = 0;
                    args.NavigationId(&mut navigation_id)?;
                    update_tab(&event_app, &event_id, |tab| {
                        tab.navigation_id = navigation_id;
                        tab.loading = true;
                        tab.error = None;
                    });
                    Ok(())
                })),
                &mut token,
            )
            .map_err(|_| "无法监听网页导航")?;
            let (event_app, event_id) = (app.clone(), id.clone());
            core.add_NavigationCompleted(
                &NavigationCompletedEventHandler::create(Box::new(move |sender, args| {
                    let mut success = BOOL::default();
                    let mut status = COREWEBVIEW2_WEB_ERROR_STATUS::default();
                    let mut navigation_id = 0;
                    if let Some(args) = args {
                        args.IsSuccess(&mut success)?;
                        args.WebErrorStatus(&mut status)?;
                        args.NavigationId(&mut navigation_id)?;
                    }
                    update_tab(&event_app, &event_id, |tab| {
                        if tab.navigation_id != navigation_id {
                            return;
                        }
                        tab.loading = false;
                        if status != COREWEBVIEW2_WEB_ERROR_STATUS_OPERATION_CANCELED {
                            tab.error = (!success.as_bool()).then(|| {
                                format!("网页加载失败（WebView2 {}），请检查网络或重试", status.0)
                            });
                        }
                    });
                    if let Some(sender) = sender {
                        refresh(&event_app, &event_id, &sender);
                    }
                    Ok(())
                })),
                &mut token,
            )
            .map_err(|_| "无法监听网页加载结果")?;
            let (event_app, event_id) = (app.clone(), id.clone());
            core.add_HistoryChanged(
                &HistoryChangedEventHandler::create(Box::new(move |sender, _| {
                    if let Some(sender) = sender {
                        refresh(&event_app, &event_id, &sender);
                    }
                    Ok(())
                })),
                &mut token,
            )
            .map_err(|_| "无法监听浏览历史")?;
            let (event_app, event_id) = (app.clone(), id.clone());
            core.add_SourceChanged(
                &SourceChangedEventHandler::create(Box::new(move |sender, _| {
                    if let Some(sender) = sender {
                        refresh(&event_app, &event_id, &sender);
                    }
                    Ok(())
                })),
                &mut token,
            )
            .map_err(|_| "无法监听页面网址")?;
            let (event_app, event_id) = (app.clone(), id.clone());
            core.add_DocumentTitleChanged(
                &DocumentTitleChangedEventHandler::create(Box::new(move |sender, _| {
                    if let Some(sender) = sender {
                        refresh(&event_app, &event_id, &sender);
                    }
                    Ok(())
                })),
                &mut token,
            )
            .map_err(|_| "无法监听页面标题")?;
            let (event_app, event_id) = (app.clone(), id.clone());
            core.add_ProcessFailed(
                &ProcessFailedEventHandler::create(Box::new(move |_, _| {
                    update_tab(&event_app, &event_id, |tab| {
                        tab.loading = false;
                        tab.error = Some("浏览器进程异常，请刷新或重新打开标签".into());
                    });
                    Ok(())
                })),
                &mut token,
            )
            .map_err(|_| "无法监听浏览器状态")?;
            let (popup_app, popup_id) = (app.clone(), id.clone());
            core.add_NewWindowRequested(
                &NewWindowRequestedEventHandler::create(Box::new(move |_, args| {
                    let Some(args) = args else {
                        return Ok(());
                    };
                    // Handle the COM event directly, without introducing a
                    // separate HWND dispatcher for the browser's own routing.
                    // No native popup escapes the managed tab workspace.
                    args.SetHandled(true)?;
                    let Some(url) = read_string(|value| args.Uri(value)) else {
                        popup_notice(&popup_app, &popup_id, "BROWSER_URL: 无法读取新标签网址");
                        return Ok(());
                    };
                    let app = popup_app.clone();
                    let id = popup_id.clone();
                    // Only owned strings/AppHandle cross threads. In particular,
                    // no COM args/deferral are captured by the blocking worker.
                    tauri::async_runtime::spawn_blocking(move || {
                        let state = app.state::<BrowserState>();
                        let _guard = state.operations.lock().unwrap_or_else(|e| e.into_inner());
                        if require_tab(&app, &id).is_err() {
                            return;
                        }
                        if let Err(error) = create_tab(&app, Some(&url)) {
                            popup_notice(&app, &id, &error);
                        }
                    });
                    Ok(())
                })),
                &mut token,
            )
            .map_err(|_| "无法监听新标签请求")?;
            // Prevent untrusted pages (including subframes/fetch) from reading
            // the development bundle or reaching the privileged local API.
            core.cast::<ICoreWebView2_22>()
                .map_err(|_| "BROWSER_RUNTIME: 请升级 WebView2 Runtime 后使用内嵌浏览器")?
                .AddWebResourceRequestedFilterWithRequestSourceKinds(
                    w!("*"),
                    COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL,
                    COREWEBVIEW2_WEB_RESOURCE_REQUEST_SOURCE_KINDS_ALL,
                )
                .map_err(|_| "无法隔离应用服务")?;
            let resource_app = app.clone();
            let environment = native.environment();
            core.add_WebResourceRequested(
                &WebResourceRequestedEventHandler::create(Box::new(move |_, args| {
                    let Some(args) = args else {
                        return Ok(());
                    };
                    let request = args.Request()?;
                    let uri = read_string(|value| request.Uri(value));
                    if uri
                        .and_then(|uri| tauri::Url::parse(&uri).ok())
                        .is_some_and(|url| protected_url(&resource_app, &url))
                    {
                        let response = environment.CreateWebResourceResponse(
                            None,
                            403,
                            w!("Forbidden"),
                            w!("Content-Type: text/plain\r\nCache-Control: no-store"),
                        )?;
                        args.SetResponse(&response)?;
                    }
                    Ok(())
                })),
                &mut token,
            )
            .map_err(|_| "无法隔离应用服务")?;
            refresh(&app, &id, &core);
            Ok(())
        })
    }

    pub fn create_tab(app: &AppHandle, raw: Option<&str>) -> Result<(), String> {
        let url = raw
            .filter(|raw| !raw.trim().is_empty() && raw.trim() != "about:blank")
            .map(parse_url)
            .transpose()?;
        if url.as_ref().is_some_and(|url| protected_url(app, url)) {
            return Err("BROWSER_URL: 应用内部地址不能在浏览器标签中打开".into());
        }
        if app.state::<BrowserState>().snapshot().tabs.len() >= MAX_TABS {
            return Err(format!(
                "BROWSER_TAB_LIMIT: 最多同时打开 {MAX_TABS} 个浏览器标签，请先关闭一个"
            ));
        }
        let window = app.get_window("main").ok_or("主窗口不可用")?;
        #[cfg(debug_assertions)]
        let profile = std::env::var_os("OPC_BROWSER_DATA_DIR")
            .map(std::path::PathBuf::from)
            .unwrap_or_else(|| {
                std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"))
                    .join("../../../.local/browser-profile")
            });
        #[cfg(not(debug_assertions))]
        let profile = app
            .path()
            .app_local_data_dir()
            .map_err(|_| "无法定位浏览器数据目录")?
            .join("browser-profile");
        std::fs::create_dir_all(&profile).map_err(|_| "无法创建浏览器数据目录")?;
        let id = format!("browser-{}", uuid::Uuid::new_v4());
        let nav_app = app.clone();
        let nav_id = id.clone();
        let builder =
            WebviewBuilder::new(&id, WebviewUrl::External("about:blank".parse().unwrap()))
                .data_directory(profile)
                .focused(false)
                .devtools(false)
                .on_navigation(move |url| {
                    let allowed = url.as_str() == "about:blank"
                        || (parse_url(url.as_str()).is_ok() && !protected_url(&nav_app, url));
                    if !allowed {
                        update_tab(&nav_app, &nav_id, |tab| {
                            tab.loading = false;
                            tab.error = Some("已阻止不支持的网址协议".into());
                        });
                    }
                    allowed
                });
        snapshot_mutation(app, |snapshot| {
            snapshot.tabs.push(BrowserTab {
                id: id.clone(),
                url: "about:blank".into(),
                title: "新标签页".into(),
                loading: false,
                can_go_back: false,
                can_go_forward: false,
                error: None,
                navigation_id: 0,
            });
        });
        let setup = (|| {
            let child = window
                .add_child(
                    builder,
                    PhysicalPosition::new(0, 0),
                    PhysicalSize::new(1, 1),
                )
                .map_err(|_| "无法创建 Chromium 浏览器，请确认 WebView2 Runtime 已安装")?;
            child.hide().map_err(|_| "无法初始化浏览区域")?;
            install_events(app, &id, &child)?;
            if let Some(url) = url {
                navigate(app, &id, url)?;
            }
            snapshot_mutation(app, |snapshot| snapshot.active_tab_id = Some(id.clone()));
            apply_layout(app)
        })();
        if setup.is_err() {
            if let Some(child) = app.get_webview(&id) {
                let _ = child.close();
            }
            snapshot_mutation(app, |snapshot| remove_tab(snapshot, &id));
        }
        setup
    }

    pub fn navigate(app: &AppHandle, id: &str, url: tauri::Url) -> Result<(), String> {
        if protected_url(app, &url) {
            return Err("BROWSER_URL: 应用内部地址不能在浏览器标签中打开".into());
        }
        view(app, id)?
            .navigate(url)
            .map_err(|_| "无法打开网址".to_string())
    }

    pub fn action(app: &AppHandle, id: &str, action: BrowserAction) -> Result<(), String> {
        on_native(&view(app, id)?, move |native| unsafe {
            let core = native
                .controller()
                .CoreWebView2()
                .map_err(|_| "浏览器不可用")?;
            match action {
                BrowserAction::Back => core.GoBack(),
                BrowserAction::Forward => core.GoForward(),
                BrowserAction::Reload => core.Reload(),
                BrowserAction::Stop => core.Stop(),
            }
            .map_err(|_| "浏览器操作失败".into())
        })?;
        if matches!(action, BrowserAction::Stop) {
            update_tab(app, id, |tab| tab.loading = false);
        }
        Ok(())
    }

    pub fn capture_page_text(
        app: &AppHandle,
        id: &str,
        target: BrowserTab,
    ) -> Result<BrowserPageTextCapture, String> {
        let child = view(app, id)?;
        let expected_url = target.url.clone();
        let (sender, receiver) = mpsc::sync_channel(1);
        on_native(&child, move |native| unsafe {
            let core = native
                .controller()
                .CoreWebView2()
                .map_err(|_| "浏览器不可用")?;
            let current = read_string(|value| core.Source(value))
                .ok_or("BROWSER_PAGE_CHANGED: 网页地址已变化")?;
            if current != expected_url {
                return Err("BROWSER_PAGE_CHANGED: 网页地址已变化，请重试".into());
            }
            let script = CoTaskMemPWSTR::from(BROWSER_PAGE_TEXT_SCRIPT);
            let handler = ExecuteScriptCompletedHandler::create(Box::new(move |status, result| {
                let payload = if status.is_ok() {
                    parse_browser_page_text_result(&result)
                } else {
                    Err("BROWSER_CAPTURE_FAILED: 网页文本读取失败".into())
                };
                let _ = sender.send(payload);
                Ok(())
            }));
            core.ExecuteScript(*script.as_ref().as_pcwstr(), &handler)
                .map_err(|_| "BROWSER_CAPTURE_FAILED: 无法读取网页文本")?;
            Ok(())
        })?;

        let payload = receiver
            .recv_timeout(Duration::from_secs(8))
            .map_err(|_| "BROWSER_CAPTURE_TIMEOUT: 网页文本读取超时，请重试".to_string())??;
        let child = view(app, id)?;
        let expected_url = target.url.clone();
        let title = on_native(&child, move |native| unsafe {
            let core = native
                .controller()
                .CoreWebView2()
                .map_err(|_| "浏览器不可用")?;
            let current = read_string(|value| core.Source(value))
                .ok_or("BROWSER_PAGE_CHANGED: 网页地址已变化")?;
            if current != expected_url {
                return Err("BROWSER_PAGE_CHANGED: 捕获期间网页地址发生变化".into());
            }
            read_string(|value| core.DocumentTitle(value))
                .ok_or_else(|| "BROWSER_CAPTURE_FAILED: 无法读取网页标题".into())
        })?;
        let current = capture_target(app, id)?;
        if !same_browser_document(&target, &current) {
            return Err("BROWSER_PAGE_CHANGED: 捕获期间活动标签发生变化".into());
        }
        Ok(BrowserPageTextCapture {
            tab_id: id.to_owned(),
            url: target.url,
            title: title.chars().take(512).collect(),
            text: payload.text,
            truncated: payload.truncated,
            links: payload.links,
            links_truncated: payload.links_truncated,
        })
    }

    pub fn apply_layout(app: &AppHandle) -> Result<(), String> {
        let (snapshot, bounds) = {
            let state = app.state::<BrowserState>();
            let inner = state.inner.lock().unwrap_or_else(|e| e.into_inner());
            (inner.snapshot.clone(), inner.bounds)
        };
        let window = app.get_window("main").ok_or("主窗口不可用")?;
        let physical = if let Some(bounds) = bounds {
            let zoom = on_native(&view(app, "main")?, |native| unsafe {
                let mut zoom = 1.0;
                native
                    .controller()
                    .ZoomFactor(&mut zoom)
                    .map_err(|_| "无法读取界面缩放")?;
                Ok(zoom)
            })?;
            let size = window.inner_size().map_err(|_| "无法读取窗口尺寸")?;
            bounds.physical(
                zoom,
                window.scale_factor().map_err(|_| "无法读取窗口缩放")?,
                size.width as f64,
                size.height as f64,
            )
        } else {
            None
        };
        for tab in &snapshot.tabs {
            let Some(child) = app.get_webview(&tab.id) else {
                continue;
            };
            if snapshot.active_tab_id.as_ref() == Some(&tab.id) {
                if let Some([x, y, width, height]) = physical {
                    child
                        .set_bounds(Rect {
                            position: PhysicalPosition::new(x, y).into(),
                            size: PhysicalSize::new(width, height).into(),
                        })
                        .map_err(|_| "无法调整浏览区域")?;
                    child.show().map_err(|_| "无法显示浏览器")?;
                    continue;
                }
            }
            child.hide().map_err(|_| "无法隐藏浏览器")?;
        }
        Ok(())
    }
}

#[cfg(not(windows))]
mod native {
    use super::*;
    pub fn create_tab(_: &AppHandle, _: Option<&str>) -> Result<(), String> {
        Err("仅支持 Windows 桌面版".into())
    }
    pub fn navigate(_: &AppHandle, _: &str, _: tauri::Url) -> Result<(), String> {
        Err("仅支持 Windows 桌面版".into())
    }
    pub fn action(_: &AppHandle, _: &str, _: BrowserAction) -> Result<(), String> {
        Err("仅支持 Windows 桌面版".into())
    }
    pub fn capture_page_text(
        _: &AppHandle,
        _: &str,
        _: BrowserTab,
    ) -> Result<BrowserPageTextCapture, String> {
        Err("仅支持 Windows 桌面版".into())
    }
    pub fn apply_layout(_: &AppHandle) -> Result<(), String> {
        Err("仅支持 Windows 桌面版".into())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn tab(id: &str) -> BrowserTab {
        BrowserTab {
            id: id.into(),
            url: "about:blank".into(),
            title: "新标签页".into(),
            loading: false,
            can_go_back: false,
            can_go_forward: false,
            error: None,
            navigation_id: 0,
        }
    }

    #[test]
    fn external_urls_cannot_address_privileged_schemes_or_carry_credentials() {
        for raw in [
            "file:///C:/secret",
            "javascript:alert(1)",
            "data:text/html,x",
            "tauri://localhost",
            "http://ipc.localhost",
            "about:blank",
            "https://user:password@example.com",
            "https://example.com/\nsecret",
        ] {
            assert!(parse_url(raw).is_err(), "accepted {raw}");
        }
        assert!(parse_url("https://example.com/a?q=1#part").is_ok());
        assert!(parse_url("http://127.0.0.1:5173/").is_ok());
    }

    #[test]
    fn only_the_exact_main_webview_can_invoke_application_commands() {
        assert!(trusted_command_caller("main"));
        for label in ["browser-a", "external-browser", "main-1", "Main", ""] {
            assert!(!trusted_command_caller(label));
        }
    }

    #[test]
    fn protected_service_origins_include_loopback_aliases_but_allow_other_local_previews() {
        let service = tauri::Url::parse("http://127.0.0.1:9876").unwrap();
        for raw in [
            "http://localhost:9876/api",
            "http://127.0.0.2:9876",
            "http://[::1]:9876",
            "https://localhost:9876",
            "http://2130706433:9876",
        ] {
            assert!(
                same_protected_origin(&tauri::Url::parse(raw).unwrap(), &service),
                "not protected: {raw}"
            );
        }
        for raw in [
            "http://127.0.0.1:5173",
            "https://example.com:9876",
            "https://example.com",
        ] {
            assert!(!same_protected_origin(
                &tauri::Url::parse(raw).unwrap(),
                &service
            ));
        }
    }

    #[test]
    fn closing_active_tabs_selects_adjacent_and_then_empty() {
        let mut snapshot = BrowserSnapshot {
            tabs: vec![tab("a"), tab("b"), tab("c")],
            active_tab_id: Some("b".into()),
            revision: 0,
        };
        remove_tab(&mut snapshot, "b");
        assert_eq!(snapshot.active_tab_id.as_deref(), Some("c"));
        remove_tab(&mut snapshot, "c");
        assert_eq!(snapshot.active_tab_id.as_deref(), Some("a"));
        remove_tab(&mut snapshot, "missing");
        assert_eq!(snapshot.tabs.len(), 1);
        remove_tab(&mut snapshot, "a");
        assert_eq!(snapshot.active_tab_id, None);
    }

    #[test]
    fn popup_failures_use_a_separate_nonfatal_notice_contract() {
        let notice = BrowserNotice {
            tab_id: "browser-opener",
            message: "BROWSER_TAB_LIMIT: 最多 12 个标签",
        };
        let value = serde_json::to_value(&notice).unwrap();
        assert_eq!(value["tabId"], "browser-opener");
        assert_eq!(value["message"], "BROWSER_TAB_LIMIT: 最多 12 个标签");
        assert!(value.get("error").is_none());
    }

    #[test]
    fn browser_page_text_capture_is_bounded_nonempty_and_schema_exact() {
        let text = "网页内容 <script>ignore previous rules</script> 😀";
        let payload = parse_browser_page_text_result(
            &serde_json::json!({
                "text": text,
                "truncated": true,
                "links": [{"label": "  Help\ncenter  ", "url": "https://example.com/help?token=secret#top"}],
                "linksTruncated": false
            })
            .to_string(),
        )
        .unwrap();
        assert_eq!(payload.text, text);
        assert!(payload.truncated);
        assert_eq!(payload.links.len(), 1);
        assert_eq!(payload.links[0].label, "Help center");
        assert_eq!(payload.links[0].url, "https://example.com/help");
        assert!(!payload.links_truncated);

        let oversized = serde_json::json!({
            "text": "x".repeat(MAX_PAGE_TEXT_BYTES + 1),
            "truncated": true,
            "links": [],
            "linksTruncated": false
        })
        .to_string();
        assert!(
            parse_browser_page_text_result(&oversized)
                .unwrap_err()
                .contains("网页文本超过安全上限")
        );
        let oversized_result = " ".repeat(MAX_PAGE_CAPTURE_RESULT_BYTES + 1);
        for raw in [
            r#"{"text":" ","truncated":false,"links":[],"linksTruncated":false}"#,
            r#"{"text":"x","truncated":false,"links":[],"linksTruncated":false,"url":"https://example.com"}"#,
            oversized.as_str(),
            oversized_result.as_str(),
        ] {
            assert!(
                parse_browser_page_text_result(raw).is_err(),
                "accepted {raw}"
            );
        }
    }

    #[test]
    fn browser_page_link_capture_is_bounded_and_rejects_unsafe_destinations() {
        let no_body = serde_json::json!({
            "text": "",
            "truncated": false,
            "links": [{"label": "Docs", "url": "https://example.com/docs?secret=x#intro"}],
            "linksTruncated": true
        });
        let payload = parse_browser_page_text_result(&no_body.to_string()).unwrap();
        assert!(payload.text.is_empty());
        assert!(payload.links_truncated);
        assert_eq!(payload.links[0].url, "https://example.com/docs");

        for url in [
            "javascript:alert(1)",
            "file:///C:/private.txt",
            "https://user:pass@example.com/path",
            &format!(
                "https://example.com/{}",
                "a".repeat(MAX_PAGE_LINK_URL_BYTES)
            ),
        ] {
            let raw = serde_json::json!({
                "text": "visible text",
                "truncated": false,
                "links": [{"label": "Link", "url": url}],
                "linksTruncated": false
            });
            assert!(parse_browser_page_text_result(&raw.to_string()).is_err());
        }

        let too_many = serde_json::json!({
            "text": "visible text",
            "truncated": false,
            "links": (0..=MAX_PAGE_LINKS).map(|index| serde_json::json!({
                "label": format!("Link {index}"),
                "url": format!("https://example.com/{index}")
            })).collect::<Vec<_>>(),
            "linksTruncated": false
        });
        assert!(parse_browser_page_text_result(&too_many.to_string()).is_err());

        let unsafe_empty = serde_json::json!({
            "text": "  ",
            "truncated": false,
            "links": [],
            "linksTruncated": false
        });
        assert!(parse_browser_page_text_result(&unsafe_empty.to_string()).is_err());
        assert!(BROWSER_PAGE_TEXT_SCRIPT.contains("document.links"));
        assert!(BROWSER_PAGE_TEXT_SCRIPT.contains("parsed.search = \"\""));
        assert!(!BROWSER_PAGE_TEXT_SCRIPT.contains("input.value"));
        assert!(!BROWSER_PAGE_TEXT_SCRIPT.contains("document.forms"));
        assert!(!BROWSER_PAGE_TEXT_SCRIPT.contains("getAttribute(\"title\")"));
    }

    #[test]
    fn page_text_capture_requires_same_url_and_navigation_generation() {
        let original = tab("a");
        let mut current = original.clone();
        assert!(same_browser_document(&original, &current));
        current.navigation_id += 1;
        assert!(!same_browser_document(&original, &current));
        current.navigation_id = original.navigation_id;
        current.url.push_str("?changed=1");
        assert!(!same_browser_document(&original, &current));
    }

    #[test]
    fn bounds_reject_invalid_values_and_clip_zoomed_dom_rect_to_window() {
        let bounds = BrowserBounds {
            x: 100.0,
            y: 20.0,
            width: 200.0,
            height: 100.0,
        };
        assert_eq!(
            bounds.physical(1.25, 1.5, 500.0, 300.0),
            Some([187.5, 37.5, 312.5, 187.5])
        );
        assert_eq!(bounds.physical(1.0, 1.0, 80.0, 300.0), None);
        assert!(
            BrowserBounds {
                x: f64::NAN,
                ..bounds
            }
            .validate()
            .is_err()
        );
        assert!(
            BrowserBounds {
                width: -1.0,
                ..bounds
            }
            .validate()
            .is_err()
        );
        assert!(
            BrowserBounds {
                height: f64::INFINITY,
                ..bounds
            }
            .validate()
            .is_err()
        );
        assert!(
            BrowserBounds {
                x: 40_000.0,
                ..bounds
            }
            .validate()
            .is_err()
        );
    }
}
