//! A message's Markdown, made by htmd from HTML, or from plain text through the envelope's
//! literal-text escape, so that Markdown reads none of the text as markup.
//!
//! htmd walks the document recursively, and html5ever caps no depth, so an HTML body nested a few
//! thousand elements deep would overflow the stack, a fault no guard catches, and end the whole
//! run by signal. A pass over the HTML's tags counts its open elements first, as the HTML parser
//! would leave them open ([`too_deep`]), and a body nested past [`NESTING_MAX`] never reaches
//! htmd.
//!
//! The Markdown is made once a message's attachments are read, nested messages included, so that
//! its images link media names ([`make`]). Before htmd reads the HTML, each `<img>`'s source is
//! rewritten to the media name of what it names: a `cid:` source, percent-decoded, names the
//! attachment whose content_id it is, and any other source the attachment whose Content-Location
//! it is. A `data:` source is its own payload, written as media and listed in the message's
//! `media`. A source that names nothing is kept as written, with a note. An attachment is
//! `inline` when the Markdown links it, and a `data:` payload is written, and listed, only when
//! the Markdown links it, so every file written is named by a line.
//!
//! Markdown made from HTML is compared with the message's source text by the content rule, the
//! shared crate's (context/layers.md, schema "2"): the source text's sentences the Markdown lacks
//! are appended to it, in order, as one paragraph escaped as a text body's Markdown is
//! ([`with_missing`]). The source text is the text body, else the RTF body's own text.

use std::borrow::Cow;
use std::collections::HashSet;
use std::ops::Range;

use docproc_envelope::content::{self, Text};
use docproc_envelope::markdown::escape;
use docproc_envelope::{Fault, Media};
use mail_parser::Encoding;

use crate::model::{Medium, Message};

/// The most open elements an HTML body may nest for htmd to read it: Blink's cap on the depth
/// of the tree its parser builds.
pub const NESTING_MAX: usize = 512;

/// A message's bodies as its Markdown is made from them: its HTML, stored or recovered from its
/// RTF body, and its plain text, the text body or else the RTF body's own text. Beside HTML and no
/// text body, `rtf_text` is the RTF body's own text, read for the content rule alone.
pub struct Bodies {
    pub html: Option<String>,
    pub text: Option<String>,
    pub rtf_text: Option<String>,
}

/// Makes `message`'s Markdown from `bodies`, once its attachments are read: its images linked to
/// their media names, each attachment's `inline` set by whether the Markdown links it, and the
/// `data:` payloads it links written through `files` and listed in its `media`. `locations` is
/// each attachment's Content-Location, in the attachments' order, read for matching only. Notes
/// go to `notes`; a payload that can't be written is a media fault.
pub fn make(
    message: &mut Message,
    bodies: &Bodies,
    locations: &[Option<String>],
    files: &mut Media,
    notes: &mut Vec<String>,
) -> Result<(), Fault> {
    let mut images = Images::new(message, locations);
    let markdown = derive(
        bodies.html.as_deref(),
        bodies.text.as_deref(),
        bodies.text.as_deref().or(bodies.rtf_text.as_deref()),
        &mut |src| images.resolve(src),
        notes,
    );
    let Images {
        linked,
        data,
        notes: mut image_notes,
        ..
    } = images;
    notes.append(&mut image_notes);
    let links = |file: &str| markdown.as_deref().and_then(|markdown| markdown.find(file));
    for (attachment, linked) in message.attachments.iter_mut().zip(linked) {
        attachment.inline = linked && attachment.file.as_deref().and_then(links).is_some();
    }
    // A payload an attachment already names is that attachment's, and not listed again.
    let mut media: Vec<(usize, Data)> = data
        .into_iter()
        .filter(|data| {
            !message
                .attachments
                .iter()
                .any(|attachment| attachment.file.as_deref() == Some(data.name.as_str()))
        })
        .filter_map(|data| Some((links(&data.name)?, data)))
        .collect();
    media.sort_by_key(|(at, _)| *at);
    for (_, data) in media {
        let file = files.store(data.bytes.as_slice(), data.extension.as_deref())?;
        message.media.push(Medium {
            file,
            mime_type: data.mime_type,
        });
    }
    message.markdown = markdown;
    Ok(())
}

