//! The Markdown the tools write around their readers' text: the literal-text escape and the
//! destination writer. The escape is htmd's rules, which every tool's Markdown shares
//! (context/layers.md, "Common envelope", Text layer).

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

/// A link's or an image's target as a Markdown destination. One holding a space, a control
/// character, `<`, or `>` is written `<…>`, its `<` and `>` escaped; any other is written bare,
/// its parentheses escaped, so neither form ends before the target does.
pub fn destination(target: &str) -> String {
    if target
        .chars()
        .any(|c| c == ' ' || c == '<' || c == '>' || c.is_control())
    {
        format!("<{}>", target.replace('<', "\\<").replace('>', "\\>"))
    } else {
        target.replace('(', "\\(").replace(')', "\\)")
    }
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

    #[test]
    fn a_plain_destination_is_bare_with_its_parentheses_escaped() {
        assert_eq!(destination("abc.png"), "abc.png");
        assert_eq!(
            destination("https://en.wikipedia.org/wiki/Moth_(disambiguation)"),
            "https://en.wikipedia.org/wiki/Moth_\\(disambiguation\\)"
        );
    }

    #[test]
    fn a_spaced_or_bracketed_destination_is_angled() {
        assert_eq!(destination("my file (1).png"), "<my file (1).png>");
        assert_eq!(destination("a<b>c"), "<a\\<b\\>c>");
        assert_eq!(destination("a\tb"), "<a\tb>");
        assert_eq!(destination("x y <z>"), "<x y \\<z\\>>");
    }
}
