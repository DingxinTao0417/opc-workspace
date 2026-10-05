use std::{
    io::{BufRead, BufReader, Write},
    process::{Child, ChildStdout, Command, Stdio},
};

use windows::Win32::{
    Foundation::{CloseHandle, HANDLE, WAIT_OBJECT_0},
    System::Threading::{
        GetExitCodeProcess, OpenProcess, PROCESS_QUERY_LIMITED_INFORMATION, PROCESS_SYNCHRONIZE,
        WaitForSingleObject,
    },
};

use super::{JobAttachStep, ProcessJob};

const MODE: &str = "OPC_JOB_TEST_MODE";
// A fixture process that nothing reclaims ends by itself after this long, so a
// failing test cannot leave processes behind for good.
const FIXTURE_LIFETIME: std::time::Duration = std::time::Duration::from_secs(120);
const EXIT_WAIT_MS: u32 = 10_000;

// The parent tests run this ignored test in a child copy of the test binary.
// In "tree" mode it stands in for a process tree that never answers: once told
// to, it starts a grandchild, reports its process ID and then ignores
// everything.
#[test]
#[ignore = "fixture entrypoint executed by the process job tests"]
fn job_tree_fixture() {
    match std::env::var(MODE).as_deref() {
        Ok("exit") => {}
        Ok("leaf") => std::thread::sleep(FIXTURE_LIFETIME),
        // Leaves by itself, with exit code 0, when it is asked to.
        Ok("graceful") => {
            let mut line = String::new();
            std::io::stdin().read_line(&mut line).unwrap();
            assert_eq!(line.trim(), "shutdown");
            std::process::exit(0);
        }
        Ok("tree") => {
            let mut line = String::new();
            std::io::stdin().read_line(&mut line).unwrap();
            assert_eq!(line.trim(), "go");
            let grandchild = fixture("leaf")
                .stdin(Stdio::null())
                .stdout(Stdio::null())
                .stderr(Stdio::null())
                .spawn()
                .unwrap();
            println!("grandchild={}", grandchild.id());
            std::io::stdout().flush().unwrap();
            std::thread::sleep(FIXTURE_LIFETIME);
        }
        other => panic!("unknown fixture mode: {other:?}"),
    }
}

pub(crate) fn fixture(mode: &str) -> Command {
    let mut command = Command::new(std::env::current_exe().unwrap());
    command
        .args([
            "--exact",
            &format!(
                "{}::job_tree_fixture",
                module_path!().split_once("::").unwrap().1
            ),
            "--ignored",
            "--nocapture",
        ])
        .env(MODE, mode);
    command
}

/// A process handle that outlives whoever owns the process, for observing its
/// exit.
pub(crate) struct Watched(HANDLE);
impl Watched {
    pub(crate) fn open(pid: u32) -> Self {
        Self(unsafe {
            OpenProcess(
                PROCESS_SYNCHRONIZE | PROCESS_QUERY_LIMITED_INFORMATION,
                false,
                pid,
            )
            .unwrap()
        })
    }
    pub(crate) fn exited(&self) -> bool {
        unsafe { WaitForSingleObject(self.0, EXIT_WAIT_MS) == WAIT_OBJECT_0 }
    }
    pub(crate) fn exit_code(&self) -> u32 {
        let mut code = 0u32;
        unsafe { GetExitCodeProcess(self.0, &mut code).unwrap() };
        code
    }
}
impl Drop for Watched {
    fn drop(&mut self) {
        unsafe {
            let _ = CloseHandle(self.0);
        }
    }
}

/// Reads the tree fixture's output until it names its grandchild.
pub(crate) fn reported_grandchild(stdout: ChildStdout) -> u32 {
    let mut reader = BufReader::new(stdout);
    loop {
        let mut line = String::new();
        assert_ne!(
            reader.read_line(&mut line).unwrap(),
            0,
            "the fixture ended before it reported its grandchild"
        );
        if let Some(pid) = line.trim().strip_prefix("grandchild=") {
            return pid.parse::<u32>().unwrap();
        }
    }
}

/// Starts the tree fixture, puts it into a job and only then lets it start its
/// grandchild, so the grandchild is born inside the job.
fn governed_tree() -> (Child, ProcessJob, Watched, Watched) {
    let mut child = fixture("tree")
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let job = ProcessJob::attach(child.id()).expect("the fixture should be governable");
    child.stdin.as_mut().unwrap().write_all(b"go\n").unwrap();
    let grandchild = reported_grandchild(child.stdout.take().unwrap());
    let watched_child = Watched::open(child.id());
    let watched_grandchild = Watched::open(grandchild);
    (child, job, watched_child, watched_grandchild)
}

#[test]
fn closing_the_job_ends_the_child_and_the_grandchild() {
    let (mut child, job, watched_child, watched_grandchild) = governed_tree();
    drop(job);
    assert!(watched_child.exited(), "the child outlived its job");
    assert!(
        watched_grandchild.exited(),
        "the grandchild outlived the job"
    );
    child.wait().unwrap();
}

#[test]
fn terminate_ends_the_whole_tree_with_the_given_exit_code() {
    let (mut child, job, watched_child, watched_grandchild) = governed_tree();
    job.terminate(1).expect("the job should accept termination");
    assert!(watched_child.exited(), "the child survived termination");
    assert!(
        watched_grandchild.exited(),
        "the grandchild survived termination"
    );
    assert_eq!(child.wait().unwrap().code(), Some(1));
}

#[test]
fn a_process_that_already_ended_is_reported_as_exited() {
    let mut child = fixture("exit")
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    child.wait().unwrap();
    // The process object stays addressable while `child` holds its handle.
    let error = ProcessJob::attach(child.id())
        .err()
        .expect("an ended process cannot join a job");
    assert_eq!(error.step, JobAttachStep::Assign);
    assert!(error.process_exited);
}
