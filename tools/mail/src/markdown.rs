//! A message's Markdown, made by htmd from HTML, or from plain text through the envelope's
//! literal-text escape, so that Markdown reads none of the text as markup.
//!
//! htmd walks the document recursively, and html5ever caps no depth, so an HTML body nested a few
//! thousand elements deep would overflow the stack, a fault no guard catches, and end the whole
//! run by signal. A pass over the HTML's tags counts its open elements first, as the HTML parser
//! would leave them open ([`too_deep`]), and a body nested past [`NESTING_MAX`] never reaches
//! htmd.

use docproc_envelope::markdown::escape;

/// The most open elements an HTML body may nest for htmd to read it: Blink's cap on the depth
/// of the tree its parser builds.
pub const NESTING_MAX: usize = 512;

/// A message's Markdown, by the one rule every format follows: made from `html` when the
/// message has an HTML body, else from `text`, read literally. An HTML body nested past
/// [`NESTING_MAX`] open elements gives way to `text`, else to its own text with its tags stripped,
/// with a note. One that can't be made is left out, with a note.
pub fn derive(html: Option<&str>, text: Option<&str>, notes: &mut Vec<String>) -> Option<String> {
    let made = match (html, text) {
        (Some(html), _) if too_deep(html) => {
            let (made, from) = match text {
                Some(text) => (from_text(text), "the text body"),
                None => (
                    from_text(&stripped(html)),
                    "the HTML's text, its tags stripped",
                ),
            };
            notes.push(format!(
                "the HTML body nests more than {NESTING_MAX} open elements; \
                 the Markdown is made from {from}"
            ));
            Ok(made)
        }
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

/// Elements that never hold content, so a start tag of one opens nothing.
const VOID: &[&str] = &[
    "area", "base", "basefont", "bgsound", "br", "col", "embed", "frame", "hr", "img", "input",
    "keygen", "link", "meta", "param", "source", "track", "wbr",
];

/// Elements whose content is text up to their end tag, never tags; the first three are no part
/// of a message's text.
const RAW: &[&str] = &[
    "script", "style", "title", "textarea", "xmp", "iframe", "noembed", "noframes",
];

/// Elements whose start or end ends a line of the HTML's text.
const BREAKS: &[&str] = &[
    "address",
    "article",
    "aside",
    "blockquote",
    "br",
    "dd",
    "div",
    "dl",
    "dt",
    "figure",
    "footer",
    "form",
    "h1",
    "h2",
    "h3",
    "h4",
    "h5",
    "h6",
    "header",
    "hr",
    "li",
    "main",
    "nav",
    "ol",
    "p",
    "pre",
    "section",
    "table",
    "tr",
    "ul",
];

/// Whether `html` nests more than [`NESTING_MAX`] open elements. Each start tag opens an element
/// but a void one's, and a self-closing one's inside `svg` or `math`, where `/>` closes it as it
/// doesn't in HTML; an end tag closes the most recent open element of its name and those opened
/// after it, and one with no such element is ignored, as the HTML parser does. A start tag also
/// closes the open element the parser closes for it in the common case, the one just before it:
/// a `p`, `li`, `dt`, `dd`, `option`, `tr`, `td`, or `th` the same as it, or an open `p` before a
/// block. The count errs high rather than low, so a body it lets through htmd reads.
fn too_deep(html: &str) -> bool {
    let mut open: Vec<String> = Vec::new();
    let mut foreign = 0usize;
    for piece in Pieces::new(html) {
        match piece {
            Piece::Start(name, self_closing) => {
                while open.last().is_some_and(|last| closes(last, &name)) {
                    open.pop();
                }
                if VOID.contains(&name.as_str()) || self_closing && foreign > 0 {
                    continue;
                }
                foreign += usize::from(matches!(name.as_str(), "svg" | "math"));
                open.push(name);
                if open.len() > NESTING_MAX {
                    return true;
                }
            }
            Piece::End(name) => {
                if let Some(at) = open.iter().rposition(|open| *open == name) {
                    foreign -= open[at..]
                        .iter()
                        .filter(|open| matches!(open.as_str(), "svg" | "math"))
                        .count();
                    open.truncate(at);
                }
            }
            Piece::Text(_) => {}
        }
    }
    false
}

/// Whether a start tag `name` closes the open element `last` just before it.
fn closes(last: &str, name: &str) -> bool {
    let cell = |name: &str| matches!(name, "td" | "th");
    let term = |name: &str| matches!(name, "dt" | "dd");
    match last {
        "p" => name == "p" || BREAKS.contains(&name) && !matches!(name, "br" | "hr"),
        "li" | "option" | "tr" => name == last,
        _ if cell(last) => cell(name) || name == "tr",
        _ if term(last) => term(name),
        _ => false,
    }
}

/// The text of `html` with its tags stripped: its text, character references decoded and its
/// line breaks spaces, as it renders, with a line break where a block starts or ends. A run of
/// spaces is left for [`from_text`] to read as one.
fn stripped(html: &str) -> String {
    let mut text = String::with_capacity(html.len());
    for piece in Pieces::new(html) {
        match piece {
            Piece::Text(run) => text.extend(
                references(run)
                    .chars()
                    .map(|c| if c.is_ascii_whitespace() { ' ' } else { c }),
            ),
            Piece::Start(name, _) | Piece::End(name) if BREAKS.contains(&name.as_str()) => {
                text.push('\n');
            }
            _ => {}
        }
    }
    text
}

/// `text` with its character references decoded: the numeric ones, and the named ones a message's
/// text commonly holds. Any other is kept as written.
fn references(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    let mut rest = text;
    while let Some(at) = rest.find('&') {
        out.push_str(&rest[..at]);
        rest = &rest[at..];
        let decoded = rest[1..]
            .find(';')
            .filter(|end| *end <= 10)
            .and_then(|end| {
                let name = &rest[1..=end];
                let c = match name {
                    "amp" => Some('&'),
                    "lt" => Some('<'),
                    "gt" => Some('>'),
                    "quot" => Some('"'),
                    "apos" => Some('\''),
                    "nbsp" => Some('\u{A0}'),
                    _ => {
                        let number = name.strip_prefix('#')?;
                        let code = match number.strip_prefix(['x', 'X']) {
                            Some(hex) => u32::from_str_radix(hex, 16),
                            None => number.parse(),
                        };
                        code.ok().and_then(char::from_u32)
                    }
                };
                c.map(|c| (c, end + 2))
            });
        match decoded {
            Some((c, length)) => {
                out.push(c);
                rest = &rest[length..];
            }
            None => {
                out.push('&');
                rest = &rest[1..];
            }
        }
    }
    out.push_str(rest);
    out
}

/// A piece of HTML as its tag pass reads it: a run of text, or a start or end tag by its name in
/// ASCII lowercase, a start tag with whether it ends `/>`. Comments, doctypes, and processing
/// instructions are passed over, and a raw-text element's content is text, or nothing for the
/// elements no part of the text.
enum Piece<'a> {
    Text(&'a str),
    Start(String, bool),
    End(String),
}

struct Pieces<'a> {
    html: &'a str,
    at: usize,
    /// The raw-text element whose content comes next.
    raw: Option<String>,
}

impl<'a> Pieces<'a> {
    fn new(html: &'a str) -> Self {
        Self {
            html,
            at: 0,
            raw: None,
        }
    }

    /// The offset in the rest of the HTML just past the first `>` from `from`, or its end, with
    /// quoted attribute values passed over when `quoted`.
    fn past_tag(rest: &[u8], from: usize, quoted: bool) -> usize {
        let mut quote = None;
        for (at, &byte) in rest.iter().enumerate().skip(from) {
            match quote {
                Some(open) if byte == open => quote = None,
                Some(_) => {}
                None if byte == b'>' => return at + 1,
                None if quoted && matches!(byte, b'"' | b'\'') => quote = Some(byte),
                None => {}
            }
        }
        rest.len()
    }

    /// The tag name starting at `from` in the rest, in ASCII lowercase, and where it ends.
    fn name(rest: &str, from: usize) -> (String, usize) {
        let end = rest[from..]
            .find(|c: char| c.is_ascii_whitespace() || c == '/' || c == '>')
            .map_or(rest.len(), |end| from + end);
        (rest[from..end].to_ascii_lowercase(), end)
    }
}

impl<'a> Iterator for Pieces<'a> {
    type Item = Piece<'a>;

    fn next(&mut self) -> Option<Piece<'a>> {
        loop {
            let rest = &self.html[self.at..];
            if rest.is_empty() {
                return None;
            }
            if let Some(name) = self.raw.take() {
                let close = format!("</{name}");
                let end = rest
                    .as_bytes()
                    .windows(close.len())
                    .position(|window| window.eq_ignore_ascii_case(close.as_bytes()))
                    .unwrap_or(rest.len());
                self.at += end;
                if end == 0 || matches!(name.as_str(), "script" | "style" | "title") {
                    continue;
                }
                return Some(Piece::Text(&rest[..end]));
            }
            let bytes = rest.as_bytes();
            if bytes[0] != b'<' {
                let end = rest.find('<').unwrap_or(rest.len());
                self.at += end;
                return Some(Piece::Text(&rest[..end]));
            }
            match bytes.get(1) {
                Some(b'!') if rest.starts_with("<!--") => {
                    self.at += rest[4..].find("-->").map_or(rest.len(), |end| 4 + end + 3);
                }
                Some(b'!' | b'?') => self.at += Self::past_tag(bytes, 2, false),
                Some(b'/') if bytes.get(2).is_some_and(u8::is_ascii_alphabetic) => {
                    let (name, end) = Self::name(rest, 2);
                    self.at += Self::past_tag(bytes, end, false);
                    return Some(Piece::End(name));
                }
                Some(letter) if letter.is_ascii_alphabetic() => {
                    let (name, end) = Self::name(rest, 1);
                    let past = Self::past_tag(bytes, end, true);
                    let self_closing = bytes[..past].ends_with(b"/>");
                    self.at += past;
                    if RAW.contains(&name.as_str()) {
                        self.raw = Some(name.clone());
                    }
                    return Some(Piece::Start(name, self_closing));
                }
                // A "<" that opens no tag is text.
                _ => {
                    self.at += 1;
                    return Some(Piece::Text(&rest[..1]));
                }
            }
        }
    }
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

    /// `depth` nested elements of `tag` around a word, the outer one closed.
    fn nested(tag: &str, depth: usize) -> String {
        format!("{}word</{tag}>", format!("<{tag}>").repeat(depth))
    }

    /// The cap itself: 512 open elements are htmd's to read, and the 513th gives way to the text
    /// body, else to the HTML's text, each with a note.
    #[test]
    fn html_past_the_nesting_cap_gives_way() {
        let mut notes = Vec::new();
        let at_cap = derive(Some(&nested("div", 512)), Some("text"), &mut notes);
        assert_eq!(at_cap.as_deref(), Some("word"));
        assert!(notes.is_empty(), "{notes:?}");

        let past = nested("div", 513);
        let from_text = derive(Some(&past), Some("the *text* body"), &mut notes);
        assert_eq!(from_text.as_deref(), Some("the \\*text\\* body"));
        let from_html = derive(Some(&past), None, &mut notes);
        assert_eq!(from_html.as_deref(), Some("word"));
        assert_eq!(
            notes,
            [
                "the HTML body nests more than 512 open elements; \
                 the Markdown is made from the text body",
                "the HTML body nests more than 512 open elements; \
                 the Markdown is made from the HTML's text, its tags stripped",
            ]
        );
    }

    /// What the tag pass counts as open: a void element, a closed one, a self-closing one in SVG,
    /// a tag in a comment or a script, and a `p`, `li`, or cell the next one closes open nothing;
    /// unclosed elements, and `<div/>`, which HTML doesn't close, do.
    #[test]
    fn the_tag_pass_counts_what_the_parser_leaves_open() {
        let many = |piece: &str| piece.repeat(NESTING_MAX + 1);
        for flat in [
            many("<br><img src=x><hr/>"),
            many("<div class='a>b'>x</div >"),
            format!("<svg>{}</svg>", many("<path d=x/>")),
            many("<!-- <div> --><script>'<div>'</script><style><b></style>"),
            many("<p>a"),
            many("<li>a"),
            format!("<table>{}</table>", many("<tr><td>a<td>b")),
            many("<p>a<div>b</div>"),
            many("a < b <3 </> <!doctype html><?xml ?>"),
            many("<b>x</B>"),
        ] {
            assert!(!too_deep(&flat), "{flat:.80}");
        }
        for deep in [
            many("<div>"),
            many("<font>x"),
            many("<div/>"),
            many("<DIV>"),
            format!("<svg>{}</svg>", many("<g>")),
        ] {
            assert!(too_deep(&deep), "{deep:.80}");
        }
    }

    /// The HTML's text: its tags stripped, its references decoded, its whitespace as it renders,
    /// and a line break at each block, with neither a script's nor a style's text.
    #[test]
    fn the_htmls_text_is_its_text_with_its_tags_stripped() {
        let html = "<html><head><title>T</title><style>p {}</style></head><body>\
                    <p>Fish &amp; chips\r\n  at <b>five</b>&#x2c; &lt;sharp&gt;</p><div>a&nbsp;b \
                    &copy; &#99999999; &amp</div><script>x()</script>tail</body></html>";
        assert_eq!(
            stripped(html),
            "\nFish & chips    at five, <sharp>\n\na\u{A0}b &copy; &#99999999; &amp\ntail"
        );
        assert_eq!(
            from_text(&stripped(html)),
            "Fish \\& chips at five, \\<sharp>\n\na\u{A0}b \\&copy; \\&#99999999; \\&amp  \ntail"
        );
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
