//! Parent-side expected helper artifact, bound at build time, not by runtime
//! env/argv/JSON. This is integrity relative to the trusted desktop build, not
//! Authenticode, installer trust, parent authentication or a launch permit.
use crate::file_helper_transport::{
    native::{Endpoint, Listener, Peer},
    wire::Binding,
};
use crate::file_operation_contract::{ObjectIdentity, sha256};
use crate::file_operation_read::{PinnedDirectory, read_regular};
use std::{
    fs::{File, OpenOptions},
    os::windows::{fs::OpenOptionsExt, io::OwnedHandle},
    path::{Path, PathBuf},
};
use windows::Win32::Storage::FileSystem::FILE_FLAG_OPEN_REPARSE_POINT;

const NAME: &str = "opc-file-security-helper.exe";
const MAX_BYTES: usize = 128 * 1024 * 1024;

/// Read-only product capability probe. It never launches the helper and never
/// accepts a path or digest from IPC; a standard unbound development build is
/// expected to return false.
pub(crate) fn product_ready() -> bool {
    PinnedHelperImage::installed()
        .and_then(|mut image| image.revalidate())
        .is_ok()
}

#[path = "file_helper_image/launch.rs"]
pub(crate) mod launch;

struct ExpectedImage(String);
impl ExpectedImage {
    fn embedded() -> Result<Self, String> {
        Self::parse(option_env!("OPC_FILE_HELPER_SHA256"))
    }
    fn parse(value: Option<&str>) -> Result<Self, String> {
        let value = value.ok_or("本次桌面构建未绑定辅助程序；不会启动任何程序")?;
        if value.len() != 64
            || !value
                .bytes()
                .all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
        {
            return Err("构建内辅助程序摘要无效".into());
        }
        Ok(Self(value.into()))
    }
}

pub(crate) struct PinnedHelperImage {
    file: File,
    _directory: PinnedDirectory,
    identity: ObjectIdentity,
    modified: u64,
    expected: ExpectedImage,
    path: PathBuf,
}
impl PinnedHelperImage {
    /// Only the installed desktop's sibling can be considered. No PATH search,
    /// working-directory fallback, runtime env override or user-supplied name.
    pub(crate) fn installed() -> Result<Self, String> {
        let expected = ExpectedImage::embedded()?;
        let executable = std::env::current_exe().map_err(|_| "无法定位当前桌面程序")?;
        Self::open(executable.parent().ok_or("桌面程序目录无效")?, expected)
    }
    fn open(directory: &Path, expected: ExpectedImage) -> Result<Self, String> {
        let pin = PinnedDirectory::open(directory, false)?;
        let path = directory.join(NAME);
        let mut file = OpenOptions::new()
            .read(true)
            .share_mode(1)
            .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0)
            .open(&path)
            .map_err(|_| "辅助程序缺失、被替换或正在被修改")?;
        // FILE_SHARE_READ permits the loader's read/execute access but denies
        // writes/deletion. Ancestors remain pinned. Do not grant data-write rights.
        let (bytes, identity, modified) = read_regular(&mut file, MAX_BYTES)?;
        if bytes.is_empty() || sha256(&bytes) != expected.0 {
            return Err("辅助程序内容与本次桌面构建不一致；拒绝启动".into());
        }
        Ok(Self {
            file,
            _directory: pin,
            identity,
            modified,
            expected,
            path,
        })
    }
    pub(crate) fn revalidate(&mut self) -> Result<(), String> {
        let (bytes, identity, modified) = read_regular(&mut self.file, MAX_BYTES)?;
        if identity != self.identity
            || modified != self.modified
            || sha256(&bytes) != self.expected.0
        {
            return Err("辅助程序身份或内容已变化；拒绝启动".into());
        }
        Ok(())
    }
    /// Bind a returned launch handle while the pre-launch pin is still held.
    /// This never launches/elevates and does not accept a PID from IPC/argv.
    /// The trusted caller must obtain this handle by launching AFTER acquiring
    /// this pin, not by looking up an already-running process. The OS image name
    /// is not a measurement of in-memory code or proof of launch provenance.
    pub(crate) fn bind_process(
        mut self,
        launch: OwnedHandle,
    ) -> Result<BoundHelperProcess, String> {
        self.revalidate()?;
        let peer = Peer::from_handle(launch)?;
        let image_path = peer.image_path()?;
        if image_path.file_name() != Some(std::ffi::OsStr::new(NAME)) {
            return Err("返回进程不是固定辅助程序".into());
        }
        // Reopen the kernel-reported path with the same no-reparse rules. A
        // matching name/hash alone is insufficient: require the same directory
        // and file objects as the pre-launch pin. No string canonicalization or
        // PID reopen can substitute another process/file generation.
        let observed = Self::open(
            image_path.parent().ok_or("进程映像目录无效")?,
            ExpectedImage(self.expected.0.clone()),
        )?;
        if observed.identity != self.identity
            || observed._directory.identity != self._directory.identity
        {
            return Err("返回进程映像不是本次固定的文件对象".into());
        }
        self.revalidate()?;
        peer.image_path()?;
        Ok(BoundHelperProcess { image: self, peer })
    }
    // Usable only while self and all its pins remain alive.
    pub(crate) fn path(&self) -> &Path {
        &self.path
    }
}

