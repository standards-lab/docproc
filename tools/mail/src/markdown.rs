//! The derived Markdown body, made by htmd from HTML, or from plain text through the envelope's
//! literal-text escape, so that Markdown reads none of the text as markup.

use docproc_envelope::markdown::escape;

/// The derived Markdown body, by the one rule every format follows: made from `html` when the
/// message has an HTML body, else from `text`, read literally. One that can't be made is left
/// out, with a note.
pub fn derive(html: Option<&str>, text: Option<&str>, notes: &mut Vec<String>) -> Option<String> {
    let made = match (html, text) {
        (Some(html), _) => from_html(html),
        (None, Some(text)) => Ok(from_text(text)),
        (None, None) => return None,
    };
    made.map_err(|reason| notes.push(format!("the Markdown body {reason}; left out")))
        .ok()
}

/// Markdown from an HTML body. Images keep their sources as written, `cid:` ones included. The
/// head, styles and scripts are no part of the text: htmd would write a style's rules as text.
pub fn from_html(html: &str) -> Result<String, String> {
    htmd::HtmlToMarkdown::builder()
        .skip_tags(vec!["head", "style", "script"])
        .build()
        .convert(html)
        .map_err(|err| format!("can't be made into Markdown ({err})"))
}

/// Markdown that reads as `text` does: its paragraphs, split at blank lines, each line escaped
/// as a whole line of literal text and ended by a hard line break, `"  \n"`, but the paragraph's
/// last. A line feed, a carriage return and line feed, or a lone carriage return ends a line.
/// As in HTML, which the spike's text body went through, a run of ASCII whitespace within a line
/// reads as one space, and a line's leading and trailing ASCII whitespace is dropped: Markdown
/// would read the one as an indented code block or a list's continuation, and the other as a
/// line break.
pub fn from_text(text: &str) -> String {
    let text = text.replace("\r\n", "\n").replace('\r', "\n");
    let mut out = String::with_capacity(text.len() + 64);
    let mut paragraph = false;
    let mut first = true;
    for line in text.split('\n') {
        let line = line
            .split(|c: char| c.is_ascii_whitespace())
            .filter(|word| !word.is_empty())
            .collect::<Vec<_>>()
            .join(" ");
        if line.is_empty() {
            paragraph = false;
            continue;
        }
        if paragraph {
            out.push_str("  \n");
        } else if !first {
            out.push_str("\n\n");
        }
        out.push_str(&escape(&line, true));
        paragraph = true;
        first = false;
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn text_reads_literally() {
        let text = "Hi *all* _x_ `c` [a](b) <t> & a|b ~~s~~ \\ end\r\n\
                    # not a heading\r\n- not an item\r\n> not a quote\r\n\
                    1) not an item\r\n2. not an item\r\nunderlined\r\n-----\r\n\
                    ===\r\n+ x\r\n\r\nnext paragraph";
        assert_eq!(
            from_text(text),
            "Hi \\*all\\* \\_x\\_ \\`c\\` \\[a\\](b) \\<t> \\& a\\|b \\~\\~s\\~\\~ \\\\ end  \n\
             \\# not a heading  \n\\- not an item  \n\\> not a quote  \n\
             1\\) not an item  \n2\\. not an item  \nunderlined  \n\\-----  \n\
             \\===  \n\\+ x\n\nnext paragraph"
        );
    }

    /// A line's edges are dropped, so an indented line is no code block and a line ending in
    /// spaces no extra break, and a run of whitespace within it is one space; a run of blank lines
    /// is one paragraph break. A no-break space is text.
    #[test]
    fn whitespace_reads_as_in_html() {
        let text = "\r\n    indented \t # mid  \r  - item\r\n \t \r\n\r\nafter   three\n\n\u{a0} x";
        assert_eq!(
            from_text(text),
            "indented # mid  \n\\- item\n\nafter three\n\n\u{a0} x"
        );
        assert_eq!(from_text(" \r\n\t"), "");
    }

    #[test]
    fn html_keeps_cid_images_and_drops_styles() {
        let html = "<html><head><title>T</title><style>p {color: red}</style></head>\
                    <body><p>A <b>moth</b>.</p><img src=\"cid:moth@example.net\" alt=\"moth\">\
                    <script>x()</script></body></html>";
        assert_eq!(
            from_html(html).unwrap(),
            "A **moth**.\n\n![moth](cid:moth@example.net)"
        );
    }
}
