//! The RTF body: PidTagRtfCompressed decompressed through compressed-rtf (MS-OXRTFCP), the HTML
//! an RTF body encapsulates recovered from it (MS-OXRTFEX 2.2.3), and the text of one that
//! encapsulates none, plain RTF or `\fromtext`, each the CLI's own code.
//!
//! One lexer and one group stack read the RTF for both, writing to one of two sinks
//! ([`Sink`]): the HTML sink keeps `\*\htmltag` groups' fragments, and the text sink skips them
//! as it skips any other ignorable group, so it keeps the document's visible text alone. Both
//! strip control words and the invisible destinations (`\*`, the font, color and style tables,
//! and the rest of [`INVISIBLE`], a nested table's `\nonesttables` fallback among them), write
//! `\par`, `\line`, `\row`, `\page` and `\sect` as line breaks, `\tab`, `\cell` and `\nestcell` as
//! tabs, so a table row reads as one line, and the symbol words (`\rquote`, `\emdash`, …) as their
//! characters, and honor `\htmlrtf`, which a plain RTF never holds. Inside a tag's fragment,
//! `\line` and the table, page and section words write nothing.
//!
//! De-encapsulation follows MS-OXRTFEX 2.2.3.2, with these choices:
//! - text and `\'hh` escapes are decoded in the header's `\ansicpg` code page, else 1252; the
//!   per-font code pages of the font table are not tracked;
//! - an `\*\mhtmltag` group is skipped (2.1.3.1.5): it holds a rewritten copy of a tag whose
//!   original, its `cid:` reference included, is in an `\*\htmltag` group (footnote 8).
//!
//! Compressed RTF's header is 16 bytes (MS-OXRTFCP 2.1.3.1.1): COMPSIZE, RAWSIZE, COMPTYPE and CRC,
//! a COMPTYPE of "MELA" naming uncompressed RTF. A run of compressed data is a control byte and
//! eight tokens, each a literal byte or a two-byte dictionary reference yielding at most 17 bytes
//! (MS-OXRTFCP 2.1.3.1.4), so 17 bytes of data yield at most 136, and one byte of data at most
//! [`RATIO_MAX`], 9, rounded up.
//!
//! The encapsulation, `\fromhtml1` or `\fromtext`, is looked for in the document's first ten tokens
//! (MS-OXRTFEX 2.2.3.1). The lexer follows RTF 1.9.1's "Conventions of an RTF reader": line ends
//! are not text, one space after a control word delimits it and is part of it, a lone `-` after one
//! is text, a malformed `\'` escape is dropped, and the bytes of `\binN` are data, never text. A
//! `\uN` is a signed 16-bit value, a surrogate pair two of them, and the `\ucN` characters after
//! it, one by default, are skipped, one per byte of text or escape, or per control word; `\ucN` and
//! `\htmlrtf` are each restored when their group ends, which also stops the skipping. A destination
//! nested in a tag's fragment is not part of it, and the header's `\ansicpgN` is read up to the
//! font table or the text.

use docproc_envelope::guarded;

use crate::codepage::{self, FALLBACK};

const HEADER: usize = 16;

const UNCOMPRESSED: u32 = 0x414C_454D;

const RATIO_MAX: u64 = 9;

/// Decompresses PidTagRtfCompressed, or says why it can't. compressed-rtf 1.0.1 slices its
/// input by the header's sizes unchecked, and allocates the header's raw size, up to 4 GiB, which
/// a failed allocation aborts on, unseen by any guard: so those sizes are checked first, a raw
/// size more than [`RATIO_MAX`] times the data after the header can yield refused, and any other
/// panic it raises is caught by the envelope's guard and becomes the reason.
pub fn decompress(compressed: &[u8]) -> Result<String, String> {
    if compressed.len() < HEADER {
        return Err(format!(
            "is {} bytes, shorter than its {HEADER}-byte header",
            compressed.len()
        ));
    }
    let word = |at: usize| {
        u32::from_le_bytes([
            compressed[at],
            compressed[at + 1],
            compressed[at + 2],
            compressed[at + 3],
        ])
    };
    let raw_size = word(4) as usize;
    if word(8) == UNCOMPRESSED && raw_size > compressed.len() - HEADER {
        return Err(format!(
            "says it holds {raw_size} bytes of uncompressed RTF, more than it has"
        ));
    }
    let data = compressed.len() - HEADER;
    if raw_size as u64 > RATIO_MAX * data as u64 {
        return Err(format!(
            "says it decompresses to {raw_size} bytes, more than its {data} bytes of data \
             can yield; not decompressed"
        ));
    }
    guarded(|| compressed_rtf::decompress_rtf(compressed))
        .map_err(|failure| format!("can't be decompressed ({})", failure.reason))
}