/// Integrity relative to the pinned artifact, NOT publisher authentication,
/// proof of elevation, memory-injection protection, or permission to execute.
pub(crate) struct BoundHelperProcess {
    image: PinnedHelperImage,
    peer: Peer,
}
impl BoundHelperProcess {
    pub(crate) fn accept(mut self, listener: Listener) -> Result<BoundHelperConnection, String> {
        self.image.revalidate()?;
        let endpoint = listener.accept(self.peer)?;
        self.image.revalidate()?;
        Ok(BoundHelperConnection {
            image: self.image,
            endpoint,
        })
    }
}
pub(crate) struct BoundHelperConnection {
    image: PinnedHelperImage,
    endpoint: Endpoint,
}
impl BoundHelperConnection {
    /// One-shot exchange; image/ancestor pins cover accept, send and receipt.
    /// Failure means unknown communication outcome, never implied rollback.
    pub(crate) fn request(mut self, binding: &Binding, body: &[u8]) -> Result<Vec<u8>, String> {
        self.image.revalidate()?;
        let response = self.endpoint.request(binding, body)?;
        self.image.revalidate()?;
        Ok(response)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::file_helper_transport::native::Cancel;
    use std::{fs, io::Write};
    use std::{
        os::windows::io::AsHandle,
        process::{Child, Command, Stdio},
        time::{Duration, Instant},
    };
    const TIMEOUT: Duration = Duration::from_secs(10);

    fn directory_delete_access(path: &Path) -> std::io::Result<File> {
        use windows::Win32::Storage::FileSystem::{DELETE, FILE_FLAG_BACKUP_SEMANTICS};
        // Access probe only: never marks a directory for deletion or moves it.
        // This directly conflicts with PinnedDirectory's no-delete sharing.
        OpenOptions::new()
            .access_mode(DELETE.0)
            .share_mode(7)
            .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0 | FILE_FLAG_BACKUP_SEMANTICS.0)
            .open(path)
    }