/// The images' sources resolved while the HTML is rewritten: what each attachment can be named
/// by, which ones a source named, the `data:` payloads, and the notes.
struct Images {
    /// Each attachment's content_id, without angle brackets, its Content-Location, and its media
    /// name; an attachment with no media name is named by no source.
    targets: Vec<(Option<String>, Option<String>, Option<String>)>,
    /// Whether a source named each attachment.
    linked: Vec<bool>,
    /// The `data:` payloads, each once, in the HTML's order.
    data: Vec<Data>,
    /// The sources already noted as naming nothing, each noted once.
    unmatched: HashSet<String>,
    notes: Vec<String>,
}

/// A `data:` image's payload, decoded, its name hashed but not yet written.
struct Data {
    name: String,
    bytes: Vec<u8>,
    mime_type: Option<String>,
    extension: Option<String>,
}

impl Images {
    fn new(message: &Message, locations: &[Option<String>]) -> Self {
        let targets = message
            .attachments
            .iter()
            .enumerate()
            .map(|(at, attachment)| {
                let content_id = attachment.content_id.as_deref().map(|id| {
                    let id = id.trim();
                    id.strip_prefix('<')
                        .and_then(|id| id.strip_suffix('>'))
                        .unwrap_or(id)
                        .to_owned()
                });
                let location = locations.get(at).cloned().flatten();
                (content_id, location, attachment.file.clone())
            })
            .collect::<Vec<_>>();
        Self {
            linked: vec![false; targets.len()],
            targets,
            data: Vec::new(),
            unmatched: HashSet::new(),
            notes: Vec::new(),
        }
    }

    /// The media name an image's source names, its character references already decoded, or None
    /// to keep it as written.
    fn resolve(&mut self, src: &str) -> Option<String> {
        // A URL in an attribute is read without its leading and trailing whitespace.
        let src = src.trim_matches(|c: char| c.is_ascii_whitespace());
        if src.is_empty() {
            return None;
        }
        let found = if let Some(id) = scheme(src, "cid:") {
            let id = percent_decoded(id.as_bytes());
            let id = String::from_utf8_lossy(&id);
            self.target(|(content_id, _, _)| content_id.as_deref() == Some(&*id))
        } else if let Some(data) = scheme(src, "data:") {
            return self.data(data);
        } else {
            self.target(|(_, location, _)| location.as_deref() == Some(src))
        };
        if found.is_none() && self.unmatched.insert(src.to_owned()) {
            self.notes.push(format!(
                "an image's source {src:?} names no attachment; kept as written"
            ));
        }
        found
    }

    /// The media name of the first attachment with one that `names` matches, which is now linked.
    fn target(
        &mut self,
        names: impl Fn(&(Option<String>, Option<String>, Option<String>)) -> bool,
    ) -> Option<String> {
        let at = self
            .targets
            .iter()
            .position(|target| target.2.is_some() && names(target))?;
        self.linked[at] = true;
        self.targets[at].2.clone()
    }