#[derive(Debug, PartialEq)]
enum Encapsulated {
    Html,
    Text,
    None,
}

const RECOGNIZE_TOKENS: usize = 10;

fn recognize(rtf: &[u8]) -> Encapsulated {
    if !rtf.starts_with(b"{\\rtf1") {
        return Encapsulated::None;
    }
    let mut lexer = Lexer { rtf, at: 0 };
    for _ in 0..RECOGNIZE_TOKENS {
        match lexer.next() {
            Some(Token::Open) => {}
            Some(Token::Word(b"fromhtml", Some(1))) => return Encapsulated::Html,
            Some(Token::Word(b"fromtext", _)) => return Encapsulated::Text,
            Some(Token::Word(..)) => {}
            _ => return Encapsulated::None,
        }
    }
    Encapsulated::None
}

/// The HTML an RTF document encapsulates, with notes on any text decoded lossily, or None when
/// it encapsulates no HTML (\fromtext, or plain RTF). `rtf` is as compressed-rtf gives it: each
/// char one byte of the RTF.
pub fn html(rtf: &str) -> Option<(String, Vec<String>)> {
    let bytes = bytes_of(rtf);
    (recognize(&bytes) == Encapsulated::Html).then(|| Deencapsulator::new(&bytes, Sink::Html).run())
}

/// The text of an RTF document that encapsulates no HTML, plain RTF or `\fromtext`, with notes on
/// any text decoded lossily: its visible text, read by the RTF specification, `\par`, `\line`,
/// `\row`, `\page` and `\sect` as line breaks and a table's cells as tab-separated. None for a
/// document that encapsulates HTML, whose text is that HTML's, and for bytes that are no RTF
/// document. `rtf` is as for [`html`].
pub fn text(rtf: &str) -> Option<(String, Vec<String>)> {
    let bytes = bytes_of(rtf);
    (bytes.starts_with(b"{\\rtf") && recognize(&bytes) != Encapsulated::Html)
        .then(|| Deencapsulator::new(&bytes, Sink::Text).run())
}

fn bytes_of(rtf: &str) -> Vec<u8> {
    rtf.chars()
        .map(|c| u8::try_from(u32::from(c)).unwrap_or(b'?'))
        .collect()
}

#[derive(Clone, Copy, PartialEq)]
enum Sink {
    Html,
    Text,
}

#[derive(Debug, PartialEq)]
enum Token<'a> {
    Open,
    Close,
    Word(&'a [u8], Option<i32>),
    Symbol(u8),
    Hex(u8),
    Text(u8),
    Binary,
}

struct Lexer<'a> {
    rtf: &'a [u8],
    at: usize,
}

impl<'a> Lexer<'a> {
    fn next(&mut self) -> Option<Token<'a>> {
        loop {
            let byte = *self.rtf.get(self.at)?;
            self.at += 1;
            return Some(match byte {
                b'{' => Token::Open,
                b'}' => Token::Close,
                b'\r' | b'\n' => continue,
                b'\\' => self.control(),
                _ => Token::Text(byte),
            });
        }
    }

    fn peek(&self) -> Option<u8> {
        self.rtf.get(self.at).copied()
    }

    fn control(&mut self) -> Token<'a> {
        let Some(first) = self.peek() else {
            return Token::Text(b'\\');
        };
        if !first.is_ascii_alphabetic() {
            self.at += 1;
            if first != b'\'' {
                return Token::Symbol(first);
            }
            let hex = self
                .rtf
                .get(self.at..self.at + 2)
                .and_then(|hex| std::str::from_utf8(hex).ok())
                .and_then(|hex| u8::from_str_radix(hex, 16).ok());
            return match hex {
                Some(byte) => {
                    self.at += 2;
                    Token::Hex(byte)
                }
                None => Token::Symbol(b'\''),
            };
        }
        let start = self.at;
        while self.peek().is_some_and(|byte| byte.is_ascii_alphabetic()) {
            self.at += 1;
        }
        let word = &self.rtf[start..self.at];
        let digits = self.at;
        if self.peek() == Some(b'-') {
            self.at += 1;
        }
        while self.peek().is_some_and(|byte| byte.is_ascii_digit()) {
            self.at += 1;
        }
        let parameter = std::str::from_utf8(&self.rtf[digits..self.at])
            .ok()
            .and_then(|digits| digits.parse::<i32>().ok());
        if parameter.is_none() {
            self.at = digits;
        }
        if self.peek() == Some(b' ') {
            self.at += 1;
        }
        if word == b"bin" {
            let skip = parameter.and_then(|n| usize::try_from(n).ok()).unwrap_or(0);
            self.at = self.at.saturating_add(skip).min(self.rtf.len());
            return Token::Binary;
        }
        Token::Word(word, parameter)
    }
}

