//! The stderr lines, one each: `<tool>: warning: <where>: <what>`, and for exit codes 1 to 4
//! `<tool>: error: <reason>`.
//!
//! `<where>` and `<what>` are escaped, and docproc inverts the escape: a backslash before a
//! backslash, `\n` and `\r` for a line feed and a carriage return, `\u{…}` with the lowercase hex
//! of its code point for any other control character, and, in `<where>`, `:\ ` for a colon
//! followed by a space. So `<where>` never holds `": "`, and a warning line splits at its first
//! three. A reason takes `<what>`'s escape, so an echoed argument or a reader's message that holds
//! a line break still makes one line. A reason never leads with the input's path, which the caller
//! already knows.

use std::io::{self, Write};

/// Writes a warning line.
pub fn warning(tool: &str, place: &str, what: &str) {
    write(&warning_line(tool, place, what));
}

/// Writes an error line.
pub fn error(tool: &str, reason: &str) {
    write(&error_line(tool, reason));
}

/// A warning's line, its line feed included.
pub fn warning_line(tool: &str, place: &str, what: &str) -> String {
    format!(
        "{tool}: warning: {}: {}\n",
        escape(place, true),
        escape(what, false)
    )
}

/// An error's line, its line feed included.
pub fn error_line(tool: &str, reason: &str) -> String {
    format!("{tool}: error: {}\n", escape(reason, false))
}

/// Writes text to stderr as it is. stderr is the only place a failure to write could be told, so
/// one is dropped: the exit code still says what happened.
pub(crate) fn write(text: &str) {
    let _ = io::stderr().lock().write_all(text.as_bytes());
}

/// The envelope's escape, `in_where` adding `:\ ` for a colon followed by a space.
fn escape(text: &str, in_where: bool) -> String {
    let mut out = String::with_capacity(text.len());
    let mut chars = text.chars().peekable();
    while let Some(c) = chars.next() {
        match c {
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            ':' if in_where && chars.peek() == Some(&' ') => out.push_str(":\\"),
            c if c.is_control() => out.push_str(&format!("\\u{{{:x}}}", u32::from(c))),
            c => out.push(c),
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::cases;

    /// The table's tool name, which each of its lines begins with.
    const TOOL: &str = "tool";

    #[test]
    fn a_warning_writes_its_tables_line() {
        for case in cases::group("warnings") {
            let line = warning_line(
                TOOL,
                cases::text(&case, "where"),
                cases::text(&case, "what"),
            );
            assert_eq!(line, format!("{}\n", cases::text(&case, "line")), "{case}");
        }
    }

    /// Split at its first three `": "` and unescaped, each warning line gives its where and what
    /// back, as docproc reads it.
    #[test]
    fn a_warning_line_splits_back_to_its_where_and_what() {
        for case in cases::group("warnings") {
            let line = cases::text(&case, "line");
            let mut parts = line.splitn(4, ": ");
            assert_eq!(parts.next(), Some(TOOL));
            assert_eq!(parts.next(), Some("warning"));
            assert_eq!(unescape(parts.next().unwrap()), cases::text(&case, "where"));
            assert_eq!(unescape(parts.next().unwrap()), cases::text(&case, "what"));
        }
    }

    #[test]
    fn an_error_writes_its_tables_line() {
        for case in cases::group("errors") {
            let line = error_line(TOOL, cases::text(&case, "reason"));
            assert_eq!(line, format!("{}\n", cases::text(&case, "line")), "{case}");
        }
    }

    /// Control characters past the table's line feed and carriage return: each a `\u{…}`, in a
    /// where, a what, and a reason, and each read back.
    #[test]
    fn a_control_character_is_escaped_by_its_code_point() {
        let place = "tab\there: \u{1b}[0m";
        let what = "bell\u{7} and next line\u{85}";
        let line = warning_line(TOOL, place, what);
        assert_eq!(
            line,
            "tool: warning: tab\\u{9}here:\\ \\u{1b}[0m: bell\\u{7} and next line\\u{85}\n"
        );
        let mut parts = line.trim_end_matches('\n').splitn(4, ": ").skip(2);
        assert_eq!(unescape(parts.next().unwrap()), place);
        assert_eq!(unescape(parts.next().unwrap()), what);
        assert_eq!(error_line(TOOL, "a\0b"), "tool: error: a\\u{0}b\n");
    }

    /// The inverse docproc applies, here only to show the escape inverts.
    fn unescape(text: &str) -> String {
        let mut out = String::new();
        let mut chars = text.chars();
        while let Some(c) = chars.next() {
            if c != '\\' {
                out.push(c);
                continue;
            }
            match chars.next() {
                Some('n') => out.push('\n'),
                Some('r') => out.push('\r'),
                Some('u') => {
                    let hex: String = chars.by_ref().skip(1).take_while(|&c| c != '}').collect();
                    out.extend(char::from_u32(u32::from_str_radix(&hex, 16).unwrap()));
                }
                Some(other) => out.push(other),
                None => panic!("a trailing backslash in {text:?}"),
            }
        }
        out
    }
}
