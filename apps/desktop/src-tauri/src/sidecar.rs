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
const MAX_RESTART_ATTEMPTS: usize = 2;
const RESTART_BACKOFFS: [Duration; MAX_RESTART_ATTEMPTS] =
    [Duration::from_millis(500), Duration::from_secs(2)];
const RESTART_STABILITY_WINDOW: Duration = Duration::from_secs(30);

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct SidecarRuntimeStatus {
    pub phase: SidecarPhase,
    pub base_url: Option<String>,
    pub session_token: Option<String>,
    pub message: Option<String>,
    pub startup_stage: Option<String>,
    pub app_version: Option<String>,
    pub api_version: Option<String>,
    pub schema_version: Option<String>,
    pub generation: Option<u64>,
}

impl SidecarRuntimeStatus {
    fn starting(
        base_url: Option<String>,
        session_token: Option<String>,
        generation: Option<u64>,
    ) -> Self {
        Self {
            phase: SidecarPhase::Starting,
            base_url,
            session_token,
            message: Some("正在连接本地服务".to_owned()),
            startup_stage: Some("waiting_for_sidecar".to_owned()),
            app_version: None,
            api_version: None,
            schema_version: None,
            generation,
        }
    }
}

#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum SidecarPhase {
    Starting,
    Restarting,
    Ready,
    Error,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum StartupStage {
    WaitingForSidecar,
    AcquiringWorkspaceLock,
    CheckingPendingRestore,
    VerifyingRestorePackage,
    ApplyingRestore,
    VerifyingRestoredWorkspace,
    FinalizingRestore,
    OpeningDatabase,
    CreatingMigrationRollback,
    ApplyingDatabaseMigration,
    InitializingWorkspace,
    StartingLocalApi,
}

impl StartupStage {
    fn parse(raw: &str) -> Option<Self> {
        Some(match raw {
            "waiting_for_sidecar" => Self::WaitingForSidecar,
            "acquiring_workspace_lock" => Self::AcquiringWorkspaceLock,
            "checking_pending_restore" => Self::CheckingPendingRestore,
            "verifying_restore_package" => Self::VerifyingRestorePackage,
            "applying_restore" => Self::ApplyingRestore,
            "verifying_restored_workspace" => Self::VerifyingRestoredWorkspace,
            "finalizing_restore" => Self::FinalizingRestore,
            "opening_database" => Self::OpeningDatabase,
            "creating_migration_rollback" => Self::CreatingMigrationRollback,
            "applying_database_migration" => Self::ApplyingDatabaseMigration,
            "initializing_workspace" => Self::InitializingWorkspace,
            "starting_local_api" => Self::StartingLocalApi,
            _ => return None,
        })
    }

    fn as_str(self) -> &'static str {
        match self {
            Self::WaitingForSidecar => "waiting_for_sidecar",
            Self::AcquiringWorkspaceLock => "acquiring_workspace_lock",
            Self::CheckingPendingRestore => "checking_pending_restore",
            Self::VerifyingRestorePackage => "verifying_restore_package",
            Self::ApplyingRestore => "applying_restore",
            Self::VerifyingRestoredWorkspace => "verifying_restored_workspace",
            Self::FinalizingRestore => "finalizing_restore",
            Self::OpeningDatabase => "opening_database",
            Self::CreatingMigrationRollback => "creating_migration_rollback",
            Self::ApplyingDatabaseMigration => "applying_database_migration",
            Self::InitializingWorkspace => "initializing_workspace",
            Self::StartingLocalApi => "starting_local_api",
        }
    }

    fn message(self) -> &'static str {
        match self {
            Self::WaitingForSidecar => "正在连接本地服务",
            Self::AcquiringWorkspaceLock => "正在确认本地工作区未被其他进程占用",
            Self::CheckingPendingRestore => "正在检查是否有待完成的本地恢复",
            Self::VerifyingRestorePackage => "正在验证待恢复备份",
            Self::ApplyingRestore => "正在恢复本地数据，请勿退出应用",
            Self::VerifyingRestoredWorkspace => "正在验证已恢复的本地数据",
            Self::FinalizingRestore => "正在完成恢复清理",
            Self::OpeningDatabase => "正在打开本地数据库",
            Self::CreatingMigrationRollback => "正在为数据升级创建安全回滚点",
            Self::ApplyingDatabaseMigration => "正在更新本地数据结构",
            Self::InitializingWorkspace => "正在初始化本地工作区",
            Self::StartingLocalApi => "正在启动本地 API",
        }
    }
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

