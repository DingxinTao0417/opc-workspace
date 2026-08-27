use std::{
    env, fs,
    net::{IpAddr, Ipv4Addr, SocketAddr},
    path::PathBuf,
    str::FromStr,
    sync::{
        Arc, Condvar, Mutex, RwLock,
        atomic::{AtomicBool, Ordering},
    },
    time::Duration,
};

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager, State};
use tauri_plugin_shell::{
    ShellExt,
    process::{CommandChild, CommandEvent},
};
use url::Url;
use uuid::Uuid;

const SIDECAR_NAME: &str = "opc-sidecar";
const LOOPBACK_HOST: &str = "127.0.0.1";
const HEALTH_ATTEMPTS: usize = 10;
const HEALTH_RETRY_DELAY: Duration = Duration::from_millis(100);
const HEALTH_REQUEST_TIMEOUT: Duration = Duration::from_millis(400);
const GRACEFUL_SHUTDOWN_TIMEOUT: Duration = Duration::from_secs(7);

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct SidecarRuntimeStatus {
    pub phase: SidecarPhase,
    pub base_url: Option<String>,
    pub session_token: Option<String>,
    pub message: Option<String>,
    pub app_version: Option<String>,
    pub api_version: Option<String>,
    pub schema_version: Option<String>,
}

impl SidecarRuntimeStatus {
    fn starting(base_url: Option<String>, session_token: Option<String>) -> Self {
        Self {
            phase: SidecarPhase::Starting,
            base_url,
            session_token,
            message: Some("正在连接本地服务".to_owned()),
            app_version: None,
            api_version: None,
            schema_version: None,
        }
    }
}

#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum SidecarPhase {
    Starting,
    Ready,
    Error,
}

struct SidecarManagerInner {
    runtime: RwLock<SidecarRuntimeStatus>,
    child: Mutex<Option<CommandChild>>,
    exited: (Mutex<bool>, Condvar),
    shutting_down: AtomicBool,
}

#[derive(Clone)]
pub struct SidecarManager {
    inner: Arc<SidecarManagerInner>,
}

impl SidecarManager {
    pub fn new() -> Self {
        Self {
            inner: Arc::new(SidecarManagerInner {
                runtime: RwLock::new(SidecarRuntimeStatus::starting(None, None)),
                child: Mutex::new(None),
                exited: (Mutex::new(false), Condvar::new()),
                shutting_down: AtomicBool::new(false),
            }),
        }
    }

    fn status(&self) -> SidecarRuntimeStatus {
        self.inner
            .runtime
            .read()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .clone()
    }

    fn update(&self, update: impl FnOnce(&mut SidecarRuntimeStatus)) {
        let mut runtime = self
            .inner
            .runtime
            .write()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        update(&mut runtime);
    }

    fn set_starting(&self, base_url: Option<String>, session_token: Option<String>) {
        self.update(|runtime| {
            *runtime = SidecarRuntimeStatus::starting(base_url, session_token);
        });
    }

    fn set_ready(&self, ready: &ReadyLine, session_token: Option<String>) {
        self.update(|runtime| {
            runtime.phase = SidecarPhase::Ready;
            runtime.base_url = Some(ready.base_url.clone());
            runtime.session_token = session_token;
            runtime.message = None;
            runtime.app_version = ready.app_version.clone();
            runtime.api_version = ready.api_version.clone();
            runtime.schema_version = ready.schema_version.clone();
        });
    }

    fn set_external_ready(&self, base_url: String, session_token: Option<String>) {
        self.update(|runtime| {
            runtime.phase = SidecarPhase::Ready;
            runtime.base_url = Some(base_url);
            runtime.session_token = session_token;
            runtime.message = None;
        });
    }

    fn set_error(&self, message: impl Into<String>) {
        self.update(|runtime| {
            runtime.phase = SidecarPhase::Error;
            runtime.message = Some(message.into());
        });
    }