#[derive(Clone, Copy, PartialEq)]
enum Destination {
    Text,
    HtmlTag,
    Skip,
}

#[derive(Clone, Copy)]
struct State {
    destination: Destination,
    suppressed: bool,
    uc: usize,
}

const INVISIBLE: &[&[u8]] = &[
    b"fonttbl",
    b"colortbl",
    b"stylesheet",
    b"info",
    b"pict",
    b"object",
    b"listtable",
    b"listoverridetable",
    b"revtbl",
    b"rsidtbl",
    b"filetbl",
    b"header",
    b"headerl",
    b"headerr",
    b"headerf",
    b"footer",
    b"footerl",
    b"footerr",
    b"footerf",
    b"fldinst",
    b"bkmkstart",
    b"bkmkend",
    b"xe",
    b"tc",
    b"nonesttables",
];

struct Deencapsulator<'a> {
    lexer: Lexer<'a>,
    sink: Sink,
    state: State,
    stack: Vec<State>,
    group_start: bool,
    starred: bool,
    skip: usize,
    out: Out,
}

struct Out {
    html: String,
    bytes: Vec<u8>,
    units: Vec<u16>,
    code_page: u16,
    what: &'static str,
    notes: Vec<String>,
}

impl Out {
    fn byte(&mut self, byte: u8) {
        self.flush_units();
        self.bytes.push(byte);
    }

    fn unit(&mut self, unit: u16) {
        self.flush_bytes();
        self.units.push(unit);
    }

    fn text(&mut self, text: &str) {
        self.flush();
        self.html.push_str(text);
    }

    fn flush(&mut self) {
        self.flush_bytes();
        self.flush_units();
    }

    fn flush_bytes(&mut self) {
        if self.bytes.is_empty() {
            return;
        }
        let (text, lossy) = codepage::decode(&self.bytes, self.code_page);
        if let Some(lossy) = lossy {
            let note = format!("the RTF body's {} {lossy}", self.what);
            if !self.notes.contains(&note) {
                self.notes.push(note);
            }
        }
        self.html.push_str(&text);
        self.bytes.clear();
    }

    fn flush_units(&mut self) {
        if !self.units.is_empty() {
            self.html.push_str(&String::from_utf16_lossy(&self.units));
            self.units.clear();
        }
    }
}

impl<'a> Deencapsulator<'a> {
    fn new(rtf: &'a [u8], sink: Sink) -> Self {
        let code_page = ansicpg(rtf).unwrap_or(FALLBACK);
        Self {
            lexer: Lexer { rtf, at: 0 },
            sink,
            state: State {
                destination: Destination::Text,
                suppressed: false,
                uc: 1,
            },
            stack: Vec::new(),
            group_start: false,
            starred: false,
            skip: 0,
            out: Out {
                html: String::new(),
                bytes: Vec::new(),
                units: Vec::new(),
                code_page,
                what: match sink {
                    Sink::Html => "encapsulated HTML",
                    Sink::Text => "text",
                },
                notes: Vec::new(),
            },
        }
    }