struct ManagedChild {
    generation: u64,
    control: Box<dyn SidecarChildControl>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct ChildExit {
    generation: u64,
    code: Option<i32>,
    signal: Option<i32>,
}

impl ChildExit {
    fn is_clean(self) -> bool {
        self.code == Some(0) && self.signal.is_none()
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct RestartReservation {
    ticket: u64,
    attempt: usize,
    delay: Duration,
}

#[derive(Default)]
struct ManagedChildState {
    child: Option<ManagedChild>,
    active_generation: Option<u64>,
    next_generation: u64,
    managed: bool,
    shutting_down: bool,
    shutdown_generation: Option<u64>,
    restart_attempts: usize,
    next_restart_ticket: u64,
    scheduled_restart: Option<u64>,
}

enum ChildKillOutcome {
    Requested,
    Missing,
    ShutdownOwnsChild,
    Failed(String),
}

enum ShutdownStart {
    Owner(Option<u64>),
    Waiter,
}

struct SidecarManagerInner {
    runtime: RwLock<SidecarRuntimeStatus>,
    child: Mutex<ManagedChildState>,
    exited: (Mutex<Option<ChildExit>>, Condvar),
    shutdown_complete: (Mutex<bool>, Condvar),
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
                runtime: RwLock::new(SidecarRuntimeStatus::starting(None, None, None)),
                child: Mutex::new(ManagedChildState::default()),
                exited: (Mutex::new(None), Condvar::new()),
                shutdown_complete: (Mutex::new(false), Condvar::new()),
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

    fn configure_bundled(&self) {
        self.inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .managed = true;
    }

    fn set_starting(
        &self,
        base_url: Option<String>,
        session_token: Option<String>,
        generation: Option<u64>,
    ) {
        self.log_event(DesktopEvent::SidecarStarting);
        self.update(|runtime| {
            *runtime = SidecarRuntimeStatus::starting(base_url, session_token, generation);
        });
    }

    fn set_starting_if_current(&self, generation: u64, session_token: String) -> bool {
        let state = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.active_generation != Some(generation) || state.shutting_down {
            return false;
        }
        self.log_event(DesktopEvent::SidecarStarting);
        self.update(|runtime| {
            *runtime = SidecarRuntimeStatus::starting(None, Some(session_token), Some(generation));
        });
        true
    }

    fn set_startup_stage_if_current(&self, generation: u64, stage: StartupStage) -> bool {
        let state = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.active_generation != Some(generation) || state.shutting_down {
            return false;
        }
        self.update(|runtime| {
            if runtime.phase == SidecarPhase::Starting {
                runtime.startup_stage = Some(stage.as_str().to_owned());
                runtime.message = Some(stage.message().to_owned());
            }
        });
        true
    }

    fn set_restarting(&self, reservation: RestartReservation) -> bool {
        let state = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.scheduled_restart != Some(reservation.ticket) || state.shutting_down {
            return false;
        }
        self.log_event(DesktopEvent::SidecarRestartScheduled);
        self.update(|runtime| {
            runtime.phase = SidecarPhase::Restarting;
            runtime.base_url = None;
            runtime.session_token = None;
            runtime.startup_stage = None;
            runtime.message = Some(format!(
                "本地服务意外退出，正在进行第 {}/{} 次自动恢复",
                reservation.attempt, MAX_RESTART_ATTEMPTS
            ));
        });
        true
    }

    fn set_ready(&self, generation: u64, ready: &ReadyLine, session_token: Option<String>) -> bool {
        let state = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.active_generation != Some(generation) || state.shutting_down {
            return false;
        }
        self.log_event(DesktopEvent::SidecarReady);
        self.update(|runtime| {
            runtime.phase = SidecarPhase::Ready;
            runtime.base_url = Some(ready.base_url.clone());
            runtime.session_token = session_token;
            runtime.message = None;
            runtime.startup_stage = None;
            runtime.app_version = ready.app_version.clone();
            runtime.api_version = ready.api_version.clone();
            runtime.schema_version = ready.schema_version.clone();
            runtime.generation = Some(generation);
        });
        true
    }

    fn set_external_ready(&self, base_url: String, session_token: Option<String>) {
        self.log_event(DesktopEvent::SidecarReady);
        self.update(|runtime| {
            runtime.phase = SidecarPhase::Ready;
            runtime.base_url = Some(base_url);
            runtime.session_token = session_token;
            runtime.message = None;
            runtime.startup_stage = None;
            runtime.generation = None;
        });
    }

    fn set_error(&self, message: impl Into<String>) {
        self.log_event(DesktopEvent::SidecarError);
        self.update(|runtime| {
            runtime.phase = SidecarPhase::Error;
            runtime.base_url = None;
            runtime.session_token = None;
            runtime.message = Some(message.into());
            runtime.startup_stage = None;
        });
    }

    fn set_error_unless_shutting_down(&self, message: impl Into<String>) -> bool {
        let state = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.shutting_down {
            return false;
        }
        self.log_event(DesktopEvent::SidecarError);
        self.update(|runtime| {
            runtime.phase = SidecarPhase::Error;
            runtime.base_url = None;
            runtime.session_token = None;
            runtime.message = Some(message.into());
            runtime.startup_stage = None;
        });
        true
    }

    fn set_error_if_current(&self, generation: u64, message: impl Into<String>) -> bool {
        let state = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.active_generation != Some(generation) || state.shutting_down {
            return false;
        }
        self.log_event(DesktopEvent::SidecarError);
        self.update(|runtime| {
            runtime.phase = SidecarPhase::Error;
            runtime.base_url = None;
            runtime.session_token = None;
            runtime.message = Some(message.into());
        });
        true
    }

    fn append_error(&self, message: impl Into<String>) {
        self.log_event(DesktopEvent::SidecarError);
        let message = message.into();
        self.update(|runtime| {
            let existing = runtime.message.take();
            runtime.phase = SidecarPhase::Error;
            runtime.base_url = None;
            runtime.session_token = None;
            runtime.message = Some(match existing {
                Some(existing) if !existing.is_empty() => format!("{existing}；{message}"),
                _ => message,
            });
        });
    }

    fn append_error_if_current(&self, generation: u64, message: impl Into<String>) -> bool {
        let state = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.active_generation != Some(generation) || state.shutting_down {
            return false;
        }
        self.log_event(DesktopEvent::SidecarError);
        let message = message.into();
        self.update(|runtime| {
            let existing = runtime.message.take();
            runtime.phase = SidecarPhase::Error;
            runtime.base_url = None;
            runtime.session_token = None;
            runtime.message = Some(match existing {
                Some(existing) if !existing.is_empty() => format!("{existing}；{message}"),
                _ => message,
            });
        });
        true
    }

    fn spawn_and_register_child<T>(
        &self,
        spawn: impl FnOnce() -> Result<(T, Box<dyn SidecarChildControl>), String>,
    ) -> Result<Option<(T, u64)>, String> {
        let mut state = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.shutting_down {
            return Ok(None);
        }
        if state.child.is_some() || state.active_generation.is_some() {
            return Err("上一代 Sidecar 尚未确认退出，已拒绝启动新进程".to_owned());
        }
        let (events, child) = spawn()?;
        let generation = state.next_generation.saturating_add(1);
        state.next_generation = generation;
        state.active_generation = Some(generation);
        state.child = Some(ManagedChild {
            generation,
            control: child,
        });
        Ok(Some((events, generation)))
    }

    fn mark_exited(&self, generation: u64, code: Option<i32>, signal: Option<i32>) -> bool {
        let mut state = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.active_generation != Some(generation) {
            return false;
        }
        let (exited, changed) = &self.inner.exited;
        let mut exited = exited
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        *exited = Some(ChildExit {
            generation,
            code,
            signal,
        });
        if !state.shutting_down {
            self.log_event(DesktopEvent::SidecarError);
            self.update(|runtime| {
                runtime.base_url = None;
                runtime.session_token = None;
                if runtime.phase != SidecarPhase::Error {
                    runtime.phase = SidecarPhase::Error;
                    runtime.message = Some(format!(
                        "Sidecar 已退出（code={code:?}, signal={signal:?}）"
                    ));
                }
            });
        }
        state.child = None;
        state.active_generation = None;
        changed.notify_all();
        true
    }

    fn wait_for_exit(&self, generation: u64, timeout: Duration) -> Option<ChildExit> {
        let (exited, changed) = &self.inner.exited;
        let exited = exited
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if exited.is_some_and(|outcome| outcome.generation == generation) {
            return *exited;
        }
        match changed.wait_timeout_while(exited, timeout, |exited| {
            !exited.is_some_and(|outcome| outcome.generation == generation)
        }) {
            Ok((exited, _)) => exited
                .as_ref()
                .filter(|outcome| outcome.generation == generation)
                .copied(),
            Err(poisoned) => {
                let (exited, _) = poisoned.into_inner();
                exited
                    .as_ref()
                    .filter(|outcome| outcome.generation == generation)
                    .copied()
            }
        }
    }

    fn request_child_kill(&self, generation: u64) -> ChildKillOutcome {
        let child = {
            let mut state = self
                .inner
                .child
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            if state.shutting_down {
                return ChildKillOutcome::ShutdownOwnsChild;
            }
            if state.active_generation != Some(generation) {
                return ChildKillOutcome::Missing;
            }
            match state.child.take() {
                Some(child) if child.generation == generation => Some(child.control),
                Some(child) => {
                    state.child = Some(child);
                    None
                }
                None => None,
            }
        };
        let Some(child) = child else {
            return ChildKillOutcome::Missing;
        };
        match child.kill() {
            Ok(()) => ChildKillOutcome::Requested,
            Err(error) => ChildKillOutcome::Failed(error),
        }
    }

    fn reserve_restart(&self) -> Result<Option<RestartReservation>, ()> {
        let mut state = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if !state.managed
            || state.shutting_down
            || state.active_generation.is_some()
            || state.child.is_some()
            || state.scheduled_restart.is_some()
        {
            return Ok(None);
        }
        if state.restart_attempts >= MAX_RESTART_ATTEMPTS {
            return Err(());
        }
        state.restart_attempts += 1;
        state.next_restart_ticket = state.next_restart_ticket.saturating_add(1);
        let reservation = RestartReservation {
            ticket: state.next_restart_ticket,
            attempt: state.restart_attempts,
            delay: RESTART_BACKOFFS[state.restart_attempts - 1],
        };
        state.scheduled_restart = Some(reservation.ticket);
        Ok(Some(reservation))
    }

    fn claim_restart(&self, ticket: u64) -> bool {
        let mut state = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.scheduled_restart != Some(ticket)
            || state.shutting_down
            || state.active_generation.is_some()
            || state.child.is_some()
        {
            return false;
        }
        state.scheduled_restart = None;
        true
    }

    fn reset_restart_budget_if_stable(&self, generation: u64) -> bool {
        let mut state = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.active_generation != Some(generation)
            || state.shutting_down
            || state.child.is_none()
        {
            return false;
        }
        let runtime = self
            .inner
            .runtime
            .read()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if runtime.phase != SidecarPhase::Ready || runtime.generation != Some(generation) {
            return false;
        }
        state.restart_attempts = 0;
        true
    }

    pub fn shutdown(&self) {
        self.log_event(DesktopEvent::SidecarShutdownStarted);
        self.shutdown_with_timeouts(GRACEFUL_SHUTDOWN_TIMEOUT, FORCED_TERMINATION_TIMEOUT);
        self.log_event(DesktopEvent::SidecarShutdownFinished);
    }

    pub fn shutdown_for_restart(&self) -> Result<(), String> {
        let (managed, shutting_down, previous_generation, shutdown_complete) = {
            let state = self
                .inner
                .child
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            let complete = if state.shutting_down {
                *self
                    .inner
                    .shutdown_complete
                    .0
                    .lock()
                    .unwrap_or_else(|poisoned| poisoned.into_inner())
            } else {
                false
            };
            (
                state.managed,
                state.shutting_down,
                state.shutdown_generation,
                complete,
            )
        };
        if !managed {
            return Err("当前 Sidecar 不由桌面应用管理，请手动重启开发服务".to_owned());
        }
        if shutting_down {
            if !shutdown_complete {
                return Err("应用正在退出，已取消重复的重启请求".to_owned());
            }
            return self.validate_restart_shutdown(previous_generation);
        }
        let generation = match self.begin_shutdown() {
            ShutdownStart::Owner(generation) => generation,
            ShutdownStart::Waiter => {
                return Err("应用正在退出，已取消重复的重启请求".to_owned());
            }
        };
        self.log_event(DesktopEvent::SidecarShutdownStarted);
        self.perform_shutdown(
            generation,
            GRACEFUL_SHUTDOWN_TIMEOUT,
            FORCED_TERMINATION_TIMEOUT,
        );
        self.finish_shutdown();
        self.log_event(DesktopEvent::SidecarShutdownFinished);

        self.validate_restart_shutdown(generation)
    }

    fn validate_restart_shutdown(&self, generation: Option<u64>) -> Result<(), String> {
        let Some(generation) = generation else {
            return Ok(());
        };
        match self.wait_for_exit(generation, Duration::ZERO) {
            Some(outcome) if outcome.is_clean() => Ok(()),
            Some(outcome) => Err(format!(
                "Sidecar 未安全完成关闭（code={:?}, signal={:?}），已取消应用重启",
                outcome.code, outcome.signal
            )),
            None => Err(self
                .status()
                .message
                .unwrap_or_else(|| "Sidecar 未确认安全退出，已取消应用重启".to_owned())),
        }
    }

    fn shutdown_with_timeouts(&self, graceful_timeout: Duration, forced_timeout: Duration) {
        match self.begin_shutdown() {
            ShutdownStart::Owner(generation) => {
                self.perform_shutdown(generation, graceful_timeout, forced_timeout);
                self.finish_shutdown();
            }
            ShutdownStart::Waiter => self.wait_for_shutdown_complete(),
        }
    }

    fn begin_shutdown(&self) -> ShutdownStart {
        let mut state = self
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if state.shutting_down {
            return ShutdownStart::Waiter;
        }
        state.shutting_down = true;
        state.scheduled_restart = None;
        let generation = state.active_generation;
        state.shutdown_generation = generation;
        self.update(|runtime| {
            runtime.phase = SidecarPhase::Error;
            runtime.base_url = None;
            runtime.session_token = None;
            runtime.message = Some("本地服务正在安全关闭".to_owned());
        });
        *self
            .inner
            .shutdown_complete
            .0
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner()) = false;
        ShutdownStart::Owner(generation)
    }

    fn finish_shutdown(&self) {
        let (complete, changed) = &self.inner.shutdown_complete;
        *complete
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner()) = true;
        changed.notify_all();
    }