    fn set_child(&self, child: CommandChild) {
        let (exited, _) = &self.inner.exited;
        *exited
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner()) = false;
        let mut slot = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        *slot = Some(child);
    }

    fn mark_exited(&self) {
        let (exited, changed) = &self.inner.exited;
        let mut exited = exited
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        *exited = true;
        changed.notify_all();
    }

    fn wait_for_exit(&self, timeout: Duration) -> bool {
        let (exited, changed) = &self.inner.exited;
        let exited = exited
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if *exited {
            return true;
        }
        match changed.wait_timeout_while(exited, timeout, |exited| !*exited) {
            Ok((exited, _)) => *exited,
            Err(poisoned) => {
                let (exited, _) = poisoned.into_inner();
                *exited
            }
        }
    }

    fn kill_child(&self) {
        let child = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .take();
        if let Some(child) = child {
            let _ = child.kill();
        }
    }

    pub fn shutdown(&self) {
        if self.inner.shutting_down.swap(true, Ordering::SeqCst) {
            return;
        }

        let child = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .take();

        if let Some(mut child) = child {
            if child.write(b"shutdown\n").is_ok() && self.wait_for_exit(GRACEFUL_SHUTDOWN_TIMEOUT) {
                return;
            }
            let _ = child.kill();
        }
    }

    fn is_shutting_down(&self) -> bool {
        self.inner.shutting_down.load(Ordering::SeqCst)
    }
}

#[tauri::command]
pub fn sidecar_status(state: State<'_, SidecarManager>) -> SidecarRuntimeStatus {
    state.status()
}

pub fn initialize_sidecar(app: &AppHandle, manager: SidecarManager) {
    match env::var("OPC_SIDECAR_URL") {
        Ok(raw_url) if !raw_url.trim().is_empty() => {
            initialize_external_sidecar(raw_url, manager);
        }
        _ => {
            if let Err(error) = spawn_bundled_sidecar(app, manager.clone()) {
                manager.set_error(error);
            }
        }
    }
}

fn initialize_external_sidecar(raw_url: String, manager: SidecarManager) {
    let base_url = match normalize_loopback_url(&raw_url) {
        Ok(base_url) => base_url,
        Err(error) => {
            manager.set_error(format!("OPC_SIDECAR_URL 无效：{error}"));
            return;
        }
    };
    let token = env::var("OPC_SESSION_TOKEN")
        .ok()
        .filter(|value| !value.is_empty());

    manager.set_starting(Some(base_url.clone()), token.clone());
    tauri::async_runtime::spawn(async move {
        match verify_health(&base_url, token.as_deref()).await {
            Ok(()) => manager.set_external_ready(base_url, token),
            Err(error) => manager.set_error(format!("开发 Sidecar 健康检查失败：{error}")),
        }
    });
}

fn spawn_bundled_sidecar(app: &AppHandle, manager: SidecarManager) -> Result<(), String> {
    let app_data_dir = app
        .path()
        .app_data_dir()
        .map_err(|error| format!("无法定位应用数据目录：{error}"))?;
    let app_log_dir = app
        .path()
        .app_log_dir()
        .map_err(|error| format!("无法定位应用日志目录：{error}"))?;

    prepare_runtime_directories(&app_data_dir, &app_log_dir)?;

    let database_path = app_data_dir.join("opc-workspace.db");
    let token = generate_session_token();
    manager.set_starting(None, Some(token.clone()));

    let command = app
        .shell()
        .sidecar(SIDECAR_NAME)
        .map_err(|error| format!("无法定位内置 Sidecar：{error}"))?
        .env("OPC_HOST", LOOPBACK_HOST)
        .env("OPC_PORT", "0")
        .env("OPC_SESSION_TOKEN", &token)
        .env("OPC_DB_PATH", &database_path)
        .env("OPC_LOG_DIR", &app_log_dir)
        .env(
            "OPC_ALLOWED_ORIGINS",
            "http://tauri.localhost,https://tauri.localhost,tauri://localhost",
        );

    let (mut events, child) = command
        .spawn()
        .map_err(|error| format!("无法启动内置 Sidecar：{error}"))?;
    manager.set_child(child);

    tauri::async_runtime::spawn(async move {
        let mut reached_ready = false;
        while let Some(event) = events.recv().await {
            match event {
                CommandEvent::Stdout(bytes) if !reached_ready => {
                    let line = String::from_utf8_lossy(&bytes);
                    match parse_ready_line(line.trim()) {
                        Ok(ready) => {
                            reached_ready = true;
                            match verify_health(&ready.base_url, Some(&token)).await {
                                Ok(()) => manager.set_ready(&ready, Some(token.clone())),
                                Err(error) => {
                                    manager.set_error(format!("Sidecar 健康检查失败：{error}"));
                                    manager.kill_child();
                                    break;
                                }
                            }
                        }
                        Err(error) => {
                            manager.set_error(format!("Sidecar ready 握手无效：{error}"));
                            manager.kill_child();
                            break;
                        }
                    }
                }
                CommandEvent::Error(error) if !manager.is_shutting_down() => {
                    manager.set_error(format!("Sidecar 进程错误：{error}"));
                    manager.kill_child();
                    break;
                }
                CommandEvent::Terminated(payload) => {
                    manager.mark_exited();
                    if !manager.is_shutting_down() {
                        manager.set_error(format!(
                            "Sidecar 已退出（code={:?}, signal={:?}）",
                            payload.code, payload.signal
                        ));
                    }
                    break;
                }
                _ => {}
            }
        }

        manager.mark_exited();

        if !manager.is_shutting_down()
            && manager.status().phase != SidecarPhase::Error
            && manager.status().phase != SidecarPhase::Ready
        {
            manager.set_error("Sidecar 在完成 ready 握手前关闭了输出");
        }
    });

    Ok(())
}