    /// A `data:` URL's payload's media name (RFC 2397: `data:[<mediatype>][;base64],<data>`),
    /// hashed now and written once the Markdown is known to link it; one that can't be decoded is
    /// kept as written, with a note. `url` follows `data:`.
    fn data(&mut self, url: &str) -> Option<String> {
        let decoded = url.split_once(',').and_then(|(header, payload)| {
            let (media_type, base64) = match header.rsplit_once(';') {
                Some((media_type, last)) if last.eq_ignore_ascii_case("base64") => {
                    (media_type, true)
                }
                _ => (header, false),
            };
            let payload = percent_decoded(payload.as_bytes());
            let bytes = if base64 {
                let (bytes, malformed) = Encoding::Base64.decode_checked(&payload);
                (!malformed).then(|| bytes.into_owned())?
            } else {
                payload
            };
            let essence = media_type.split(';').next().unwrap_or_default().trim();
            let mime_type = (!essence.is_empty()).then(|| essence.to_ascii_lowercase());
            Some((bytes, mime_type))
        });
        let Some((bytes, mime_type)) = decoded else {
            self.notes
                .push("an image's data: URL can't be decoded; kept as written".to_owned());
            return None;
        };
        // The name a payload gets is its hash and an extension, here the media type's subtype,
        // which the store keeps when it is short and alphanumeric, as "png" and "jpeg" are.
        let extension = mime_type
            .as_deref()
            .and_then(|mime_type| mime_type.split_once('/'))
            .map(|(_, subtype)| subtype.to_owned());
        let name = Media::new(None)
            .store(bytes.as_slice(), extension.as_deref())
            .ok()?;
        if !self.data.iter().any(|data| data.name == name) {
            self.data.push(Data {
                name: name.clone(),
                bytes,
                mime_type,
                extension,
            });
        }
        Some(name)
    }
}

/// What follows `scheme`, matched without regard to ASCII case, as a URL's scheme is, when `src`
/// starts with it.
fn scheme<'a>(src: &'a str, scheme: &str) -> Option<&'a str> {
    src.get(..scheme.len())
        .filter(|start| start.eq_ignore_ascii_case(scheme))
        .map(|_| &src[scheme.len()..])
}

/// `bytes` with each `%hh` decoded; a `%` not followed by two hex digits is kept.
fn percent_decoded(bytes: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(bytes.len());
    let mut at = 0;
    while at < bytes.len() {
        let hex = bytes
            .get(at + 1..at + 3)
            .filter(|_| bytes[at] == b'%')
            .and_then(|hex| std::str::from_utf8(hex).ok())
            .and_then(|hex| u8::from_str_radix(hex, 16).ok());
        match hex {
            Some(byte) => {
                out.push(byte);
                at += 3;
            }
            None => {
                out.push(bytes[at]);
                at += 1;
            }
        }
    }
    out
}

/// A message's Markdown, by the one rule every format follows: made from `html` when the
/// message has an HTML body, its images' sources first rewritten by `images`, and the sentences
/// of `source` it lacks appended ([`with_missing`]), else from `text`, read literally. An HTML
/// body nested past [`NESTING_MAX`] open elements gives way to `text`, else to its own text with
/// its tags stripped, with a note, and its images are not read. One that can't be made is left
/// out, with a note.
pub fn derive(
    html: Option<&str>,
    text: Option<&str>,
    source: Option<&str>,
    images: &mut dyn FnMut(&str) -> Option<String>,
    notes: &mut Vec<String>,
) -> Option<String> {
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
        (Some(html), _) => {
            from_html(&rewritten(html, images)).map(|markdown| with_missing(markdown, source))
        }
        (None, Some(text)) => Ok(from_text(text)),
        (None, None) => return None,
    };
    made.map_err(|reason| notes.push(format!("the Markdown body {reason}; left out")))
        .ok()
}

/// `markdown`, made from HTML, followed by the sentences of `source` it lacks by the content rule,
/// in order, as one paragraph: joined by a space, and escaped as a line of a text body is.
fn with_missing(markdown: String, source: Option<&str>) -> String {
    let Some(source) = source else {
        return markdown;
    };
    let missing = content::missing(&Text::plain(source), &Text::markdown(&markdown));
    if missing.is_empty() {
        return markdown;
    }
    let paragraph = from_text(&missing.join(" "));
    if markdown.trim().is_empty() {
        paragraph
    } else {
        format!("{markdown}\n\n{paragraph}")
    }
}

