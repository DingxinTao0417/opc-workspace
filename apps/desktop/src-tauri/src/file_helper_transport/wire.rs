use sha2::{Digest, Sha256};
use std::sync::atomic::{AtomicBool, Ordering};

pub(super) const MAX_BODY: usize = 1024 * 1024;
const HEADER: usize = 8 + 1 + 32 + 32 + 4;
pub(super) const MAX_FRAME: usize = HEADER + MAX_BODY;
const MAGIC: &[u8; 8] = b"OPCFIPC1";

// This binding must be freshly minted from the reviewed immutable request, not
// accepted from a WebView or temp JSON. Receiver bootstrap metadata must be
// bound to separately checked build/OS peers before use. This type itself is not
// serializable and isn't a substitute for a per-use human approval capability.
pub(crate) struct Binding {
    nonce: [u8; 32],
    digest: [u8; 32],
    sent: AtomicBool,
    received: AtomicBool,
}
impl Binding {
    /// Bootstrap metadata only. No project path/body or caller-supplied command
    /// is put on argv. These words are NOT authentication/approval credentials;
    /// the helper accepts them only after entering its installed-pair receive path.
    pub(crate) fn bootstrap_parameters(
        &self,
        pipe_id: &str,
        parent_pid: u32,
        parent_created: u64,
    ) -> Result<String, &'static str> {
        if parent_pid == 0
            || parent_created == 0
            || !uuid::Uuid::parse_str(pipe_id).is_ok_and(|id| id.to_string() == pipe_id)
        {
            return Err("辅助启动通信身份无效");
        }
        let hex = |bytes: &[u8]| bytes.iter().map(|v| format!("{v:02x}")).collect::<String>();
        Ok(format!(
            "--review-v1 {pipe_id} {parent_pid} {parent_created} {} {}",
            hex(&self.nonce),
            hex(&self.digest)
        ))
    }
    pub(super) fn from_untrusted_bootstrap(nonce: [u8; 32], digest: [u8; 32]) -> Self {
        Self {
            nonce,
            digest,
            sent: AtomicBool::new(false),
            received: AtomicBool::new(false),
        }
    }
    #[cfg(test)]
    pub(crate) fn fixture_nonce(&self) -> String {
        self.nonce.iter().map(|v| format!("{v:02x}")).collect()
    }
    #[cfg(test)]
    pub(crate) fn fixture_digest(&self) -> String {
        self.digest.iter().map(|v| format!("{v:02x}")).collect()
    }
    #[cfg(test)]
    pub(super) fn request_fixture(nonce: &str, digest: &str) -> Self {
        let mut value = Self::process_fixture(nonce);
        assert_eq!(digest.len(), 64);
        for (i, byte) in value.digest.iter_mut().enumerate() {
            *byte = u8::from_str_radix(&digest[i * 2..i * 2 + 2], 16).unwrap();
        }
        value
    }
    #[cfg(test)]
    pub(super) fn process_fixture(nonce: &str) -> Self {
        // Test-only shortcut without peer proof. Production metadata must be
        // admitted by build/OS checks; a Binding never proves human approval.
        assert_eq!(nonce.len(), 64);
        let mut result = Self::new(b"process fixture").unwrap();
        for (i, byte) in result.nonce.iter_mut().enumerate() {
            *byte = u8::from_str_radix(&nonce[i * 2..i * 2 + 2], 16).unwrap();
        }
        result
    }
    pub(crate) fn new(reviewed: &[u8]) -> Result<Self, &'static str> {
        body_size(reviewed.len())?;
        let mut nonce = [0; 32];
        nonce[..16].copy_from_slice(uuid::Uuid::new_v4().as_bytes());
        nonce[16..].copy_from_slice(uuid::Uuid::new_v4().as_bytes());
        Ok(Self {
            nonce,
            digest: Sha256::digest(reviewed).into(),
            sent: AtomicBool::new(false),
            received: AtomicBool::new(false),
        })
    }
    pub(super) fn begin_request(&self) -> Result<(), &'static str> {
        consume(&self.sent)
    }
    pub(super) fn begin_receive(&self) -> Result<(), &'static str> {
        consume(&self.received)
    }
    pub(super) fn frame(&self, request: bool, body: &[u8]) -> Result<Vec<u8>, &'static str> {
        body_size(body.len())?;
        if request && self.digest != <[u8; 32]>::from(Sha256::digest(body)) {
            return Err("请求与已冻结审查不一致");
        }
        let mut frame = Vec::with_capacity(HEADER + body.len());
        frame.extend_from_slice(MAGIC);
        frame.push(if request { 1 } else { 2 });
        frame.extend_from_slice(&self.nonce);
        frame.extend_from_slice(&self.digest);
        frame.extend_from_slice(&(body.len() as u32).to_le_bytes());
        frame.extend_from_slice(body);
        Ok(frame)
    }
    pub(super) fn decode(&self, request: bool, frame: Vec<u8>) -> Result<Vec<u8>, &'static str> {
        if !(HEADER..=MAX_FRAME).contains(&frame.len())
            || &frame[..8] != MAGIC
            || frame[8] != if request { 1 } else { 2 }
            || frame[9..41] != self.nonce
            || frame[41..73] != self.digest
        {
            return Err("辅助通信版本、方向或本次身份不符");
        }
        let size = u32::from_le_bytes(frame[73..77].try_into().unwrap()) as usize;
        body_size(size)?;
        if frame.len() != HEADER + size {
            return Err("辅助通信正文不完整或存在额外数据");
        }
        let body = frame[HEADER..].to_vec();
        if request && <[u8; 32]>::from(Sha256::digest(&body)) != self.digest {
            return Err("辅助请求与已冻结审查不一致");
        }
        Ok(body)
    }
}
fn consume(used: &AtomicBool) -> Result<(), &'static str> {
    used.compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
        .map(|_| ())
        .map_err(|_| "本次辅助通信已消费，不得重试或重放")
}
fn body_size(size: usize) -> Result<(), &'static str> {
    if size == 0 || size > MAX_BODY {
        Err("辅助通信正文为空或超限")
    } else {
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn each_role_consumes_binding_before_even_a_failed_attempt() {
        let binding = Binding::new(b"request").unwrap();
        assert!(binding.begin_request().is_ok());
        assert!(binding.frame(true, b"changed").is_err());
        assert!(binding.begin_request().is_err());
        assert!(binding.begin_receive().is_ok());
        assert!(binding.begin_receive().is_err());
    }
    #[test]
    fn exact_roundtrip_and_direction() {
        let binding = Binding::new(b"reviewed request").unwrap();
        let frame = binding.frame(true, b"reviewed request").unwrap();
        assert_eq!(
            binding.decode(true, frame.clone()).unwrap(),
            b"reviewed request"
        );
        assert!(binding.decode(false, frame).is_err());
        assert_eq!(
            binding
                .decode(false, binding.frame(false, b"receipt").unwrap())
                .unwrap(),
            b"receipt"
        );
        assert!(binding.frame(true, b"changed").is_err());
    }
    #[test]
    fn tampering_truncation_trailing_and_replay_bindings_fail() {
        let binding = Binding::new(b"request").unwrap();
        let frame = binding.frame(true, b"request").unwrap();
        for i in 0..frame.len() {
            let mut changed = frame.clone();
            changed[i] ^= 1;
            assert!(binding.decode(true, changed).is_err());
            assert!(binding.decode(true, frame[..i].to_vec()).is_err());
        }
        let mut extra = frame.clone();
        extra.push(0);
        assert!(binding.decode(true, extra).is_err());
        let second = Binding::new(b"request").unwrap();
        assert!(second.decode(true, frame).is_err());
    }
    #[test]
    fn size_is_bounded_before_encoding_and_after_receiving() {
        assert!(Binding::new(&[]).is_err());
        assert!(Binding::new(&vec![0; MAX_BODY + 1]).is_err());
        let full = vec![9; MAX_BODY];
        let binding = Binding::new(&full).unwrap();
        assert_eq!(
            binding
                .decode(true, binding.frame(true, &full).unwrap())
                .unwrap(),
            full
        );
        let mut wrong = binding.frame(true, &full).unwrap();
        wrong[73..77].copy_from_slice(&u32::MAX.to_le_bytes());
        assert!(binding.decode(true, wrong).is_err());
    }
}
