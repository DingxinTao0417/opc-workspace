//! Plain text only: never HTML/Markdown/RTF and never lossy UTF-8. Newlines and
//! invisible characters are escaped before text reaches native edit controls.
use super::*;
use std::fmt::Write;

pub(super) struct NativeText {
    pub(super) summary: String,
    pub(super) difference: String,
    pub(super) retained: String,
}
fn visible(value: &str) -> String {
    let quoted = serde_json::to_string(value).expect("string serialization cannot fail");
    let mut result = String::new();
    for ch in quoted[1..quoted.len() - 1].chars() {
        if matches!(ch, '\u{007f}'..='\u{009f}' | '\u{061c}' | '\u{200b}'..='\u{200f}'
            | '\u{2028}'..='\u{202e}' | '\u{2060}'..='\u{206f}' | '\u{feff}')
        {
            write!(&mut result, "\\u{:04x}", ch as u32).unwrap();
        } else {
            result.push(ch);
        }
    }
    result
}
fn lines(bytes: &[u8], prefix: &str, hex: bool) -> String {
    if bytes.is_empty() {
        return format!("{prefix}[空文件，0 字节]\r\n");
    }
    let mut result = String::new();
    if hex {
        for (i, row) in bytes.chunks(16).enumerate() {
            write!(&mut result, "{prefix}{:08x}:", i * 16).unwrap();
            for byte in row {
                write!(&mut result, " {byte:02x}").unwrap();
            }
            result.push_str("\r\n");
        }
    } else {
        for (i, line) in std::str::from_utf8(bytes)
            .expect("UTF-8 was checked")
            .split_inclusive('\n')
            .enumerate()
        {
            write!(&mut result, "{prefix}{:06} {}\r\n", i + 1, visible(line)).unwrap();
        }
    }
    result
}
impl ReviewDocument {
    pub(super) fn native_text(&self) -> Result<NativeText, &'static str> {
        let original = self.original.decode()?;
        let candidate = self.candidate.decode()?;
        let hex = [&original, &candidate]
            .iter()
            .any(|v| v.contains(&0) || std::str::from_utf8(v).is_err());
        let (label, before, after, kept, kept_label) = match self.operation {
            "replace" => (
                "替换：原文 → 候选；保留原对象",
                Some(&original),
                &candidate,
                &original,
                "保留的原对象",
            ),
            "restore_missing" => (
                "缺失恢复：缺失路径 → 原对象；不安装候选",
                None,
                &original,
                &candidate,
                "保留的候选对象",
            ),
            "undo_installed" => (
                "原样撤销：当前候选 → 原对象；保留候选",
                Some(&candidate),
                &original,
                &candidate,
                "保留的候选对象",
            ),
            _ => return Err("不支持的审查操作"),
        };
        let summary = format!(
            "{label}\r\n根目录：{}\r\n相对路径：{}\r\n操作：{}　审查：{}\r\n目录选择：{}\r\n原文 {} 字节　候选 {} 字节\r\n原文 SHA-256：{}\r\n候选 SHA-256：{}\r\n请求 SHA-256：{}\r\n到期时间（Unix 毫秒）：{}\r\n仅记录本次审查决定，不会提权或写入；完整 SACL、恢复记录与执行许可仍须独立核验。",
            visible(&self.root),
            visible(&self.path),
            self.operation_id,
            self.review_id,
            self.root_selection_id,
            original.len(),
            candidate.len(),
            sha256(&original),
            sha256(&candidate),
            self.request_sha256,
            self.expires_at_ms,
        );
        let mut difference = if hex {
            "完整十六进制对照（未截断）\r\n"
        } else {
            "完整转义文本对照（未截断；- 为之前，+ 为之后）\r\n"
        }
        .to_owned();
        if let Some(before) = before {
            difference.push_str(&lines(before, "- ", hex));
        } else {
            difference.push_str("之前：[目标路径不存在，不是空文件]\r\n");
        }
        difference.push_str(&lines(after, "+ ", hex));
        let retained = format!(
            "{kept_label}（仅展示，不改写）：\r\n{}",
            lines(kept, "  ", hex)
        );
        Ok(NativeText {
            summary,
            difference,
            retained,
        })
    }
}
