use std::{
    env, fs,
    net::{IpAddr, Ipv4Addr, SocketAddr},
    path::PathBuf,
    process::Command,
    str::FromStr,
    sync::{Arc, Condvar, Mutex, RwLock},
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

use crate::desktop_log::{DesktopEvent, DesktopLogger};

const SIDECAR_NAME: &str = "opc-sidecar";
const LOOPBACK_HOST: &str = "127.0.0.1";
const HEALTH_ATTEMPTS: usize = 10;
const HEALTH_RETRY_DELAY: Duration = Duration::from_millis(100);
const HEALTH_REQUEST_TIMEOUT: Duration = Duration::from_millis(400);
const READY_HANDSHAKE_TIMEOUT: Duration = Duration::from_secs(90);
const GRACEFUL_SHUTDOWN_TIMEOUT: Duration = Duration::from_secs(7);
const FORCED_TERMINATION_TIMEOUT: Duration = Duration::from_secs(5);

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

trait SidecarChildControl: Send {
    fn write(&mut self, bytes: &[u8]) -> Result<(), String>;
    fn kill(self: Box<Self>) -> Result<(), String>;
}

impl SidecarChildControl for CommandChild {
    fn write(&mut self, bytes: &[u8]) -> Result<(), String> {
        CommandChild::write(self, bytes).map_err(|error| error.to_string())
    }

    fn kill(self: Box<Self>) -> Result<(), String> {
        CommandChild::kill(*self).map_err(|error| error.to_string())
    }
}

#[derive(Default)]
struct ManagedChildState {
    child: Option<Box<dyn SidecarChildControl>>,
    shutting_down: bool,
    termination_requested: bool,
}

enum ChildKillOutcome {
    Requested,
    Missing,
    ShutdownOwnsChild,
    Failed(String),
}

struct SidecarManagerInner {
    runtime: RwLock<SidecarRuntimeStatus>,
    child: Mutex<ManagedChildState>,
    exited: (Mutex<bool>, Condvar),
    logger: RwLock<Option<DesktopLogger>>,
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
                child: Mutex::new(ManagedChildState::default()),
                exited: (Mutex::new(false), Condvar::new()),
                logger: RwLock::new(None),
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

    pub fn attach_logger(&self, logger: DesktopLogger) {
        *self
            .inner
            .logger
            .write()
            .unwrap_or_else(|poisoned| poisoned.into_inner()) = Some(logger);
    }

    pub fn log_event(&self, event: DesktopEvent) {
        if let Some(logger) = self
            .inner
            .logger
            .read()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .as_ref()
        {
            logger.event(event);
        }
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
        self.log_event(DesktopEvent::SidecarStarting);
        self.update(|runtime| {
            *runtime = SidecarRuntimeStatus::starting(base_url, session_token);
        });
    }

    fn set_ready(&self, ready: &ReadyLine, session_token: Option<String>) {
        self.log_event(DesktopEvent::SidecarReady);
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
        self.log_event(DesktopEvent::SidecarReady);
        self.update(|runtime| {
            runtime.phase = SidecarPhase::Ready;
            runtime.base_url = Some(base_url);
            runtime.session_token = session_token;
            runtime.message = None;
        });
    }

    fn set_error(&self, message: impl Into<String>) {
        self.log_event(DesktopEvent::SidecarError);
        self.update(|runtime| {
            runtime.phase = SidecarPhase::Error;
            runtime.message = Some(message.into());
        });
    }

    fn append_error(&self, message: impl Into<String>) {
        self.log_event(DesktopEvent::SidecarError);
        let message = message.into();
        self.update(|runtime| {
            let existing = runtime.message.take();
            runtime.phase = SidecarPhase::Error;
            runtime.message = Some(match existing {
                Some(existing) if !existing.is_empty() => format!("{existing}；{message}"),
                _ => message,
            });
        });
    }

    fn spawn_and_register_child<T>(
        &self,
        spawn: impl FnOnce() -> Result<(T, Box<dyn SidecarChildControl>), String>,
    ) -> Result<Option<T>, String> {
        let mut state = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.shutting_down {
            return Ok(None);
        }
        let (events, child) = spawn()?;
        let (exited, _) = &self.inner.exited;
        *exited
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner()) = false;
        state.child = Some(child);
        state.termination_requested = false;
        Ok(Some(events))
    }

    fn mark_exited(&self) {
        {
            let mut state = self
                .inner
                .child
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            state.child = None;
            state.termination_requested = false;
        }
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

    fn request_child_kill(&self) -> ChildKillOutcome {
        let child = {
            let mut state = self
                .inner
                .child
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            if state.shutting_down {
                return ChildKillOutcome::ShutdownOwnsChild;
            }
            let child = state.child.take();
            if child.is_some() {
                state.termination_requested = true;
            }
            child
        };
        let Some(child) = child else {
            return ChildKillOutcome::Missing;
        };
        match child.kill() {
            Ok(()) => ChildKillOutcome::Requested,
            Err(error) => ChildKillOutcome::Failed(error),
        }
    }

    pub fn shutdown(&self) {
        self.log_event(DesktopEvent::SidecarShutdownStarted);
        self.shutdown_with_timeouts(GRACEFUL_SHUTDOWN_TIMEOUT, FORCED_TERMINATION_TIMEOUT);
        self.log_event(DesktopEvent::SidecarShutdownFinished);
    }

    pub fn shutdown_for_restart(&self) -> Result<(), String> {
        let has_managed_child = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .child
            .is_some();
        if !has_managed_child {
            return Err("当前 Sidecar 不由桌面应用管理，请手动重启开发服务".to_owned());
        }
        self.shutdown();
        if self.wait_for_exit(Duration::ZERO) {
            return Ok(());
        }
        Err(self
            .status()
            .message
            .unwrap_or_else(|| "Sidecar 未确认安全退出，已取消应用重启".to_owned()))
    }

    fn shutdown_with_timeouts(&self, graceful_timeout: Duration, forced_timeout: Duration) {
        let (child, termination_requested) = {
            let mut state = self
                .inner
                .child
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            if state.shutting_down {
                return;
            }
            state.shutting_down = true;
            (state.child.take(), state.termination_requested)
        };

        if child.is_none() && termination_requested {
            if !self.wait_for_exit(forced_timeout) {
                self.append_error(format!(
                    "应用关闭时在 {} 毫秒内仍未收到 Sidecar 的真实退出确认",
                    forced_timeout.as_millis()
                ));
            }
            return;
        }

        if let Some(mut child) = child {
            let stop_context = match child.write(b"shutdown\n") {
                Ok(()) => {
                    if self.wait_for_exit(graceful_timeout) {
                        return;
                    }
                    "Sidecar 未在优雅关闭期限内退出".to_owned()
                }
                Err(error) => {
                    let message = format!("无法向 Sidecar 写入关闭请求：{error}");
                    self.set_error(message.clone());
                    if self.wait_for_exit(Duration::ZERO) {
                        return;
                    }
                    message
                }
            };
            if self.wait_for_exit(Duration::ZERO) {
                return;
            }
            match child.kill() {
                Ok(()) => {
                    self.set_error(format!(
                        "{stop_context}；已发送强制终止请求，正在等待真实退出确认"
                    ));
                    if !self.wait_for_exit(forced_timeout) {
                        self.append_error(format!(
                            "在 {} 毫秒内未收到 Sidecar 的真实退出确认",
                            forced_timeout.as_millis()
                        ));
                    }
                }
                Err(error) => {
                    self.set_error(format!("{stop_context}；强制终止 Sidecar 失败：{error}"));
                }
            }
        }
    }

    fn is_shutting_down(&self) -> bool {
        self.inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .shutting_down
    }
}

#[tauri::command]
pub fn sidecar_status(state: State<'_, SidecarManager>) -> SidecarRuntimeStatus {
    state.status()
}

#[tauri::command]
pub fn restart_application(app: AppHandle, state: State<'_, SidecarManager>) -> Result<(), String> {
    state.log_event(DesktopEvent::ApplicationRestartRequested);
    state.shutdown_for_restart()?;
    app.request_restart();
    Ok(())
}

#[tauri::command]
pub fn open_log_directory(app: AppHandle) -> Result<(), String> {
    let log_dir = app
        .path()
        .app_log_dir()
        .map_err(|_| "无法定位应用日志目录".to_owned())?;
    fs::create_dir_all(&log_dir).map_err(|_| "无法准备应用日志目录".to_owned())?;

    #[cfg(target_os = "windows")]
    let mut command = Command::new("explorer.exe");
    #[cfg(target_os = "macos")]
    let mut command = Command::new("open");
    #[cfg(all(unix, not(target_os = "macos")))]
    let mut command = Command::new("xdg-open");

    command.arg(&log_dir);
    command
        .spawn()
        .map_err(|_| "无法打开应用日志目录".to_owned())?;
    Ok(())
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
    let artifact_dir = app_data_dir.join("artifacts");
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
        .env("OPC_ARTIFACT_DIR", &artifact_dir)
        .env("OPC_LOG_DIR", &app_log_dir)
        .env(
            "OPC_ALLOWED_ORIGINS",
            "http://tauri.localhost,https://tauri.localhost,tauri://localhost",
        );

    let Some(mut events) = manager.spawn_and_register_child(|| {
        command
            .spawn()
            .map(|(events, child)| {
                let child: Box<dyn SidecarChildControl> = Box::new(child);
                (events, child)
            })
            .map_err(|error| format!("无法启动内置 Sidecar：{error}"))
    })?
    else {
        return Ok(());
    };

    tauri::async_runtime::spawn(async move {
        let mut reached_ready = false;
        let mut stopping_after_error = false;
        let mut forced_termination_deadline = None;
        let mut stream_closed_without_termination = false;
        let ready_deadline = tokio::time::Instant::now() + READY_HANDSHAKE_TIMEOUT;
        loop {
            let event = if let Some(deadline) = forced_termination_deadline {
                match tokio::time::timeout_at(deadline, events.recv()).await {
                    Ok(event) => event,
                    Err(_) => {
                        manager.append_error(format!(
                            "在强制停止期限 {} 毫秒内未收到 Sidecar 的真实退出确认",
                            FORCED_TERMINATION_TIMEOUT.as_millis()
                        ));
                        // The bounded confirmation wait is over, but retain the
                        // receiver so a late real Terminated event can still be
                        // observed and wake a concurrent shutdown waiter.
                        forced_termination_deadline = None;
                        continue;
                    }
                }
            } else if reached_ready || stopping_after_error {
                events.recv().await
            } else {
                match receive_before_ready_deadline(
                    events.recv(),
                    ready_deadline,
                    READY_HANDSHAKE_TIMEOUT,
                )
                .await
                {
                    Ok(event) => event,
                    Err(error) => {
                        if handle_ready_handshake_timeout(&manager, error) {
                            forced_termination_deadline =
                                Some(tokio::time::Instant::now() + FORCED_TERMINATION_TIMEOUT);
                        }
                        // If shutdown owns the child, keep draining events so a
                        // real Terminated event can wake its graceful wait.
                        stopping_after_error = true;
                        continue;
                    }
                }
            };
            let Some(event) = event else {
                stream_closed_without_termination = true;
                break;
            };

            match event {
                CommandEvent::Stdout(bytes) if !reached_ready && !stopping_after_error => {
                    let line = String::from_utf8_lossy(&bytes);
                    match parse_ready_line(line.trim()) {
                        Ok(ready) => {
                            reached_ready = true;
                            match verify_health(&ready.base_url, Some(&token)).await {
                                Ok(()) => manager.set_ready(&ready, Some(token.clone())),
                                Err(error) => {
                                    if begin_forced_stop(
                                        &manager,
                                        format!("Sidecar 健康检查失败：{error}"),
                                    ) {
                                        forced_termination_deadline = Some(
                                            tokio::time::Instant::now()
                                                + FORCED_TERMINATION_TIMEOUT,
                                        );
                                    }
                                    stopping_after_error = true;
                                }
                            }
                        }
                        Err(error) => {
                            if begin_forced_stop(
                                &manager,
                                format!("Sidecar ready 握手无效：{error}"),
                            ) {
                                forced_termination_deadline =
                                    Some(tokio::time::Instant::now() + FORCED_TERMINATION_TIMEOUT);
                            }
                            stopping_after_error = true;
                        }
                    }
                }
                CommandEvent::Error(error)
                    if !manager.is_shutting_down() && !stopping_after_error =>
                {
                    if begin_forced_stop(&manager, format!("Sidecar 进程错误：{error}")) {
                        forced_termination_deadline =
                            Some(tokio::time::Instant::now() + FORCED_TERMINATION_TIMEOUT);
                    }
                    stopping_after_error = true;
                }
                CommandEvent::Terminated(payload) => {
                    manager.mark_exited();
                    if !manager.is_shutting_down() && manager.status().phase != SidecarPhase::Error
                    {
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

        if stream_closed_without_termination {
            manager.append_error("Sidecar 事件流已关闭，未收到真实退出确认");
        }
    });

    Ok(())
}

async fn receive_before_ready_deadline<T>(
    receive: impl std::future::Future<Output = T>,
    deadline: tokio::time::Instant,
    timeout: Duration,
) -> Result<T, String> {
    tokio::time::timeout_at(deadline, receive)
        .await
        .map_err(|_| ready_handshake_timeout_message(timeout))
}

fn ready_handshake_timeout_message(timeout: Duration) -> String {
    let timeout_label = if timeout.as_secs() > 0 {
        format!("{} 秒", timeout.as_secs())
    } else {
        format!("{} 毫秒", timeout.as_millis())
    };
    format!("Sidecar ready 握手在 {timeout_label}内未完成")
}

fn handle_ready_handshake_timeout(manager: &SidecarManager, error: String) -> bool {
    begin_forced_stop(manager, error)
}

fn begin_forced_stop(manager: &SidecarManager, error: String) -> bool {
    match manager.request_child_kill() {
        ChildKillOutcome::ShutdownOwnsChild => false,
        ChildKillOutcome::Requested => {
            manager.set_error(format!("{error}；已发送终止请求，正在等待真实退出确认"));
            true
        }
        ChildKillOutcome::Missing => {
            manager.set_error(format!("{error}；没有可用的子进程句柄，无法发送终止请求"));
            true
        }
        ChildKillOutcome::Failed(kill_error) => {
            manager.set_error(format!("{error}；终止 Sidecar 失败：{kill_error}"));
            true
        }
    }
}

fn prepare_runtime_directories(
    app_data_dir: &PathBuf,
    app_log_dir: &PathBuf,
) -> Result<(), String> {
    for directory in [
        app_data_dir.clone(),
        app_data_dir.join("attachments"),
        app_data_dir.join("artifacts"),
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
    use std::{
        future,
        sync::{Arc, Mutex, mpsc},
        thread,
        time::{Duration, Instant},
    };

    use super::{
        SidecarChildControl, SidecarManager, SidecarPhase, SidecarRuntimeStatus,
        handle_ready_handshake_timeout, parse_ready_line, receive_before_ready_deadline,
    };

    #[derive(Default)]
    struct FakeChildState {
        writes: Vec<Vec<u8>>,
        kill_calls: usize,
    }

    struct FakeChild {
        state: Arc<Mutex<FakeChildState>>,
        write_error: Option<&'static str>,
        kill_error: Option<&'static str>,
    }

    impl SidecarChildControl for FakeChild {
        fn write(&mut self, bytes: &[u8]) -> Result<(), String> {
            self.state
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .writes
                .push(bytes.to_vec());
            match self.write_error {
                Some(error) => Err(error.to_owned()),
                None => Ok(()),
            }
        }

        fn kill(self: Box<Self>) -> Result<(), String> {
            self.state
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .kill_calls += 1;
            match self.kill_error {
                Some(error) => Err(error.to_owned()),
                None => Ok(()),
            }
        }
    }

    fn install_fake_child(
        manager: &SidecarManager,
        write_error: Option<&'static str>,
        kill_error: Option<&'static str>,
    ) -> Arc<Mutex<FakeChildState>> {
        let state = Arc::new(Mutex::new(FakeChildState::default()));
        let registered = manager
            .spawn_and_register_child(|| {
                let child: Box<dyn SidecarChildControl> = Box::new(FakeChild {
                    state: state.clone(),
                    write_error,
                    kill_error,
                });
                Ok(((), child))
            })
            .expect("fake child registration should succeed");
        assert!(registered.is_some());
        state
    }

    fn wait_until(mut predicate: impl FnMut() -> bool) {
        let deadline = Instant::now() + Duration::from_secs(1);
        while !predicate() && Instant::now() < deadline {
            thread::yield_now();
        }
        assert!(
            predicate(),
            "condition was not reached before the test deadline"
        );
    }

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

    #[test]
    fn living_silent_sidecar_hits_ready_handshake_timeout() {
        let manager = SidecarManager::new();
        let timeout = Duration::from_millis(20);

        let timed_out = tauri::async_runtime::block_on(async {
            let deadline = tokio::time::Instant::now() + timeout;
            receive_before_ready_deadline(future::pending::<()>(), deadline, timeout).await
        });

        let error = timed_out.expect_err("an open but silent event stream must time out");
        assert!(error.contains("20 毫秒"));
        assert!(handle_ready_handshake_timeout(&manager, error));

        let status = manager.status();
        assert_eq!(status.phase, SidecarPhase::Error);
        assert!(
            status
                .message
                .as_deref()
                .is_some_and(|message| message.contains("ready 握手")
                    && message.contains("无法发送终止请求")
                    && !message.contains("已终止"))
        );
        assert!(!manager.wait_for_exit(Duration::ZERO));
        assert!(
            manager
                .inner
                .child
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .child
                .is_none()
        );
    }

    #[test]
    fn ready_timeout_keeps_receiver_alive_while_shutdown_owns_child() {
        let manager = SidecarManager::new();
        let child_state = install_fake_child(&manager, None, None);
        let shutdown_manager = manager.clone();
        let shutdown = thread::spawn(move || {
            shutdown_manager
                .shutdown_with_timeouts(Duration::from_millis(200), Duration::from_millis(50));
        });

        wait_until(|| {
            !child_state
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .writes
                .is_empty()
        });

        assert!(!handle_ready_handshake_timeout(
            &manager,
            "timeout during shutdown".to_owned(),
        ));
        assert_eq!(manager.status().phase, SidecarPhase::Starting);
        assert!(!manager.wait_for_exit(Duration::ZERO));

        // Simulate the receiver continuing to drain and observing Terminated.
        manager.mark_exited();
        shutdown.join().expect("shutdown thread should finish");
        let state = child_state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        assert_eq!(state.writes, vec![b"shutdown\n".to_vec()]);
        assert_eq!(state.kill_calls, 0);
        assert!(manager.wait_for_exit(Duration::ZERO));
    }

    #[test]
    fn shutdown_before_spawn_prevents_child_creation() {
        let manager = SidecarManager::new();
        manager.shutdown_with_timeouts(Duration::ZERO, Duration::ZERO);
        let spawn_called = Arc::new(Mutex::new(false));
        let spawn_called_in_closure = spawn_called.clone();

        let result = manager
            .spawn_and_register_child(|| {
                *spawn_called_in_closure
                    .lock()
                    .unwrap_or_else(|poisoned| poisoned.into_inner()) = true;
                let child: Box<dyn SidecarChildControl> = Box::new(FakeChild {
                    state: Arc::new(Mutex::new(FakeChildState::default())),
                    write_error: None,
                    kill_error: None,
                });
                Ok(((), child))
            })
            .expect("shutdown should reject spawning without an error");

        assert!(result.is_none());
        assert!(!*spawn_called.lock().unwrap_or_else(|p| p.into_inner()));
        let state = manager
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        assert!(state.child.is_none());
        assert!(state.shutting_down);
        assert!(!state.termination_requested);
    }

    #[test]
    fn shutdown_cannot_return_during_atomic_spawn_and_registration() {
        let manager = SidecarManager::new();
        let child_state = Arc::new(Mutex::new(FakeChildState::default()));
        let (spawn_entered_tx, spawn_entered_rx) = mpsc::channel();
        let (release_spawn_tx, release_spawn_rx) = mpsc::channel();
        let spawn_manager = manager.clone();
        let spawn_child_state = child_state.clone();
        let spawn = thread::spawn(move || {
            spawn_manager
                .spawn_and_register_child(|| {
                    spawn_entered_tx.send(()).expect("signal spawn entry");
                    release_spawn_rx.recv().expect("release spawn");
                    let child: Box<dyn SidecarChildControl> = Box::new(FakeChild {
                        state: spawn_child_state,
                        write_error: None,
                        kill_error: None,
                    });
                    Ok(((), child))
                })
                .expect("spawn registration should succeed")
        });
        spawn_entered_rx
            .recv_timeout(Duration::from_secs(1))
            .expect("spawn closure should hold the child lock");

        let (shutdown_done_tx, shutdown_done_rx) = mpsc::channel();
        let shutdown_manager = manager.clone();
        let shutdown = thread::spawn(move || {
            shutdown_manager.shutdown_with_timeouts(Duration::ZERO, Duration::ZERO);
            shutdown_done_tx
                .send(())
                .expect("signal shutdown completion");
        });
        assert!(
            shutdown_done_rx
                .recv_timeout(Duration::from_millis(20))
                .is_err()
        );

        release_spawn_tx.send(()).expect("complete spawn");
        assert!(spawn.join().expect("spawn thread should finish").is_some());
        shutdown_done_rx
            .recv_timeout(Duration::from_secs(1))
            .expect("shutdown should finish after seeing the registered child");
        shutdown.join().expect("shutdown thread should finish");

        let state = child_state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        assert_eq!(state.writes, vec![b"shutdown\n".to_vec()]);
        assert_eq!(state.kill_calls, 1);
    }

    #[test]
    fn shutdown_waits_when_ready_timeout_already_owns_forced_stop() {
        let manager = SidecarManager::new();
        let child_state = install_fake_child(&manager, None, None);
        assert!(handle_ready_handshake_timeout(
            &manager,
            "ready timeout".to_owned(),
        ));
        assert_eq!(
            child_state
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .kill_calls,
            1
        );

        let (finished_tx, finished_rx) = mpsc::channel();
        let shutdown_manager = manager.clone();
        let shutdown = thread::spawn(move || {
            shutdown_manager.shutdown_with_timeouts(Duration::ZERO, Duration::from_millis(200));
            finished_tx
                .send(())
                .expect("shutdown completion signal should send");
        });

        assert!(finished_rx.recv_timeout(Duration::from_millis(20)).is_err());
        manager.mark_exited();
        finished_rx
            .recv_timeout(Duration::from_millis(200))
            .expect("shutdown should finish after a real termination event");
        shutdown.join().expect("shutdown thread should finish");
        assert!(manager.wait_for_exit(Duration::ZERO));
    }

    #[test]
    fn shutdown_reports_write_failure_and_waits_for_kill_confirmation() {
        let manager = SidecarManager::new();
        let child_state = install_fake_child(&manager, Some("broken pipe"), None);
        let shutdown_manager = manager.clone();
        let shutdown = thread::spawn(move || {
            shutdown_manager.shutdown_with_timeouts(Duration::ZERO, Duration::from_millis(200));
        });

        wait_until(|| {
            child_state
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .kill_calls
                == 1
        });
        assert!(!manager.wait_for_exit(Duration::ZERO));
        manager.mark_exited();
        shutdown.join().expect("shutdown thread should finish");

        let message = manager.status().message.unwrap_or_default();
        assert!(message.contains("无法向 Sidecar 写入关闭请求：broken pipe"));
        assert!(message.contains("等待真实退出确认"));
        assert!(!message.contains("已终止"));
    }

    #[test]
    fn shutdown_reports_kill_failure_without_faking_exit() {
        let manager = SidecarManager::new();
        let child_state = install_fake_child(&manager, Some("broken pipe"), Some("access denied"));

        manager.shutdown_with_timeouts(Duration::ZERO, Duration::ZERO);

        let state = child_state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        assert_eq!(state.writes, vec![b"shutdown\n".to_vec()]);
        assert_eq!(state.kill_calls, 1);
        let message = manager.status().message.unwrap_or_default();
        assert!(message.contains("broken pipe"));
        assert!(message.contains("强制终止 Sidecar 失败：access denied"));
        assert!(!manager.wait_for_exit(Duration::ZERO));
    }

    #[test]
    fn shutdown_reports_missing_confirmation_after_successful_kill() {
        let manager = SidecarManager::new();
        let child_state = install_fake_child(&manager, None, None);

        manager.shutdown_with_timeouts(Duration::ZERO, Duration::from_millis(10));

        let state = child_state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        assert_eq!(state.writes, vec![b"shutdown\n".to_vec()]);
        assert_eq!(state.kill_calls, 1);
        let message = manager.status().message.unwrap_or_default();
        assert!(message.contains("已发送强制终止请求"));
        assert!(message.contains("未收到 Sidecar 的真实退出确认"));
        assert!(!manager.wait_for_exit(Duration::ZERO));
    }

    #[test]
    fn restart_shutdown_requires_a_managed_child_and_waits_for_real_exit() {
        let external = SidecarManager::new();
        let error = external
            .shutdown_for_restart()
            .expect_err("external development Sidecar must not be restarted implicitly");
        assert!(error.contains("不由桌面应用管理"));
        assert!(!external.is_shutting_down());

        let manager = SidecarManager::new();
        let child_state = install_fake_child(&manager, None, None);
        let restart_manager = manager.clone();
        let restart = thread::spawn(move || restart_manager.shutdown_for_restart());
        wait_until(|| {
            !child_state
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .writes
                .is_empty()
        });
        assert!(!restart.is_finished());
        manager.mark_exited();
        restart
            .join()
            .expect("restart shutdown thread should finish")
            .expect("confirmed graceful exit should permit restart");
        let state = child_state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        assert_eq!(state.writes, vec![b"shutdown\n".to_vec()]);
        assert_eq!(state.kill_calls, 0);
    }
}