fn prepare_runtime_directories(
    app_data_dir: &PathBuf,
    app_log_dir: &PathBuf,
) -> Result<(), String> {
    for directory in [
        app_data_dir.clone(),
        app_data_dir.join("attachments"),
        app_data_dir.join("invoices"),
        app_data_dir.join("backups"),
        app_data_dir.join("config"),
        app_log_dir.clone(),
    ] {
        fs::create_dir_all(&directory)
            .map_err(|error| format!("无法创建运行目录 {}：{error}", directory.display()))?;
    }
    Ok(())
}

fn generate_session_token() -> String {
    format!("{}{}", Uuid::new_v4().simple(), Uuid::new_v4().simple())
}

async fn verify_health(base_url: &str, token: Option<&str>) -> Result<(), String> {
    let client = reqwest::Client::builder()
        .timeout(HEALTH_REQUEST_TIMEOUT)
        .build()
        .map_err(|error| error.to_string())?;
    let health_url = format!("{base_url}/health");
    let mut last_error = "未返回健康响应".to_owned();

    for _ in 0..HEALTH_ATTEMPTS {
        let mut request = client
            .get(&health_url)
            .header(reqwest::header::ORIGIN, "http://tauri.localhost");
        if let Some(token) = token {
            request = request.bearer_auth(token);
        }

        match request.send().await {
            Ok(response) if response.status().is_success() => return Ok(()),
            Ok(response) => {
                last_error = format!("HTTP {}", response.status());
            }
            Err(error) => {
                last_error = error.to_string();
            }
        }

        tokio::time::sleep(HEALTH_RETRY_DELAY).await;
    }

    Err(last_error)
}

#[derive(Debug, Deserialize)]
struct RawReadyLine {
    event: Option<String>,
    status: Option<String>,
    host: Option<String>,
    address: Option<String>,
    url: Option<String>,
    port: Option<u16>,
    version: Option<String>,
    app_version: Option<String>,
    api_version: Option<String>,
    schema_version: Option<serde_json::Value>,
}

#[derive(Debug, PartialEq, Eq)]
struct ReadyLine {
    base_url: String,
    app_version: Option<String>,
    api_version: Option<String>,
    schema_version: Option<String>,
}

fn parse_ready_line(line: &str) -> Result<ReadyLine, String> {
    let raw: RawReadyLine =
        serde_json::from_str(line).map_err(|error| format!("不是合法 JSON：{error}"))?;
    let is_ready = raw.event.as_deref() == Some("ready") || raw.status.as_deref() == Some("ready");
    if !is_ready {
        return Err("event/status 不是 ready".to_owned());
    }

    let base_url = if let Some(url) = raw.url.as_deref() {
        normalize_loopback_url(url)?
    } else if let Some(address) = raw.address.as_deref() {
        base_url_from_address(address)?
    } else {
        let host = raw.host.as_deref().unwrap_or(LOOPBACK_HOST);
        base_url_from_host_port(host, raw.port.ok_or("缺少监听端口")?)?
    };

    Ok(ReadyLine {
        base_url,
        app_version: raw.app_version.or(raw.version),
        api_version: raw.api_version,
        schema_version: raw.schema_version.and_then(json_scalar_to_string),
    })
}

fn json_scalar_to_string(value: serde_json::Value) -> Option<String> {
    match value {
        serde_json::Value::String(value) => Some(value),
        serde_json::Value::Number(value) => Some(value.to_string()),
        _ => None,
    }
}

fn normalize_loopback_url(raw_url: &str) -> Result<String, String> {
    let url = Url::parse(raw_url).map_err(|error| error.to_string())?;
    if url.scheme() != "http" {
        return Err("仅允许 http 协议".to_owned());
    }
    if url.host_str() != Some(LOOPBACK_HOST) {
        return Err("仅允许 127.0.0.1".to_owned());
    }
    if !url.username().is_empty() || url.password().is_some() {
        return Err("URL 不得包含凭据".to_owned());
    }
    if url.path() != "/" && !url.path().is_empty()
        || url.query().is_some()
        || url.fragment().is_some()
    {
        return Err("URL 必须是纯基础地址".to_owned());
    }
    let port = url.port().ok_or("缺少显式端口")?;
    if port == 0 {
        return Err("端口不能为 0".to_owned());
    }
    Ok(format!("http://{LOOPBACK_HOST}:{port}"))
}