    fn run(mut self) -> (String, Vec<String>) {
        while let Some(token) = self.lexer.next() {
            let first = std::mem::take(&mut self.group_start);
            match token {
                Token::Open => {
                    self.stack.push(self.state);
                    self.group_start = true;
                    self.starred = false;
                    self.skip = 0;
                }
                Token::Close => {
                    if let Some(state) = self.stack.pop() {
                        self.state = state;
                    }
                    self.skip = 0;
                }
                Token::Symbol(b'*') if first => {
                    self.starred = true;
                    self.group_start = true;
                }
                Token::Word(word, _) if first && self.opens(word) => {}
                token => self.content(token),
            }
        }
        self.out.flush();
        (self.out.html, self.out.notes)
    }

    fn opens(&mut self, word: &[u8]) -> bool {
        let inside_tag = self.state.destination == Destination::HtmlTag;
        let destination = match word {
            b"htmltag" if self.starred && self.sink == Sink::Html => Destination::HtmlTag,
            _ if self.starred => Destination::Skip,
            _ if INVISIBLE.contains(&word) => Destination::Skip,
            _ => return false,
        };
        if self.state.destination != Destination::Skip && !inside_tag {
            self.state.destination = destination;
        } else if inside_tag && destination != Destination::HtmlTag {
            self.state.destination = Destination::Skip;
        }
        true
    }

    fn content(&mut self, token: Token) {
        let destination = self.state.destination;
        if destination == Destination::Skip {
            return;
        }
        let in_tag = destination != Destination::Text;
        if let Token::Word(b"htmlrtf", parameter) = token {
            if !in_tag {
                self.state.suppressed = parameter != Some(0);
            }
            return;
        }
        if let Token::Word(b"uc", Some(n)) = token {
            self.state.uc = usize::try_from(n).unwrap_or(0);
            return;
        }
        if !in_tag && self.state.suppressed {
            return;
        }
        if self.skip > 0 && matches!(token, Token::Text(_) | Token::Hex(_) | Token::Word(..)) {
            self.skip -= 1;
            return;
        }
        match token {
            Token::Text(byte) | Token::Hex(byte) => self.out.byte(byte),
            Token::Symbol(symbol @ (b'{' | b'}' | b'\\')) => self.out.byte(symbol),
            Token::Symbol(b'~') => self.out.text("\u{A0}"),
            Token::Symbol(b'_') => self.out.text("\u{AD}"),
            Token::Word(b"u", Some(n)) => {
                self.out.unit(n as u16);
                self.skip = self.state.uc;
            }
            Token::Word(word, _) => match word {
                b"par" => self.out.text("\r\n"),
                b"line" | b"row" | b"page" | b"sect" if !in_tag => self.out.text("\r\n"),
                b"tab" => self.out.text("\t"),
                b"cell" | b"nestcell" if !in_tag => self.out.text("\t"),
                b"lquote" => self.out.text("\u{2018}"),
                b"rquote" => self.out.text("\u{2019}"),
                b"ldblquote" => self.out.text("\u{201C}"),
                b"rdblquote" => self.out.text("\u{201D}"),
                b"bullet" => self.out.text("\u{2022}"),
                b"endash" => self.out.text("\u{2013}"),
                b"emdash" => self.out.text("\u{2014}"),
                _ => {}
            },
            _ => {}
        }
    }
}

