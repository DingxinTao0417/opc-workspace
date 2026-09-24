//! Strict bounded launch metadata. A parsed value is UNTRUSTED, not a permit.
use super::wire::Binding;
use std::ffi::OsString;
pub(crate) struct UntrustedBootstrap {
    pipe: String,
    parent: u32,
    created: u64,
    nonce: [u8; 32],
    digest: [u8; 32],
}
fn hex(value: &str) -> Result<[u8; 32], &'static str> {
    if value.len() != 64
        || !value
            .bytes()
            .all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
    {
        return Err("辅助启动绑定不是规范摘要");
    }
    let mut bytes = [0; 32];
    for (i, byte) in bytes.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&value[2 * i..2 * i + 2], 16).map_err(|_| "辅助启动摘要无效")?;
    }
    Ok(bytes)
}
impl UntrustedBootstrap {
    pub(crate) fn parse(args: &[OsString]) -> Result<Self, &'static str> {
        if args.len() != 6 {
            return Err("辅助启动参数数量不符");
        }
        let text: Vec<_> = args
            .iter()
            .map(|a| {
                a.to_str()
                    .filter(|s| s.len() <= 64)
                    .ok_or("辅助启动参数编码或长度无效")
            })
            .collect::<Result<_, _>>()?;
        if text[0] != "--review-v1"
            || !uuid::Uuid::parse_str(text[1]).is_ok_and(|v| v.to_string() == text[1])
        {
            return Err("辅助启动协议或通道身份不符");
        }
        let parent: u32 = text[2].parse().map_err(|_| "辅助父进程身份无效")?;
        let created: u64 = text[3].parse().map_err(|_| "辅助父进程世代无效")?;
        if parent == 0
            || created == 0
            || parent.to_string() != text[2]
            || created.to_string() != text[3]
        {
            return Err("辅助父进程身份或世代不是规范值");
        }
        Ok(Self {
            pipe: text[1].into(),
            parent,
            created,
            nonce: hex(text[4])?,
            digest: hex(text[5])?,
        })
    }
    pub(crate) fn pipe(&self) -> &str {
        &self.pipe
    }
    pub(crate) fn parent(&self) -> u32 {
        self.parent
    }
    pub(crate) fn created(&self) -> u64 {
        self.created
    }
    pub(crate) fn into_binding(self) -> Binding {
        Binding::from_untrusted_bootstrap(self.nonce, self.digest)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn arguments() -> Vec<OsString> {
        Binding::new(b"request")
            .unwrap()
            .bootstrap_parameters(&uuid::Uuid::new_v4().to_string(), 42, 123)
            .unwrap()
            .split(' ')
            .map(OsString::from)
            .collect()
    }
    #[test]
    fn canonical_metadata_roundtrips_without_granting_approval() {
        let args = arguments();
        let value = UntrustedBootstrap::parse(&args).unwrap();
        assert_eq!(value.parent(), 42);
        assert_eq!(value.created(), 123);
        assert_eq!(value.pipe(), args[1].to_str().unwrap());
        assert_eq!(
            value.into_binding().fixture_nonce(),
            args[4].to_str().unwrap()
        );
    }
    #[test]
    fn malformed_overlong_and_extra_metadata_rejects() {
        let args = arguments();
        for i in 0..args.len() {
            assert!(UntrustedBootstrap::parse(&args[..i]).is_err());
        }
        let mut extra = args.clone();
        extra.push("approved".into());
        assert!(UntrustedBootstrap::parse(&extra).is_err());
        for (index, value) in [
            (0, "--replace"),
            (1, "../pipe"),
            (2, "0"),
            (2, "042"),
            (2, "+42"),
            (2, "4294967296"),
            (3, "0"),
            (3, "0123"),
            (3, "18446744073709551616"),
            (4, "AB"),
            (5, "a b"),
        ] {
            let mut invalid = args.clone();
            invalid[index] = value.into();
            assert!(
                UntrustedBootstrap::parse(&invalid).is_err(),
                "{index}:{value}"
            );
        }
        let mut overlong = args;
        overlong[4] = "a".repeat(65).into();
        assert!(UntrustedBootstrap::parse(&overlong).is_err());
    }
    #[test]
    fn nonunicode_arguments_are_not_lossily_accepted() {
        use std::os::windows::ffi::OsStringExt;
        let mut args = arguments();
        args[4] = OsString::from_wide(&[0xd800]);
        assert!(UntrustedBootstrap::parse(&args).is_err());
    }
    #[test]
    fn process_generation_is_checked_against_a_live_kernel_handle() {
        let peer = super::super::native::Peer::pin(std::process::id()).unwrap();
        let created = peer.creation_time().unwrap();
        peer.require_creation_time(created).unwrap();
        assert!(peer.require_creation_time(0).is_err());
        assert!(peer.require_creation_time(created + 1).is_err());
    }
}