fn base_url_from_address(address: &str) -> Result<String, String> {
    let address = SocketAddr::from_str(address).map_err(|error| error.to_string())?;
    if address.ip() != IpAddr::V4(Ipv4Addr::LOCALHOST) {
        return Err("Sidecar 未绑定到 127.0.0.1".to_owned());
    }
    if address.port() == 0 {
        return Err("Sidecar ready 端口不能为 0".to_owned());
    }
    Ok(format!("http://{address}"))
}

fn base_url_from_host_port(host: &str, port: u16) -> Result<String, String> {
    if host != LOOPBACK_HOST {
        return Err("Sidecar 未绑定到 127.0.0.1".to_owned());
    }
    if port == 0 {
        return Err("Sidecar ready 端口不能为 0".to_owned());
    }
    Ok(format!("http://{host}:{port}"))
}

#[cfg(test)]
mod tests {
    use super::{SidecarManager, SidecarPhase, SidecarRuntimeStatus, parse_ready_line};

    #[test]
    fn parses_backend_ready_payload_and_versions() {
        let ready = parse_ready_line(
            r#"{"event":"ready","status":"ready","url":"http://127.0.0.1:43127","address":"127.0.0.1:43127","port":43127,"app_version":"0.1.0","api_version":"v1","schema_version":1}"#,
        )
        .expect("ready line should parse");

        assert_eq!(ready.base_url, "http://127.0.0.1:43127");
        assert_eq!(ready.app_version.as_deref(), Some("0.1.0"));
        assert_eq!(ready.api_version.as_deref(), Some("v1"));
        assert_eq!(ready.schema_version.as_deref(), Some("1"));
    }

    #[test]
    fn parses_host_and_port_fallback() {
        let ready =
            parse_ready_line(r#"{"event":"ready","host":"127.0.0.1","port":9876,"version":"dev"}"#)
                .expect("host/port ready line should parse");

        assert_eq!(ready.base_url, "http://127.0.0.1:9876");
        assert_eq!(ready.app_version.as_deref(), Some("dev"));
    }

    #[test]
    fn rejects_non_loopback_ready_address() {
        let error =
            parse_ready_line(r#"{"event":"ready","url":"http://0.0.0.0:9876","port":9876}"#)
                .expect_err("non-loopback URL must be rejected");

        assert!(error.contains("127.0.0.1"));
    }

    #[test]
    fn rejects_malformed_or_non_ready_output() {
        assert!(parse_ready_line("not-json").is_err());
        assert!(parse_ready_line(r#"{"event":"starting","port":9876}"#).is_err());
        assert!(parse_ready_line(r#"{"event":"ready","port":0}"#).is_err());
    }

    #[test]
    fn serializes_status_with_frontend_contract() {
        let status = SidecarRuntimeStatus {
            phase: SidecarPhase::Ready,
            base_url: Some("http://127.0.0.1:9876".to_owned()),
            session_token: Some("token".to_owned()),
            message: None,
            app_version: Some("0.1.0".to_owned()),
            api_version: Some("v1".to_owned()),
            schema_version: Some("1".to_owned()),
        };

        let json = serde_json::to_value(status).expect("status should serialize");
        assert_eq!(json["phase"], "ready");
        assert_eq!(json["baseUrl"], "http://127.0.0.1:9876");
        assert_eq!(json["sessionToken"], "token");
        assert_eq!(json["schemaVersion"], "1");
    }

    #[test]
    fn runtime_state_moves_from_starting_to_ready_and_error() {
        let manager = SidecarManager::new();
        assert_eq!(manager.status().phase, SidecarPhase::Starting);

        manager.set_starting(
            Some("http://127.0.0.1:9876".to_owned()),
            Some("token".to_owned()),
        );
        manager.set_external_ready("http://127.0.0.1:9876".to_owned(), Some("token".to_owned()));
        let ready = manager.status();
        assert_eq!(ready.phase, SidecarPhase::Ready);
        assert_eq!(ready.session_token.as_deref(), Some("token"));

        manager.set_error("sidecar stopped");
        let error = manager.status();
        assert_eq!(error.phase, SidecarPhase::Error);
        assert_eq!(error.message.as_deref(), Some("sidecar stopped"));
    }
}