/// `html` with each `<img>`'s `src` rewritten to the name `images` gives for it, its character
/// references decoded first; a source it gives none for is kept as written. A rewritten value is
/// written in double quotes, and a media name needs no escape in them.
fn rewritten<'h>(html: &'h str, images: &mut dyn FnMut(&str) -> Option<String>) -> Cow<'h, str> {
    let mut out: Option<String> = None;
    let mut copied = 0;
    for piece in Pieces::new(html) {
        let Piece::Start(name, _, tag) = piece else {
            continue;
        };
        if name != "img" {
            continue;
        }
        let Some((whole, value)) = src_attribute(&html[tag.clone()]) else {
            continue;
        };
        let Some(media) = images(&references(
            &html[tag.start + value.start..tag.start + value.end],
        )) else {
            continue;
        };
        let out = out.get_or_insert_with(|| String::with_capacity(html.len()));
        out.push_str(&html[copied..tag.start + whole.start]);
        out.push('"');
        out.push_str(&media);
        out.push('"');
        copied = tag.start + whole.end;
    }
    match out {
        Some(mut out) => {
            out.push_str(&html[copied..]);
            Cow::Owned(out)
        }
        None => Cow::Borrowed(html),
    }
}

/// Where a start tag's first `src` attribute's value is, in `tag`: the value as written, quotes
/// included, and the value itself, read as the HTML tokenizer reads attributes.
fn src_attribute(tag: &str) -> Option<(Range<usize>, Range<usize>)> {
    let bytes = tag.as_bytes();
    let space = |byte: u8| byte.is_ascii_whitespace();
    let mut at = 1;
    while at < bytes.len() && !space(bytes[at]) && !matches!(bytes[at], b'/' | b'>') {
        at += 1;
    }
    loop {
        while at < bytes.len() && (space(bytes[at]) || bytes[at] == b'/') {
            at += 1;
        }
        if at >= bytes.len() || bytes[at] == b'>' {
            return None;
        }
        let name_start = at;
        at += 1;
        while at < bytes.len() && !space(bytes[at]) && !matches!(bytes[at], b'/' | b'>' | b'=') {
            at += 1;
        }
        let name = &tag[name_start..at];
        while at < bytes.len() && space(bytes[at]) {
            at += 1;
        }
        if bytes.get(at) != Some(&b'=') {
            continue;
        }
        at += 1;
        while at < bytes.len() && space(bytes[at]) {
            at += 1;
        }
        let start = at;
        let (whole, value) = match bytes.get(at) {
            Some(&quote @ (b'"' | b'\'')) => {
                let end = bytes[at + 1..]
                    .iter()
                    .position(|&byte| byte == quote)
                    .map_or(bytes.len(), |end| at + 1 + end);
                at = (end + 1).min(bytes.len());
                (start..at, start + 1..end)
            }
            _ => {
                while at < bytes.len() && !space(bytes[at]) && bytes[at] != b'>' {
                    at += 1;
                }
                (start..at, start..at)
            }
        };
        if name.eq_ignore_ascii_case("src") {
            return Some((whole, value));
        }
    }
}

