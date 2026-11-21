//! The Markdown the tools write around their readers' text: the literal-text escape, the
//! destination writer, and the title writer. The escape is htmd's rules, which every tool's
//! Markdown shares (docs/tool-contract.md, "Common envelope", Text layer).

/// A line of literal text as Markdown that reads as the text does: a backslash before each of
/// ``\ * _ ` [ ] | ~ & $``, and before `<` when what follows could open an HTML tag, comment, or
/// instruction. With `line_start`, the text begins a line, so a first `-`, `=`, `+`, `#`, or `>`
/// is escaped too, and the `.` or `)` after a first one to nine digits, so that no line is a
/// heading, a rule, a quote, or a list item, or underlines the line before it into a heading.
/// The text holds no line break; the caller writes its own.
pub fn escape(text: &str, line_start: bool) -> String {
    let mut out = String::with_capacity(text.len() + 8);
    let mut rest = text;
    if line_start {
        if rest.starts_with(['-', '=', '+', '#', '>']) {
            out.push('\\');
        }
        let digits = rest.bytes().take_while(u8::is_ascii_digit).count();
        if (1..=9).contains(&digits) && matches!(rest.as_bytes().get(digits), Some(b'.' | b')')) {
            out.push_str(&rest[..digits]);
            out.push('\\');
            rest = &rest[digits..];
        }
    }
    for (index, c) in rest.char_indices() {
        match c {
            '\\' | '*' | '_' | '`' | '[' | ']' | '|' | '~' | '&' | '$' => {
                out.push('\\');
                out.push(c);
            }
            '<' if html_like(&rest[index + 1..]) => out.push_str("\\<"),
            _ => out.push(c),
        }
    }
    out
}

/// Whether what follows a `<` could make it an HTML tag, a comment, or an instruction: a letter,
/// `/` then a letter or nothing, `!`, or `?`. A `<` that ends its text is escaped too, since the
/// text after it could carry on.
fn html_like(rest: &str) -> bool {
    let mut chars = rest.chars();
    match chars.next() {
        None | Some('!' | '?') => true,
        Some('/') => chars.next().is_none_or(|c| c.is_ascii_alphabetic()),
        Some(c) => c.is_ascii_alphabetic(),
    }
}

/// A link's or an image's target as a Markdown destination that CommonMark reads back as the
/// target. One holding whitespace, a control character, `<`, or `>` is written `<…>`, its `<` and
/// `>` escaped; any other is written bare, its `(` and `)` escaped, so neither form ends before
/// the target does, balanced or not. In both, each `\` is escaped, and each `&` that could begin
/// a character reference, since CommonMark reads both escapes and references in a destination.
/// A line ending or a NUL, which no destination can hold, is percent-encoded (`%0A`, `%0D`,
/// `%00`), the one way the destination reads back other than as the target. A `|` is left to a
/// table cell's writer.
pub fn destination(target: &str) -> String {
    let angled = target
        .chars()
        .any(|c| c.is_whitespace() || c.is_control() || c == '<' || c == '>');
    let mut out = String::with_capacity(target.len() + 8);
    if angled {
        out.push('<');
    }
    for (index, c) in target.char_indices() {
        match c {
            '\\' => out.push_str("\\\\"),
            '&' if reference_like(&target[index + 1..]) => out.push_str("\\&"),
            '<' | '>' if angled => {
                out.push('\\');
                out.push(c);
            }
            '(' | ')' if !angled => {
                out.push('\\');
                out.push(c);
            }
            '\n' => out.push_str("%0A"),
            '\r' => out.push_str("%0D"),
            '\0' => out.push_str("%00"),
            _ => out.push(c),
        }
    }
    if angled {
        out.push('>');
    }
    out
}