fn ansicpg(rtf: &[u8]) -> Option<u16> {
    let mut lexer = Lexer { rtf, at: 0 };
    while let Some(token) = lexer.next() {
        match token {
            Token::Word(b"ansicpg", Some(n)) => return u16::try_from(n).ok(),
            Token::Word(b"fonttbl", _) | Token::Text(_) => return None,
            _ => {}
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    fn recover(rtf: &[u8]) -> Option<(String, Vec<String>)> {
        html(&rtf.iter().map(|&byte| char::from(byte)).collect::<String>())
    }

    fn recovered(rtf: &[u8]) -> String {
        let (html, notes) = recover(rtf).expect("encapsulated HTML");
        assert_eq!(notes, Vec::<String>::new());
        html
    }

    fn document(code_page: u16, body: &str) -> Vec<u8> {
        format!(
            "{{\\rtf1\\ansi\\ansicpg{code_page}\\fromhtml1 \\deff0{{\\fonttbl\r\n\
             {{\\f0\\fswiss Arial;}}{{\\f1\\fmodern Courier New;}}}}\r\n\
             {{\\colortbl\\red0\\green0\\blue0;}}\r\n\
             {{\\*\\generator Microsoft Exchange Server;}}\r\n{body}}}"
        )
        .into_bytes()
    }

    #[test]
    fn only_fromhtml1_encapsulates_html() {
        assert_eq!(
            recover(b"{\\rtf1\\ansi\\fromtext \\deff0 Hello\\par }"),
            None
        );
        assert_eq!(recover(b"{\\rtf1\\ansi\\deff0 Hello\\par }"), None);
        assert_eq!(recover(b"{\\rtf1\\ansi\\fromhtml0 Hello}"), None);
        assert_eq!(recover(b"<html>{\\rtf1\\fromhtml1 }"), None);
        let late = b"{\\rtf1\\a\\b\\c\\d\\e\\f\\g\\h\\i\\j\\fromhtml1 x}";
        assert_eq!(recover(late), None);
        assert!(recover(b"{\\rtf1\\ansi\\fromhtml1 x}").is_some());
    }

    #[test]
    fn tags_are_copied_and_htmlrtf_text_is_not() {
        let rtf = document(
            1252,
            "{\\*\\htmltag19 <html>}{\\*\\htmltag34 <head>}{\\*\\htmltag41 </head>}\r\n\
             {\\*\\htmltag50 <body>}\\htmlrtf {\\f0\\fs20 \\htmlrtf0 \
             {\\*\\htmltag64 <p class=\"x\">}\\htmlrtf {\\htmlrtf0 Hello, {\\*\\htmltag84 <b>}\
             \\htmlrtf\\b \\htmlrtf0 world{\\*\\htmltag92 </b>}}\\htmlrtf\\par\\htmlrtf0\
             {\\*\\htmltag72 </p>\\par}}\
             {\\*\\htmltag58 </body>}{\\*\\htmltag27 </html>}",
        );
        assert_eq!(
            recovered(&rtf),
            "<html><head></head><body><p class=\"x\">Hello, <b>world</b></p>\r\n</body></html>"
        );
    }

    #[test]
    fn htmlrtf_state_is_restored_when_a_group_ends() {
        let rtf = document(
            1252,
            "a{\\htmlrtf b}c\\htmlrtf d{\\htmlrtf0 e{f}}g\\htmlrtf0 h{{{\\htmlrtf i}j}k}",
        );
        assert_eq!(recovered(&rtf), "acefhjk");
    }

    #[test]
    fn nested_groups_inside_a_tag_are_part_of_it() {
        let rtf = document(
            1252,
            "{\\*\\htmltag <a href=\"x\" {title}=\"{\\b y}\">}{\\*\\htmltag {\\*\\other no}</a>}",
        );
        assert_eq!(recovered(&rtf), "<a href=\"x\" title=\"y\"></a>");
    }

    #[test]
    fn par_tab_and_escapes_in_and_out_of_tags() {
        let rtf = document(
            1252,
            "{\\*\\htmltag <style>\\par p\\{x:\\\\y\\}\\tab\\par </style>}\
             a\\{b\\}c\\\\d\\tab e\\line f\\~g\\lquote h\\rquote\\emdash\\bullet",
        );
        assert_eq!(
            recovered(&rtf),
            "<style>\r\np{x:\\y}\t\r\n</style>a{b}c\\d\te\r\nf\u{A0}g\u{2018}h\u{2019}\u{2014}\u{2022}"
        );
    }

    #[test]
    fn hex_escapes_are_decoded_in_the_ansicpg() {
        let rtf = document(
            1251,
            "{\\*\\htmltag <p title=\"\\'cf\">}\\'cf\\'f0\\'e8\\'e2\\'e5\\'f2",
        );
        assert_eq!(recovered(&rtf), "<p title=\"П\">Привет");
        let rtf = document(936, "\\'b4\\'b2\\'c7\\'b0");
        assert_eq!(recovered(&rtf), "床前");
        let rtf = b"{\\rtf1\\ansi\\fromhtml1 caf\\'e9}";
        assert_eq!(recovered(rtf), "café");
    }

    #[test]
    fn bytes_not_valid_in_the_code_page_are_decoded_lossily_with_a_note() {
        let (html, notes) = recover(&document(65001, "a\\'ffb")).unwrap();
        assert_eq!(html, "a\u{FFFD}b");
        assert_eq!(
            notes,
            ["the RTF body's encapsulated HTML is not valid in code page 65001; decoded lossily"]
        );
    }

    #[test]
    fn unicode_escapes_skip_their_uc_fallbacks() {
        let rtf = document(1252, "\\u1055\\'3f\\u1088?x");
        assert_eq!(recovered(&rtf), "Прx");
        let rtf = document(1252, "\\uc2\\u8364??a\\uc0\\u-3913b");
        assert_eq!(recovered(&rtf), "€a\u{F0B7}b");
        let rtf = document(1252, "\\u-10179?\\u-8701?");
        assert_eq!(recovered(&rtf), "😃");
        let rtf = document(1252, "{\\uc3 \\u1055???}\\u1088?{\\u1080}x");
        assert_eq!(recovered(&rtf), "Приx");
        let rtf = document(1252, "{\\*\\htmltag <p title=\"\\u1055?\">}");
        assert_eq!(recovered(&rtf), "<p title=\"П\">");
    }

    #[test]
    fn mhtmltag_is_skipped_and_its_htmltag_kept() {
        let rtf = document(
            1252,
            "{\\*\\mhtmltag84 <img src=\"\\htmlbase http://x/moth.png\">}\
             {\\*\\htmltag84 <img src=\"cid:moth@example.net\">}",
        );
        assert_eq!(recovered(&rtf), "<img src=\"cid:moth@example.net\">");
    }

    #[test]
    fn invisible_destinations_and_binary_data_are_skipped() {
        let rtf = document(
            1252,
            "a{\\info{\\title T}}{\\pict\\pngblip 89504e47}{\\*\\bkmkstart m}b\\bin3 {}}c",
        );
        assert_eq!(recovered(&rtf), "abc");
    }

    #[test]
    fn decompression_round_trips_and_refuses_what_it_cant_read() {
        let rtf = "{\\rtf1\\ansi\\fromhtml1 {\\*\\htmltag <p>}Hi}";
        let compressed = compressed_rtf::compress_rtf(rtf).unwrap();
        assert_eq!(decompress(&compressed).as_deref(), Ok(rtf));

        assert_eq!(
            decompress(&compressed[..10]),
            Err("is 10 bytes, shorter than its 16-byte header".to_owned())
        );
        let mut bad_crc = compressed.clone();
        *bad_crc.last_mut().unwrap() ^= 0xFF;
        assert!(
            decompress(&bad_crc)
                .unwrap_err()
                .starts_with("can't be decompressed (")
        );

        let mut mela = Vec::new();
        for word in [12 + 4, 4, UNCOMPRESSED, 0] {
            mela.extend_from_slice(&u32::to_le_bytes(word));
        }
        mela.extend_from_slice(b"{\\x}");
        assert_eq!(decompress(&mela).as_deref(), Ok("{\\x}"));
        mela[4] = 200;
        assert_eq!(
            decompress(&mela),
            Err("says it holds 200 bytes of uncompressed RTF, more than it has".to_owned())
        );
    }

    #[test]
    fn a_raw_size_past_what_the_data_can_yield_is_refused() {
        let compressed = compressed_rtf::compress_rtf("{\\rtf1 Hi}").unwrap();
        let data = compressed.len() - HEADER;
        let with_raw_size = |raw_size: u32| {
            let mut stated = compressed.clone();
            stated[4..8].copy_from_slice(&raw_size.to_le_bytes());
            decompress(&stated)
        };
        let at_most = u32::try_from(9 * data).unwrap();
        assert!(
            !matches!(with_raw_size(at_most), Err(reason) if reason.ends_with("not decompressed"))
        );
        assert_eq!(
            with_raw_size(at_most + 1),
            Err(format!(
                "says it decompresses to {} bytes, more than its {data} bytes of data can \
                 yield; not decompressed",
                at_most + 1
            ))
        );
        assert!(
            with_raw_size(u32::MAX)
                .unwrap_err()
                .ends_with("not decompressed")
        );
    }

    fn read_text(rtf: &[u8]) -> Option<(String, Vec<String>)> {
        text(&rtf.iter().map(|&byte| char::from(byte)).collect::<String>())
    }

    #[test]
    fn plain_rtf_is_read_as_its_text() {
        let rtf = b"{\\rtf1\\ansi\\ansicpg1251\\deff0{\\fonttbl{\\f0\\fswiss Arial;}}\r\n\
                    {\\colortbl;\\red255\\green0\\blue0;}{\\*\\generator Riched20 10.0;}\
                    {\\stylesheet{\\s1 Heading;}}\\viewkind4\\uc1\\pard\\cf1\\b\\f0\\fs20 \
                    \\'cf\\'f0\\'e8\\'e2\\'e5\\'f2\\b0 , {\\i world}\\par\r\n\
                    {\\pict\\pngblip 89504e47}a\\line b\\tab c\\~d\\lquote e\\rquote\\endash\\bullet \
                    \\u8364?{\\*\\bkmkstart x}\\{\\}\\\\\\par}";
        assert_eq!(
            read_text(rtf),
            Some((
                "Привет, world\r\na\r\nb\tc\u{A0}d\u{2018}e\u{2019}\u{2013}\u{2022}€{}\\\r\n"
                    .to_owned(),
                Vec::new()
            ))
        );
    }

    #[test]
    fn fromtext_is_read_as_text_and_fromhtml1_is_not() {
        let rtf = b"{\\rtf1\\ansi\\fromtext \\deff0{\\*\\htmltag64 <p>}Hi\\htmlrtf  no\\htmlrtf0 \
                    {\\*\\htmltag72 </p>}\\par there}";
        assert_eq!(read_text(rtf), Some(("Hi\r\nthere".to_owned(), Vec::new())));
        assert_eq!(
            read_text(b"{\\rtf1\\ansi\\fromhtml1 {\\*\\htmltag <p>}x}"),
            None
        );
        assert_eq!(read_text(b"Hello"), None);
    }

    #[test]
    fn a_tables_cells_are_tabs_and_its_rows_line_breaks() {
        let rtf = b"{\\rtf1\\ansi\\deff0 Before\\par\\trowd\\cellx1000\\cellx2000\\pard\\intbl \
                    Name\\cell Count\\cell\\row\\trowd\\cellx1000\\cellx2000\\pard\\intbl \
                    Moths\\cell 3\\cell\\row\\pard After\\par}";
        assert_eq!(
            read_text(rtf),
            Some((
                "Before\r\nName\tCount\t\r\nMoths\t3\t\r\nAfter\r\n".to_owned(),
                Vec::new()
            ))
        );
    }

    #[test]
    fn a_nested_tables_text_is_read_once() {
        let rtf = b"{\\rtf1\\ansi\\deff0 \\trowd\\cellx4000\\pard\\intbl Outer\\par\
                    \\pard\\intbl\\itap2 a\\nestcell b\\nestcell{\\*\\nesttableprops\\trowd\\cellx1000\
                    \\cellx2000\\nestrow}{\\nonesttables\\par a\\tab b\\par}\\pard\\intbl\\itap1 \\cell\
                    \\row\\pard End}";
        assert_eq!(
            read_text(rtf),
            Some(("Outer\r\na\tb\t\t\r\nEnd".to_owned(), Vec::new()))
        );
    }

    #[test]
    fn page_and_section_breaks_are_line_breaks() {
        let rtf = b"{\\rtf1\\ansi\\deff0 one\\page two\\sect\\sectd three}";
        assert_eq!(
            read_text(rtf),
            Some(("one\r\ntwo\r\nthree".to_owned(), Vec::new()))
        );
    }

    #[test]
    fn table_and_page_words_are_read_outside_tags_alone_in_encapsulated_html() {
        let rtf = document(
            1252,
            "{\\*\\htmltag <td title=\"a\\cell b\\row c\\page d\\sect e\\nestcell\">}x\\cell y\\row \
             p\\page q\\sect r\\nestcell{\\nonesttables s}",
        );
        assert_eq!(recovered(&rtf), "<td title=\"abcde\">x\ty\r\np\r\nq\r\nr\t");
    }

    #[test]
    fn text_not_valid_in_the_code_page_is_decoded_lossily_with_a_note() {
        let (text, notes) = read_text(b"{\\rtf1\\ansi\\ansicpg65001 a\\'ffb}").unwrap();
        assert_eq!(text, "a\u{FFFD}b");
        assert_eq!(
            notes,
            ["the RTF body's text is not valid in code page 65001; decoded lossily"]
        );
    }
}
