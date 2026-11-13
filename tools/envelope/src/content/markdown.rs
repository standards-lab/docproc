//! The content rule's Markdown-to-plain reader: a Markdown text's plain text by CommonMark's
//! semantics, read by pulldown-cmark, behind the `markdown` feature.
//!
//! A link is its text followed by its destination, and an image its alt text followed by its
//! destination, each destination one URL token; emphasis, code spans' backticks, and backslash
//! escapes are gone, and entity references decoded, as pulldown-cmark reads them. Each block, a
//! table cell included, ends a line, as a line break does. Raw HTML is read as it renders: a tag
//! is no text, the text between tags in an HTML block is. GFM tables and strikethrough are read,
//! as htmd writes them.

use pulldown_cmark::{Event, Options, Parser, Tag, TagEnd};

use super::{Text, nfc};

/// `markdown`'s plain text, each text run normalized to NFC, with its destinations' ranges.
pub fn plain(markdown: &str) -> Text {
    let mut out = Text::default();
    let mut destinations: Vec<String> = Vec::new();
    let options = Options::ENABLE_TABLES | Options::ENABLE_STRIKETHROUGH;
    for event in Parser::new_ext(markdown, options) {
        match event {
            Event::Start(Tag::Link { dest_url, .. } | Tag::Image { dest_url, .. }) => {
                destinations.push(dest_url.into_string());
            }
            Event::End(TagEnd::Link | TagEnd::Image) => {
                let destination = destinations.pop().unwrap_or_default();
                if !destination.trim().is_empty() {
                    out.text.push(' ');
                    let start = out.text.len();
                    out.text.push_str(&destination);
                    out.urls.push(start..out.text.len());
                    out.text.push(' ');
                }
            }
            Event::Text(text)
            | Event::Code(text)
            | Event::InlineMath(text)
            | Event::DisplayMath(text) => out.text.push_str(&nfc(&text)),
            Event::Html(html) => out.text.push_str(&nfc(&untagged(&html))),
            Event::SoftBreak | Event::HardBreak | Event::Rule => out.text.push('\n'),
            Event::End(
                TagEnd::Paragraph
                | TagEnd::Heading(_)
                | TagEnd::CodeBlock
                | TagEnd::HtmlBlock
                | TagEnd::Item
                | TagEnd::TableCell
                | TagEnd::TableRow
                | TagEnd::TableHead
                | TagEnd::BlockQuote(_)
                | TagEnd::DefinitionListTitle
                | TagEnd::DefinitionListDefinition
                | TagEnd::FootnoteDefinition,
            ) => out.text.push('\n'),
            _ => {}
        }
    }
    out
}

/// An HTML block's line with its tags and comments removed.
fn untagged(html: &str) -> String {
    let mut out = String::with_capacity(html.len());
    let mut rest = html;
    while let Some(open) = rest.find('<') {
        out.push_str(&rest[..open]);
        match rest[open..].find('>') {
            Some(close) => rest = &rest[open + close + 1..],
            None => {
                rest = "";
            }
        }
    }
    out.push_str(rest);
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn read(markdown: &str) -> (String, Vec<String>) {
        let text = plain(markdown);
        let urls = text
            .urls
            .iter()
            .map(|r| text.text[r.clone()].to_owned())
            .collect();
        (text.text, urls)
    }

    /// Links and images are their text and their destination; emphasis and escapes go.
    #[test]
    fn markdown_reads_as_its_plain_text() {
        let (text, urls) = read(
            "A **bold** \\*star\\* [link](http://x.example/a) and ![alt](f.png \"t\")\\\nnext &amp; `code`",
        );
        assert_eq!(
            text,
            "A bold *star* link http://x.example/a  and alt f.png \nnext & code\n"
        );
        assert_eq!(urls, ["http://x.example/a", "f.png"]);
    }

    /// Blocks end lines; a tag in an HTML block is no text, the text between tags is.
    #[test]
    fn blocks_end_lines_and_tags_are_no_text() {
        let (text, urls) = read(
            "# Head\n\n- one\n- two\n\n| a | b |\n|---|---|\n| c | d |\n\n<div>in <b>html</b></div>\n\nx<br>y",
        );
        assert_eq!(text, "Head\none\ntwo\na\nb\n\nc\nd\n\nin html\n\nxy\n");
        assert!(urls.is_empty());
    }

    /// An autolink is its text and its destination, the two compared as one URL each.
    #[test]
    fn autolinks_are_their_text_and_destination() {
        let (_, urls) = read("<http://x.example/>");
        assert_eq!(urls, ["http://x.example/"]);
    }
}