/// A link's or an image's title as the text of a Markdown title, `"…"`, that CommonMark reads
/// back as the title. Each `\` and `"` is escaped, and each `&` that could begin a character
/// reference, as in a [`destination`], since CommonMark reads both escapes and references in a
/// title; any other character reads as itself. A line feed is written `&#10;` and a carriage
/// return `&#13;`, references CommonMark reads back as them, so the title keeps to its line, where
/// no line of it can start a block, end the paragraph, or break a table's row. A NUL, which
/// CommonMark reads as U+FFFD wherever it is, is written as U+FFFD, the one way the title reads
/// back other than as written. A `|` is left to a table cell's writer.
pub fn title(text: &str) -> String {
    let mut out = String::with_capacity(text.len() + 8);
    for (index, c) in text.char_indices() {
        match c {
            '\\' | '"' => {
                out.push('\\');
                out.push(c);
            }
            '&' if reference_like(&text[index + 1..]) => out.push_str("\\&"),
            '\n' => out.push_str("&#10;"),
            '\r' => out.push_str("&#13;"),
            '\0' => out.push(char::REPLACEMENT_CHARACTER),
            _ => out.push(c),
        }
    }
    out
}

/// Whether what follows a `&` could make it a character reference: an optional `#`, one or more
/// ASCII letters and digits, then `;`. Every entity and numeric reference CommonMark reads is one,
/// and a `&` escaped that begins none still reads as itself. A writer that escapes only the `&`s
/// that could begin a reference, as docproc-mail does a text `&`, asks it of what follows each.
pub fn reference_like(rest: &str) -> bool {
    let rest = rest.strip_prefix('#').unwrap_or(rest);
    let name = rest.bytes().take_while(u8::is_ascii_alphanumeric).count();
    name > 0 && rest.as_bytes().get(name) == Some(&b';')
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::cases;

    /// Each case is a whole line of literal text.
    #[test]
    fn literal_text_makes_its_tables_markdown() {
        for case in cases::group("literal_escape") {
            let markdown = escape(cases::text(&case, "text"), true);
            assert_eq!(markdown, cases::text(&case, "markdown"), "{case}");
        }
    }

    /// Past a line's start, a first character or a run of digits is text like any other.
    #[test]
    fn text_past_a_lines_start_escapes_no_line_markup() {
        assert_eq!(escape("# x", false), "# x");
        assert_eq!(escape("1. x", false), "1. x");
        assert_eq!(escape("*x*", false), "\\*x\\*");
    }

    /// Each case's target is written as its destination.
    #[test]
    fn a_target_makes_its_tables_destination() {
        for case in cases::group("destinations") {
            let markdown = destination(cases::text(&case, "target"));
            assert_eq!(markdown, cases::text(&case, "markdown"), "{case}");
        }
    }

    /// Each case's title is written as its Markdown.
    #[test]
    fn a_title_makes_its_tables_markdown() {
        for case in cases::group("titles") {
            let markdown = title(cases::text(&case, "title"));
            assert_eq!(markdown, cases::text(&case, "markdown"), "{case}");
        }
    }

    /// CommonMark reads each case's title, in a link and in an image, back as written, or as its
    /// `reads` where it has one.
    #[cfg(feature = "markdown")]
    #[test]
    fn a_title_reads_back_as_written() {
        use pulldown_cmark::{Event, Parser, Tag};
        for case in cases::group("titles") {
            let written = case["reads"]
                .as_str()
                .unwrap_or_else(|| cases::text(&case, "title"));
            let markdown = title(cases::text(&case, "title"));
            let both = format!("[x](y \"{markdown}\") ![x](y \"{markdown}\")");
            let read: Vec<String> = Parser::new(&both)
                .filter_map(|event| match event {
                    Event::Start(Tag::Link { title, .. } | Tag::Image { title, .. }) => {
                        Some(title.into_string())
                    }
                    _ => None,
                })
                .collect();
            assert_eq!(read, [written, written], "{case}: {both}");
        }
    }

    /// CommonMark reads each case's destination, in a link, back as its target, or as its
    /// `reads` where it has one.
    #[cfg(feature = "markdown")]
    #[test]
    fn a_destination_reads_back_as_its_target() {
        use pulldown_cmark::{Event, Parser, Tag};
        for case in cases::group("destinations") {
            let target = case["reads"]
                .as_str()
                .unwrap_or_else(|| cases::text(&case, "target"));
            let link = format!("[x]({})", destination(cases::text(&case, "target")));
            let read = Parser::new(&link).find_map(|event| match event {
                Event::Start(Tag::Link { dest_url, .. }) => Some(dest_url.into_string()),
                _ => None,
            });
            assert_eq!(read.as_deref(), Some(target), "{case}: {link}");
        }
    }
}