    fn perform_shutdown(
        &self,
        generation: Option<u64>,
        graceful_timeout: Duration,
        forced_timeout: Duration,
    ) {
        let child = {
            let mut state = self
                .inner
                .child
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            match (state.child.take(), generation) {
                (Some(child), Some(generation)) if child.generation == generation => {
                    Some(child.control)
                }
                (Some(child), _) => {
                    state.child = Some(child);
                    None
                }
                (None, _) => None,
            }
        };

        let Some(generation) = generation else {
            return;
        };

        if child.is_none() {
            if self.wait_for_exit(generation, forced_timeout).is_none() {
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
                    if self.wait_for_exit(generation, graceful_timeout).is_some() {
                        return;
                    }
                    "Sidecar 未在优雅关闭期限内退出".to_owned()
                }
                Err(error) => {
                    let message = format!("无法向 Sidecar 写入关闭请求：{error}");
                    self.set_error(message.clone());
                    if self.wait_for_exit(generation, Duration::ZERO).is_some() {
                        return;
                    }
                    message
                }
            };
            if self.wait_for_exit(generation, Duration::ZERO).is_some() {
                return;
            }
            match child.kill() {
                Ok(()) => {
                    self.set_error(format!(
                        "{stop_context}；已发送强制终止请求，正在等待真实退出确认"
                    ));
                    if self.wait_for_exit(generation, forced_timeout).is_none() {
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

    fn wait_for_shutdown_complete(&self) {
        let (complete, changed) = &self.inner.shutdown_complete;
        let complete = complete
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if *complete {
            return;
        }
        drop(
            changed
                .wait_while(complete, |complete| !*complete)
                .unwrap_or_else(|poisoned| poisoned.into_inner()),
        );
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
            manager.configure_bundled();
            launch_bundled_sidecar(app.clone(), manager);
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

    manager.set_starting(Some(base_url.clone()), token.clone(), None);
    tauri::async_runtime::spawn(async move {
        match verify_health(&base_url, token.as_deref()).await {
            Ok(()) => manager.set_external_ready(base_url, token),
            Err(error) => manager.set_error(format!("开发 Sidecar 健康检查失败：{error}")),
        }
    });
}

fn launch_bundled_sidecar(app: AppHandle, manager: SidecarManager) {
    if let Err(error) = spawn_bundled_sidecar(&app, manager.clone()) {
        if manager.set_error_unless_shutting_down(error) {
            schedule_bundled_restart(app, manager);
        }
    }
}

fn schedule_bundled_restart(app: AppHandle, manager: SidecarManager) {
    match manager.reserve_restart() {
        Ok(Some(reservation)) => {
            if !manager.set_restarting(reservation) {
                return;
            }
            tauri::async_runtime::spawn(async move {
                tokio::time::sleep(reservation.delay).await;
                if manager.claim_restart(reservation.ticket) {
                    launch_bundled_sidecar(app, manager);
                }
            });
        }
        Ok(None) => {}
        Err(()) => {
            if manager.set_error_unless_shutting_down(format!(
                "本地服务连续恢复 {} 次仍未就绪，已停止自动重试",
                MAX_RESTART_ATTEMPTS
            )) {
                manager.log_event(DesktopEvent::SidecarRestartExhausted);
            }
        }
    }
}

fn schedule_restart_budget_reset(manager: SidecarManager, generation: u64) {
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(RESTART_STABILITY_WINDOW).await;
        manager.reset_restart_budget_if_stable(generation);
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
        .env("OPC_EXIT_ON_STDIN_CLOSE", "true")
        .env(
            "OPC_ALLOWED_ORIGINS",
            "http://tauri.localhost,https://tauri.localhost,tauri://localhost",
        );

    let Some((mut events, generation)) = manager.spawn_and_register_child(|| {
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
    manager.set_starting_if_current(generation, token.clone());
    let watcher_app = app.clone();

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
                        manager.append_error_if_current(
                            generation,
                            format!(
                                "在强制停止期限 {} 毫秒内未收到 Sidecar 的真实退出确认",
                                FORCED_TERMINATION_TIMEOUT.as_millis()
                            ),
                        );
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
                        if handle_ready_handshake_timeout(&manager, generation, error) {
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
                    match parse_sidecar_stdout_line(line.trim()) {
                        Ok(SidecarStdoutLine::Startup(stage)) => {
                            manager.set_startup_stage_if_current(generation, stage);
                        }
                        Ok(SidecarStdoutLine::Ready(ready)) => {
                            reached_ready = true;
                            match verify_health(&ready.base_url, Some(&token)).await {
                                Ok(()) => {
                                    if manager.set_ready(generation, &ready, Some(token.clone())) {
                                        schedule_restart_budget_reset(manager.clone(), generation);
                                    }
                                }
                                Err(error) => {
                                    if begin_forced_stop(
                                        &manager,
                                        generation,
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
                                generation,
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
                    if begin_forced_stop(&manager, generation, format!("Sidecar 进程错误：{error}"))
                    {
                        forced_termination_deadline =
                            Some(tokio::time::Instant::now() + FORCED_TERMINATION_TIMEOUT);
                    }
                    stopping_after_error = true;
                }
                CommandEvent::Terminated(payload) => {
                    if manager.mark_exited(generation, payload.code, payload.signal)
                        && !manager.is_shutting_down()
                    {
                        schedule_bundled_restart(watcher_app.clone(), manager.clone());
                    }
                    break;
                }
                _ => {}
            }
        }

        if stream_closed_without_termination {
            manager.append_error_if_current(generation, "Sidecar 事件流已关闭，未收到真实退出确认");
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

fn handle_ready_handshake_timeout(
    manager: &SidecarManager,
    generation: u64,
    error: String,
) -> bool {
    begin_forced_stop(manager, generation, error)
}

fn begin_forced_stop(manager: &SidecarManager, generation: u64, error: String) -> bool {
    match manager.request_child_kill(generation) {
        ChildKillOutcome::ShutdownOwnsChild => false,
        ChildKillOutcome::Requested => {
            manager.set_error_if_current(
                generation,
                format!("{error}；已发送终止请求，正在等待真实退出确认"),
            );
            true
        }
        ChildKillOutcome::Missing => {
            manager.set_error_if_current(
                generation,
                format!("{error}；没有可用的子进程句柄，无法发送终止请求"),
            );
            true
        }
        ChildKillOutcome::Failed(kill_error) => {
            manager.set_error_if_current(
                generation,
                format!("{error}；终止 Sidecar 失败：{kill_error}"),
            );
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
    stage: Option<String>,
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

#[derive(Debug, PartialEq, Eq)]
enum SidecarStdoutLine {
    Startup(StartupStage),
    Ready(ReadyLine),
}

fn parse_sidecar_stdout_line(line: &str) -> Result<SidecarStdoutLine, String> {
    let raw: RawReadyLine =
        serde_json::from_str(line).map_err(|error| format!("不是合法 JSON：{error}"))?;
    if raw.event.as_deref() == Some("startup") {
        let stage = raw
            .stage
            .as_deref()
            .and_then(StartupStage::parse)
            .ok_or("startup 阶段不受支持")?;
        return Ok(SidecarStdoutLine::Startup(stage));
    }
    parse_ready_payload(raw).map(SidecarStdoutLine::Ready)
}

fn parse_ready_line(line: &str) -> Result<ReadyLine, String> {
    let raw: RawReadyLine =
        serde_json::from_str(line).map_err(|error| format!("不是合法 JSON：{error}"))?;
    parse_ready_payload(raw)
}

fn parse_ready_payload(raw: RawReadyLine) -> Result<ReadyLine, String> {
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
        MAX_RESTART_ATTEMPTS, RESTART_BACKOFFS, SidecarChildControl, SidecarManager, SidecarPhase,
        SidecarRuntimeStatus, SidecarStdoutLine, StartupStage, handle_ready_handshake_timeout,
        parse_ready_line, parse_sidecar_stdout_line, receive_before_ready_deadline,
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
    ) -> (Arc<Mutex<FakeChildState>>, u64) {
        manager.configure_bundled();
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
        let (_, generation) = registered.expect("fake child should be registered");
        (state, generation)
    }

    fn wait_until(mut predicate: impl FnMut() -> bool) {
        let deadline = Instant::now() + Duration::from_secs(5);
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
    fn parses_bounded_startup_progress_without_treating_it_as_ready() {
        let progress =
            parse_sidecar_stdout_line(r#"{"event":"startup","stage":"verifying_restore_package"}"#)
                .expect("startup progress should parse");
        assert_eq!(
            progress,
            SidecarStdoutLine::Startup(StartupStage::VerifyingRestorePackage)
        );
        assert!(
            parse_ready_line(r#"{"event":"startup","stage":"verifying_restore_package"}"#).is_err()
        );
        assert!(parse_sidecar_stdout_line(r#"{"event":"startup","stage":"unknown"}"#).is_err());
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
            startup_stage: None,
            app_version: Some("0.1.0".to_owned()),
            api_version: Some("v1".to_owned()),
            schema_version: Some("1".to_owned()),
            generation: Some(7),
        };

        let json = serde_json::to_value(status).expect("status should serialize");
        assert_eq!(json["phase"], "ready");
        assert_eq!(json["baseUrl"], "http://127.0.0.1:9876");
        assert_eq!(json["sessionToken"], "token");
        assert_eq!(json["startupStage"], serde_json::Value::Null);
        assert_eq!(json["schemaVersion"], "1");
        assert_eq!(json["generation"], 7);
    }

    #[test]
    fn runtime_state_moves_from_starting_to_ready_and_error() {
        let manager = SidecarManager::new();
        assert_eq!(manager.status().phase, SidecarPhase::Starting);

        manager.set_starting(
            Some("http://127.0.0.1:9876".to_owned()),
            Some("token".to_owned()),
            None,
        );
        manager.set_external_ready("http://127.0.0.1:9876".to_owned(), Some("token".to_owned()));
        let ready = manager.status();
        assert_eq!(ready.phase, SidecarPhase::Ready);
        assert_eq!(ready.session_token.as_deref(), Some("token"));

        manager.set_error("sidecar stopped");
        let error = manager.status();
        assert_eq!(error.phase, SidecarPhase::Error);
        assert_eq!(error.message.as_deref(), Some("sidecar stopped"));
        assert!(error.base_url.is_none());
        assert!(error.session_token.is_none());
    }

    #[test]
    fn living_silent_sidecar_hits_ready_handshake_timeout() {
        let manager = SidecarManager::new();
        let (child_state, generation) = install_fake_child(&manager, None, None);
        let timeout = Duration::from_millis(20);

        let timed_out = tauri::async_runtime::block_on(async {
            let deadline = tokio::time::Instant::now() + timeout;
            receive_before_ready_deadline(future::pending::<()>(), deadline, timeout).await
        });

        let error = timed_out.expect_err("an open but silent event stream must time out");
        assert!(error.contains("20 毫秒"));
        assert!(handle_ready_handshake_timeout(&manager, generation, error));

        let status = manager.status();
        assert_eq!(status.phase, SidecarPhase::Error);
        assert!(
            status
                .message
                .as_deref()
                .is_some_and(|message| message.contains("ready 握手")
                    && message.contains("正在等待真实退出确认")
                    && !message.contains("已终止"))
        );
        assert!(manager.wait_for_exit(generation, Duration::ZERO).is_none());
        assert!(
            manager
                .inner
                .child
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .child
                .is_none()
        );
        assert_eq!(
            child_state
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .kill_calls,
            1
        );
    }

    #[test]
    fn ready_timeout_keeps_receiver_alive_while_shutdown_owns_child() {
        let manager = SidecarManager::new();
        let (child_state, generation) = install_fake_child(&manager, None, None);
        let shutdown_manager = manager.clone();
        let shutdown = thread::spawn(move || {
            shutdown_manager.shutdown_with_timeouts(Duration::from_secs(5), Duration::from_secs(1));
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
            generation,
            "timeout during shutdown".to_owned(),
        ));
        assert_eq!(manager.status().phase, SidecarPhase::Error);
        assert_eq!(
            manager.status().message.as_deref(),
            Some("本地服务正在安全关闭")
        );
        assert!(manager.wait_for_exit(generation, Duration::ZERO).is_none());

        // Simulate the receiver continuing to drain and observing Terminated.
        assert!(manager.mark_exited(generation, Some(0), None));
        shutdown.join().expect("shutdown thread should finish");
        let state = child_state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        assert_eq!(state.writes, vec![b"shutdown\n".to_vec()]);
        assert_eq!(state.kill_calls, 0);
        assert!(manager.wait_for_exit(generation, Duration::ZERO).is_some());
    }

    #[test]
    fn shutdown_before_spawn_prevents_child_creation() {
        let manager = SidecarManager::new();
        manager.shutdown_with_timeouts(Duration::ZERO, Duration::ZERO);
        assert!(!manager.set_error_unless_shutting_down("late launch failure"));
        assert_eq!(
            manager.status().message.as_deref(),
            Some("本地服务正在安全关闭")
        );
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
        let (child_state, generation) = install_fake_child(&manager, None, None);
        assert!(handle_ready_handshake_timeout(
            &manager,
            generation,
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
            shutdown_manager.shutdown_with_timeouts(Duration::ZERO, Duration::from_secs(5));
            finished_tx
                .send(())
                .expect("shutdown completion signal should send");
        });

        assert!(finished_rx.recv_timeout(Duration::from_millis(20)).is_err());
        assert!(manager.mark_exited(generation, Some(0), None));
        finished_rx
            .recv_timeout(Duration::from_secs(5))
            .expect("shutdown should finish after a real termination event");
        shutdown.join().expect("shutdown thread should finish");
        assert!(manager.wait_for_exit(generation, Duration::ZERO).is_some());
    }

    #[test]
    fn shutdown_reports_write_failure_and_waits_for_kill_confirmation() {
        let manager = SidecarManager::new();
        let (child_state, generation) = install_fake_child(&manager, Some("broken pipe"), None);
        let shutdown_manager = manager.clone();
        let shutdown = thread::spawn(move || {
            shutdown_manager.shutdown_with_timeouts(Duration::ZERO, Duration::from_secs(5));
        });

        wait_until(|| {
            child_state
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .kill_calls
                == 1
        });
        assert!(manager.wait_for_exit(generation, Duration::ZERO).is_none());
        assert!(manager.mark_exited(generation, Some(0), None));
        shutdown.join().expect("shutdown thread should finish");

        let message = manager.status().message.unwrap_or_default();
        assert!(message.contains("无法向 Sidecar 写入关闭请求：broken pipe"));
        assert!(message.contains("等待真实退出确认"));
        assert!(!message.contains("已终止"));
    }

    #[test]
    fn shutdown_reports_kill_failure_without_faking_exit() {
        let manager = SidecarManager::new();
        let (child_state, generation) =
            install_fake_child(&manager, Some("broken pipe"), Some("access denied"));

        manager.shutdown_with_timeouts(Duration::ZERO, Duration::ZERO);

        let state = child_state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        assert_eq!(state.writes, vec![b"shutdown\n".to_vec()]);
        assert_eq!(state.kill_calls, 1);
        let message = manager.status().message.unwrap_or_default();
        assert!(message.contains("broken pipe"));
        assert!(message.contains("强制终止 Sidecar 失败：access denied"));
        assert!(manager.wait_for_exit(generation, Duration::ZERO).is_none());
    }

    #[test]
    fn shutdown_reports_missing_confirmation_after_successful_kill() {
        let manager = SidecarManager::new();
        let (child_state, generation) = install_fake_child(&manager, None, None);

        manager.shutdown_with_timeouts(Duration::ZERO, Duration::from_millis(10));

        let state = child_state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        assert_eq!(state.writes, vec![b"shutdown\n".to_vec()]);
        assert_eq!(state.kill_calls, 1);
        let message = manager.status().message.unwrap_or_default();
        assert!(message.contains("已发送强制终止请求"));
        assert!(message.contains("未收到 Sidecar 的真实退出确认"));
        assert!(manager.wait_for_exit(generation, Duration::ZERO).is_none());
    }

    #[test]
    fn restart_shutdown_requires_managed_mode_and_waits_for_clean_exit() {
        let external = SidecarManager::new();
        let error = external
            .shutdown_for_restart()
            .expect_err("external development Sidecar must not be restarted implicitly");
        assert!(error.contains("不由桌面应用管理"));
        assert!(!external.is_shutting_down());

        let manager = SidecarManager::new();
        let (child_state, generation) = install_fake_child(&manager, None, None);
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
        assert!(manager.mark_exited(generation, Some(0), None));
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

    #[test]
    fn managed_restart_is_allowed_when_no_child_was_started() {
        let manager = SidecarManager::new();
        manager.configure_bundled();

        manager
            .shutdown_for_restart()
            .expect("a bundled launch failure has no live child to stop");
    }

    #[test]
    fn restart_rejects_a_nonzero_graceful_exit() {
        let manager = SidecarManager::new();
        let (child_state, generation) = install_fake_child(&manager, None, None);
        let restart_manager = manager.clone();
        let restart = thread::spawn(move || restart_manager.shutdown_for_restart());
        wait_until(|| {
            !child_state
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .writes
                .is_empty()
        });

        assert!(manager.mark_exited(generation, Some(1), None));
        let error = restart
            .join()
            .expect("restart shutdown thread should finish")
            .expect_err("a failed WAL checkpoint must block application restart");
        assert!(error.contains("code=Some(1)"));
        assert!(error.contains("取消应用重启"));
    }

    #[test]
    fn restart_budget_is_bounded_and_uses_deterministic_backoff() {
        let manager = SidecarManager::new();
        manager.configure_bundled();

        for attempt in 1..=MAX_RESTART_ATTEMPTS {
            let reservation = manager
                .reserve_restart()
                .expect("budget should remain")
                .expect("one retry should be reserved");
            assert_eq!(reservation.attempt, attempt);
            assert_eq!(reservation.delay, RESTART_BACKOFFS[attempt - 1]);
            assert!(
                manager
                    .reserve_restart()
                    .expect("scheduled retry")
                    .is_none()
            );
            assert!(manager.claim_restart(reservation.ticket));
        }

        assert!(manager.reserve_restart().is_err());
    }

    #[test]
    fn stable_generation_resets_the_restart_budget_without_reviving_old_generations() {
        let manager = SidecarManager::new();
        manager.configure_bundled();
        let reservation = manager
            .reserve_restart()
            .expect("budget should remain")
            .expect("retry should be reserved");
        assert!(manager.claim_restart(reservation.ticket));
        let (_, generation) = install_fake_child(&manager, None, None);

        assert!(!manager.reset_restart_budget_if_stable(generation));
        manager.update(|runtime| {
            runtime.phase = SidecarPhase::Ready;
            runtime.generation = Some(generation);
        });
        assert!(manager.reset_restart_budget_if_stable(generation));
        assert_eq!(
            manager
                .reserve_restart()
                .expect("an active generation cannot reserve a retry"),
            None
        );
        assert!(manager.mark_exited(generation, Some(1), None));
        let fresh = manager
            .reserve_restart()
            .expect("stable runtime should restore the retry budget")
            .expect("a new crash can reserve the first retry again");
        assert_eq!(fresh.attempt, 1);
        assert!(!manager.reset_restart_budget_if_stable(generation));
    }

    #[test]
    fn shutdown_cancels_a_pending_restart() {
        let manager = SidecarManager::new();
        manager.configure_bundled();
        let reservation = manager
            .reserve_restart()
            .expect("budget should remain")
            .expect("retry should be reserved");
        assert!(manager.set_restarting(reservation));

        manager.shutdown_with_timeouts(Duration::ZERO, Duration::ZERO);

        assert!(!manager.claim_restart(reservation.ticket));
        assert!(manager.is_shutting_down());
    }

    #[test]
    fn manual_restart_waits_for_a_claimed_retry_to_finish_registration() {
        let manager = SidecarManager::new();
        manager.configure_bundled();
        let reservation = manager
            .reserve_restart()
            .expect("budget should remain")
            .expect("retry should be reserved");
        assert!(manager.claim_restart(reservation.ticket));

        let child_state = Arc::new(Mutex::new(FakeChildState::default()));
        let (spawn_entered_tx, spawn_entered_rx) = mpsc::channel();
        let (release_spawn_tx, release_spawn_rx) = mpsc::channel();
        let (generation_tx, generation_rx) = mpsc::channel();
        let spawn_manager = manager.clone();
        let spawn_child_state = child_state.clone();
        let spawn = thread::spawn(move || {
            let registered = spawn_manager
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
                .expect("claimed retry spawn should succeed")
                .expect("claimed retry should register a child");
            generation_tx
                .send(registered.1)
                .expect("publish registered generation");
        });
        spawn_entered_rx
            .recv_timeout(Duration::from_secs(5))
            .expect("spawn closure should hold the child lock");

        let (restart_started_tx, restart_started_rx) = mpsc::channel();
        let (restart_done_tx, restart_done_rx) = mpsc::channel();
        let restart_manager = manager.clone();
        let restart = thread::spawn(move || {
            restart_started_tx.send(()).expect("signal restart entry");
            restart_done_tx
                .send(restart_manager.shutdown_for_restart())
                .expect("publish restart result");
        });
        restart_started_rx
            .recv_timeout(Duration::from_secs(5))
            .expect("manual restart should start");
        assert!(restart_done_rx.try_recv().is_err());

        release_spawn_tx.send(()).expect("complete claimed spawn");
        let generation = generation_rx
            .recv_timeout(Duration::from_secs(5))
            .expect("registered generation should be available");
        wait_until(|| {
            !child_state
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .writes
                .is_empty()
        });
        assert!(manager.mark_exited(generation, Some(0), None));
        restart_done_rx
            .recv_timeout(Duration::from_secs(5))
            .expect("manual restart should finish after the registered child exits")
            .expect("clean registered child exit should allow application restart");
        spawn.join().expect("spawn thread should finish");
        restart.join().expect("restart thread should finish");

        let state = child_state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        assert_eq!(state.writes, vec![b"shutdown\n".to_vec()]);
        assert_eq!(state.kill_calls, 0);
    }

    #[test]
    fn a_late_clean_exit_allows_retrying_the_application_restart() {
        let manager = SidecarManager::new();
        let (_, generation) = install_fake_child(&manager, None, None);

        manager.shutdown_with_timeouts(Duration::ZERO, Duration::ZERO);
        let first_error = manager
            .shutdown_for_restart()
            .expect_err("unconfirmed exit must still block application restart");
        assert!(first_error.contains("真实退出确认"));

        assert!(manager.mark_exited(generation, Some(0), None));
        manager
            .shutdown_for_restart()
            .expect("a later clean Terminated event should unlock manual recovery");
    }

    #[test]
    fn stale_generation_exit_cannot_clear_the_current_child() {
        let manager = SidecarManager::new();
        let (_, first_generation) = install_fake_child(&manager, None, None);
        assert!(manager.mark_exited(first_generation, Some(1), None));
        let (_, second_generation) = install_fake_child(&manager, None, None);
        assert!(second_generation > first_generation);

        assert!(!manager.mark_exited(first_generation, Some(0), None));
        let state = manager
            .inner
            .child
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        assert_eq!(state.active_generation, Some(second_generation));
        assert_eq!(
            state.child.as_ref().map(|child| child.generation),
            Some(second_generation)
        );
    }

    #[test]
    fn concurrent_shutdown_callers_share_one_stop_operation() {
        let manager = SidecarManager::new();
        let (child_state, generation) = install_fake_child(&manager, None, None);
        let first_manager = manager.clone();
        let first = thread::spawn(move || {
            first_manager.shutdown_with_timeouts(Duration::from_secs(5), Duration::from_secs(1));
        });
        wait_until(|| {
            !child_state
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .writes
                .is_empty()
        });
        let second_manager = manager.clone();
        let second = thread::spawn(move || {
            second_manager.shutdown_with_timeouts(Duration::from_secs(5), Duration::from_secs(1));
        });

        assert!(manager.mark_exited(generation, Some(0), None));
        first.join().expect("shutdown owner should finish");
        second.join().expect("shutdown waiter should finish");

        let state = child_state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        assert_eq!(state.writes, vec![b"shutdown\n".to_vec()]);
        assert_eq!(state.kill_calls, 0);
    }
}