    pub(super) struct ChildFixture(Option<Child>);
    impl ChildFixture {
        pub(super) fn start(
            path: &Path,
            mode: &str,
            listener: &Listener,
            binding: &Binding,
        ) -> Self {
            Self(Some(
                Command::new(path)
                    .args([
                        "--exact",
                        "file_helper_transport::tests::pipe_process_fixture",
                        "--ignored",
                        "--nocapture",
                    ])
                    .env("OPC_PIPE_TEST_MODE", mode)
                    .env("OPC_PIPE_TEST_ID", listener.id())
                    .env("OPC_PIPE_TEST_PARENT", std::process::id().to_string())
                    .env("OPC_PIPE_TEST_NONCE", binding.fixture_nonce())
                    .env("OPC_PIPE_TEST_DIGEST", binding.fixture_digest())
                    .stdin(Stdio::piped())
                    .stdout(Stdio::null())
                    .stderr(Stdio::null())
                    .spawn()
                    .unwrap(),
            ))
        }
        pub(super) fn handle(&self) -> OwnedHandle {
            self.0
                .as_ref()
                .expect("fixture already finished")
                .as_handle()
                .try_clone_to_owned()
                .unwrap()
        }
        pub(super) fn finish(&mut self) {
            let Some(child) = self.0.as_mut() else { return };
            drop(child.stdin.take());
            let until = Instant::now() + TIMEOUT;
            let status = loop {
                if let Some(status) = child.try_wait().unwrap() {
                    break status;
                }
                assert!(Instant::now() < until, "owned image fixture did not exit");
                std::thread::sleep(Duration::from_millis(5));
            };
            // Waiting for exit does not close the parent's process handle.
            // Consume the finished owner before asserting all fixture resources
            // have been released or renaming its image directory. Keep self's
            // ownership during the wait so unwinding still kills/waits OUR child.
            drop(self.0.take());
            assert!(status.success());
        }
    }
    impl Drop for ChildFixture {
        fn drop(&mut self) {
            let Some(child) = self.0.as_mut() else { return };
            if !matches!(child.try_wait(), Ok(Some(_))) {
                let _ = child.kill();
            }
            let _ = child.wait();
        }
    }
    pub(super) struct Fixture(PathBuf);
    impl Fixture {
        pub(super) fn new() -> Self {
            let path = std::env::temp_dir()
                .join(format!("opc-helper-image-test-{}", uuid::Uuid::new_v4()));
            fs::create_dir(&path).unwrap();
            fs::create_dir(path.join("app")).unwrap();
            fs::write(
                path.join("app").join(NAME),
                b"test artifact, never executed",
            )
            .unwrap();
            Self(path)
        }
        fn pin(&self) -> Result<PinnedHelperImage, String> {
            PinnedHelperImage::open(
                &self.0.join("app"),
                ExpectedImage(sha256(b"test artifact, never executed")),
            )
        }
        pub(super) fn executable_pin(&self) -> PinnedHelperImage {
            let source = std::env::current_exe().unwrap();
            fs::copy(&source, self.0.join("app").join(NAME)).unwrap();
            PinnedHelperImage::open(
                &self.0.join("app"),
                ExpectedImage(sha256(&fs::read(source).unwrap())),
            )
            .unwrap()
        }
    }
    impl Drop for Fixture {
        fn drop(&mut self) {
            assert!(
                self.0.is_absolute() && self.0.parent() == Some(std::env::temp_dir().as_path())
            );
            assert!(
                self.0
                    .file_name()
                    .unwrap()
                    .to_string_lossy()
                    .starts_with("opc-helper-image-test-")
            );
            fs::remove_dir_all(&self.0).unwrap();
        }
    }
    #[test]
    fn missing_or_malformed_embedded_policy_is_not_runtime_trust() {
        for value in [None, Some(""), Some("abcdef"), Some(&"AB".repeat(32))] {
            assert!(ExpectedImage::parse(value).is_err());
        }
        assert!(ExpectedImage::parse(Some(&sha256(b"known build"))).is_ok());
    }
    #[test]
    fn exact_artifact_is_readonly_and_pins_ancestors_but_allows_loader_reads() {
        let f = Fixture::new();
        let mut pin = f.pin().unwrap();
        assert_eq!(pin.path(), f.0.join("app").join(NAME));
        assert!(File::open(pin.path()).is_ok());
        assert!(pin.file.write_all(b"no").is_err());
        assert!(fs::write(pin.path(), b"no").is_err());
        assert!(fs::rename(pin.path(), f.0.join("moved.exe")).is_err());
        assert!(fs::rename(f.0.join("app"), f.0.join("other")).is_err());
        pin.revalidate().unwrap();
        drop(pin);
        drop(directory_delete_access(&f.0.join("app")).unwrap());
        // No executable was loaded in this fixture: test the actual rename
        // separately from Windows image teardown and external image readers.
        fs::rename(f.0.join("app"), f.0.join("released")).unwrap();
    }
    #[test]
    fn wrong_missing_empty_directory_or_linked_artifacts_refuse_without_fallback() {
        for kind in ["wrong", "missing", "empty", "directory", "linked"] {
            let f = Fixture::new();
            let path = f.0.join("app").join(NAME);
            match kind {
                "wrong" => fs::write(&path, b"unrelated").unwrap(),
                "empty" => fs::write(&path, b"").unwrap(),
                "missing" => fs::remove_file(&path).unwrap(),
                "directory" => {
                    fs::remove_file(&path).unwrap();
                    fs::create_dir(&path).unwrap();
                }
                "linked" => fs::hard_link(&path, f.0.join("alias")).unwrap(),
                _ => unreachable!(),
            }
            assert!(f.pin().is_err(), "{kind}");
        }
    }
    #[test]
    fn late_alias_invalidates_the_pin_without_modifying_the_alias() {
        let f = Fixture::new();
        let mut pin = f.pin().unwrap();
        fs::hard_link(pin.path(), f.0.join("alias")).unwrap();
        assert!(pin.revalidate().is_err());
        assert_eq!(
            fs::read(f.0.join("alias")).unwrap(),
            b"test artifact, never executed"
        );
    }