/// Markdown from an HTML body. Images keep their sources as written. The head, styles and scripts are no part of the text: htmd would write a style's rules as text.
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
            Piece::Start(name, self_closing, _) => {
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
            Piece::Start(name, ..) | Piece::End(name) if BREAKS.contains(&name.as_str()) => {
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
/// ASCII lowercase, a start tag with whether it ends `/>` and where the whole tag is in the HTML. Comments, doctypes, and processing
/// instructions are passed over, and a raw-text element's content is text, or nothing for the
/// elements no part of the text.
enum Piece<'a> {
    Text(&'a str),
    Start(String, bool, Range<usize>),
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
                    let tag = self.at..self.at + past;
                    self.at += past;
                    if RAW.contains(&name.as_str()) {
                        self.raw = Some(name.clone());
                    }
                    return Some(Piece::Start(name, self_closing, tag));
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

    /// Markdown made from HTML is followed by the source text's sentences it lacks, in order, as
    /// one paragraph escaped as a text body's line; HTML that lacks none, and Markdown made from
    /// the text itself, gain nothing.
    #[test]
    fn html_markdown_gains_the_sentences_it_lacks() {
        let mut notes = Vec::new();
        let source = "We found a moth.\r\n# It is *taped* in. Done\r\n";
        let made = |html: Option<&str>, notes: &mut Vec<String>| {
            derive(html, Some(source), Some(source), &mut |_| None, notes)
        };
        assert_eq!(
            made(Some("<p>We found a <b>moth</b>!</p>"), &mut notes).as_deref(),
            Some("We found a **moth**!\n\n\\# It is \\*taped\\* in. Done")
        );
        assert_eq!(
            made(
                Some("<p>We found a moth. It is taped in.</p><p>Done</p>"),
                &mut notes
            )
            .as_deref(),
            Some("We found a moth. It is taped in.\n\nDone")
        );
        assert_eq!(made(None, &mut notes), Some(from_text(source)));
        let lacking = derive(
            Some("<p>x</p>"),
            None,
            Some("Only the RTF's text."),
            &mut |_| None,
            &mut notes,
        );
        assert_eq!(lacking.as_deref(), Some("x\n\nOnly the RTF's text."));
        assert!(notes.is_empty(), "{notes:?}");
    }

    /// The cap itself: 512 open elements are htmd's to read, and the 513th gives way to the text
    /// body, else to the HTML's text, each with a note.
    #[test]
    fn html_past_the_nesting_cap_gives_way() {
        let mut notes = Vec::new();
        let at_cap = derive(
            Some(&nested("div", 512)),
            Some("text"),
            None,
            &mut |_| None,
            &mut notes,
        );
        assert_eq!(at_cap.as_deref(), Some("word"));
        assert!(notes.is_empty(), "{notes:?}");

        let past = nested("div", 513);
        let from_text = derive(
            Some(&past),
            Some("the *text* body"),
            None,
            &mut |_| None,
            &mut notes,
        );
        assert_eq!(from_text.as_deref(), Some("the \\*text\\* body"));
        let from_html = derive(Some(&past), None, None, &mut |_| None, &mut notes);
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

    /// The `src` of a start tag, quoted either way or bare, past other attributes, a name that
    /// only starts with it, and a valueless one, the first of two; none in a tag without one.
    #[test]
    fn the_src_attribute_is_read_as_the_tokenizer_reads_it() {
        fn value(tag: &str) -> Option<&str> {
            src_attribute(tag).map(|(_, value)| &tag[value])
        }
        assert_eq!(value("<img src=\"a b\">"), Some("a b"));
        assert_eq!(value("<img alt='x>y' SRC='c'>"), Some("c"));
        assert_eq!(value("<img srcset=x src = d/>"), Some("d/"));
        assert_eq!(value("<img ismap src=e src=f>"), Some("e"));
        assert_eq!(value("<img\tsrc=\"unclosed>"), Some("unclosed>"));
        assert_eq!(value("<img alt=src>"), None);
        assert_eq!(value("<img>"), None);
    }

    #[test]
    fn percent_escapes_are_decoded_and_others_kept() {
        assert_eq!(percent_decoded(b"One%2Bimg%40x%2%zz%"), b"One+img@x%2%zz%");
    }

    /// Only an `<img>`'s source is rewritten, references decoded first, and the rest of the HTML
    /// is kept byte for byte; a source given no name is kept as written.
    #[test]
    fn only_image_sources_are_rewritten() {
        let html = "<a href=\"cid:x\">a</a><img alt=1 src='cid:x&amp;y'><IMG SRC=keep>\
                    <script>'<img src=cid:x>'</script><img src=cid:x>";
        let mut seen = Vec::new();
        let out = rewritten(html, &mut |src| {
            seen.push(src.to_owned());
            (src != "keep").then(|| "f.png".to_owned())
        });
        assert_eq!(seen, ["cid:x&y", "keep", "cid:x"]);
        assert_eq!(
            out,
            "<a href=\"cid:x\">a</a><img alt=1 src=\"f.png\"><IMG SRC=keep>\
             <script>'<img src=cid:x>'</script><img src=\"f.png\">"
        );
        assert!(matches!(
            rewritten("<p>x</p>", &mut |_| None),
            Cow::Borrowed(_)
        ));
    }

    /// An attachment with a content-id, one with a Content-Location, and one with neither.
    fn message(html_ids: &[(Option<&str>, &str)]) -> Message {
        Message {
            attachments: html_ids
                .iter()
                .map(|(id, file)| crate::model::Attachment {
                    content_id: id.map(str::to_owned),
                    file: (!file.is_empty()).then(|| (*file).to_owned()),
                    inline: true,
                    ..Default::default()
                })
                .collect(),
            ..Message::default()
        }
    }

    fn made(message: &mut Message, html: &str, locations: &[Option<String>]) -> Vec<String> {
        let bodies = Bodies {
            html: Some(html.to_owned()),
            text: None,
            rtf_text: None,
        };
        let mut notes = Vec::new();
        make(
            message,
            &bodies,
            locations,
            &mut Media::new(None),
            &mut notes,
        )
        .unwrap();
        notes
    }

    /// A `cid:` matches a content-id, angle brackets and all, percent-decoded and its scheme in
    /// any case; another source a Content-Location; an attachment without a file matches nothing.
    /// Only a linked attachment is inline, and each unmatched source is noted once.
    #[test]
    fn sources_match_content_ids_and_locations() {
        let mut message = message(&[
            (Some("<a+b@x>"), "a.png"),
            (None, "b.png"),
            (Some("c@x"), ""),
            (Some("d@x"), "d.png"),
        ]);
        let locations = [None, Some("pics/b.png".to_owned()), None, None];
        let notes = made(
            &mut message,
            "<img src=\"CID:a%2Bb@x\"><img src=\" pics/b.png \"><img src=cid:c@x>\
             <img src=cid:c@x><img src=\"\">",
            &locations,
        );
        assert_eq!(
            message.markdown.as_deref(),
            Some("![](a.png)![](b.png)![](cid:c@x)![](cid:c@x)![]()")
        );
        let inline: Vec<bool> = message.attachments.iter().map(|a| a.inline).collect();
        assert_eq!(inline, [true, true, false, false]);
        assert_eq!(
            notes,
            ["an image's source \"cid:c@x\" names no attachment; kept as written"]
        );
        assert!(message.media.is_empty());
    }

    /// A `data:` image is named by its decoded payload and the media type's subtype, listed once
    /// in `media`, in the Markdown's order, unless an attachment already names that payload; one
    /// that can't be decoded is kept, with a note.
    #[test]
    fn data_images_are_media() {
        let png = Media::new(None).store(&b"PNG"[..], Some("png")).unwrap();
        let svg = Media::new(None).store(&b"<svg/>"[..], None).unwrap();
        let mut message = message(&[(None, &png)]);
        let notes = made(
            &mut message,
            "<img src=\"data:Image/SVG+xml;charset=utf-8,%3Csvg/%3E\">\
             <img src=\"data:image/png;base64,UE5H\"><img src=\"data:,%3Csvg/%3E\">\
             <img src=\"data:image/png;base64,%%%\"><img src=\"data:nocomma\">",
            &[],
        );
        assert_eq!(
            message.media,
            [Medium {
                file: svg.clone(),
                mime_type: Some("image/svg+xml".to_owned()),
            }]
        );
        assert!(
            !message.attachments[0].inline,
            "a data: image links no attachment"
        );
        let markdown = message.markdown.unwrap();
        assert!(
            markdown.starts_with(&format!("![]({svg})![]({png})![]({svg})")),
            "{markdown}"
        );
        assert_eq!(
            notes,
            ["an image's data: URL can't be decoded; kept as written"; 2]
        );
    }

    /// An HTML body past the nesting cap gives way to its text, so its images link nothing: no
    /// attachment is inline and no `data:` image is listed or written.
    #[test]
    fn html_past_the_cap_links_no_images() {
        let mut message = message(&[(Some("a@x"), "a.png")]);
        let html = format!(
            "<img src=cid:a@x><img src=\"data:,x\">{}",
            nested("div", NESTING_MAX + 1)
        );
        made(&mut message, &html, &[]);
        assert_eq!(message.markdown.as_deref(), Some("word"));
        assert!(!message.attachments[0].inline);
        assert!(message.media.is_empty());
    }
}