    #[test]
    fn held_read_pin_allows_owned_test_image_to_load_without_write_rights() {
        // Use this very test executable, not any system/user executable. --list
        // only enumerates its tests; no fixture actions, desktop, model or UAC.
        let f = Fixture::new();
        let source = std::env::current_exe().unwrap();
        fs::copy(&source, f.0.join("app").join(NAME)).unwrap();
        let expected = ExpectedImage(sha256(&fs::read(&source).unwrap()));
        let mut pin = PinnedHelperImage::open(&f.0.join("app"), expected).unwrap();
        let result = std::process::Command::new(pin.path())
            .arg("--list")
            .stdin(std::process::Stdio::null())
            .output()
            .unwrap();
        assert!(result.status.success());
        assert!(
            String::from_utf8(result.stdout)
                .unwrap()
                .contains("held_read_pin_allows_owned_test_image")
        );
        pin.revalidate().unwrap();
    }

    #[test]
    fn returned_handle_binds_exact_image_and_keeps_pins_through_pipe_exchange() {
        let f = Fixture::new();
        let pin = f.executable_pin();
        let path = pin.path().to_owned();
        let binding = Binding::new(b"process fixture").unwrap();
        let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
        let mut child = ChildFixture::start(&path, "exchange", &listener, &binding);
        let process = pin.bind_process(child.handle()).unwrap();
        assert!(directory_delete_access(&f.0.join("app")).is_err());
        assert!(fs::rename(f.0.join("app"), f.0.join("moved")).is_err());
        let connection = process.accept(listener).unwrap();
        assert!(fs::write(&path, b"must not change").is_err());
        assert_eq!(
            connection.request(&binding, b"process fixture").unwrap(),
            b"child receipt"
        );
        assert!(child.0.as_mut().unwrap().try_wait().unwrap().is_none());
        drop(
            directory_delete_access(&f.0.join("app"))
                .expect("adapter directory pin was not released"),
        );
        // This directly proves the adapter's no-delete directory pin is gone,
        // while the fixture process is STILL alive. A directory tree containing
        // a just-run image need not become immediately renameable on Windows;
        // probing its DELETE access avoids conflating that with our pin lifetime.
        child.finish();
        assert!(child.0.is_none());
    }

    #[test]
    fn matching_name_and_bytes_in_another_directory_are_not_the_pinned_image() {
        let expected = Fixture::new();
        let unrelated = Fixture::new();
        let pin = expected.executable_pin();
        let other = unrelated.executable_pin();
        let binding = Binding::new(b"process fixture").unwrap();
        let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
        let mut child = ChildFixture::start(other.path(), "idle", &listener, &binding);
        assert!(pin.bind_process(child.handle()).is_err());
        child.finish();
    }

    #[test]
    fn returned_exited_process_or_nonprocess_handle_fails_closed() {
        let f = Fixture::new();
        let pin = f.executable_pin();
        let binding = Binding::new(b"process fixture").unwrap();
        let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
        let mut child = ChildFixture::start(pin.path(), "idle", &listener, &binding);
        let handle = child.handle();
        child.finish();
        assert!(pin.bind_process(handle).is_err());
        let pin = f.executable_pin();
        let file = File::open(pin.path()).unwrap();
        assert!(pin.bind_process(file.into()).is_err());
    }

    #[test]
    fn process_exit_after_binding_and_late_image_alias_prevent_accept() {
        for late_alias in [false, true] {
            let f = Fixture::new();
            let pin = f.executable_pin();
            let path = pin.path().to_owned();
            let binding = Binding::new(b"process fixture").unwrap();
            let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
            let mut child = ChildFixture::start(&path, "idle", &listener, &binding);
            let process = pin.bind_process(child.handle()).unwrap();
            if late_alias {
                fs::hard_link(&path, f.0.join("late-alias.exe")).unwrap();
            } else {
                child.finish();
            }
            assert!(process.accept(listener).is_err());
            if late_alias {
                child.finish();
            }
        }
    }
}
