//! A page's Markdown, rendered from the crate's reading of it alone (src/page.rs), so the same
//! page always gives the same text.
//!
//! The title comes first, its date and time paragraphs on one line after it, then the page's
//! containers top to bottom, then left to right. A paragraph is a block: an hN heading one level
//! below the title's `#`, capped at 6; a code paragraph fenced, with the code paragraphs beside
//! it in one fence; a quote or citation quoted, with the ones beside it in one quote. Bold,
//! italic, strikethrough and links are marked; underline, highlight, colour, superscript and
//! subscript are plain text. A list marker makes its element a list item, its children nested
//! under it and its numbers counted from its restart; a To Do tag makes one a task item. Every
//! tag's label is a `[Label]` prefix, a To Do's after its box. A table is GFM's, its first row
//! the header.
//!
//! An image's alt text is followed by the sentences of its OCR text that the alt text lacks, by
//! the shared content rule over plain text, and a hyperlinked image is a link. An ink drawing's
//! alt text is the recognised words its own strokes belong to, in the recognition's order; a
//! word no drawing carries is on one plain line after the page's content, so every recognised
//! word is in the Markdown.
//!
//! Literal text is escaped by the shared envelope's literal-text escape, the Rust tools' one,
//! which escapes `$` as this Markdown writes math between `$`s, and destinations are written by
//! its destination writer. In a table cell, a `|` that isn't literal text, in a code span, an
//! equation's LaTeX or a destination, is written `\|`, since GFM splits a row at every
//! unescaped `|` before it reads a cell's spans.

use std::cell::RefCell;
use std::collections::HashSet;

use docproc_envelope::content::{self, Text};
use docproc_envelope::markdown::{destination, escape};

use crate::ink::UNITS_PER_HALF_INCH;
use crate::page::{
    Block, Cell, Container, EmbeddedFile, Image, Ink, Item, List, Page, Paragraph, Run, Table, Tag,
    Word,
};

/// A page's Markdown; empty when nothing on the page renders.
pub fn page(page: &Page) -> String {
    let words = Words::new(&page.words);
    let mut containers: Vec<&Container> = page.content.iter().collect();
    // Stable, so containers at one place keep their stored order.
    containers.sort_by(|a, b| {
        let (a, b) = (place(a), place(b));
        a.0.cmp(&b.0)
            .then(a.1.total_cmp(&b.1))
            .then(a.2.total_cmp(&b.2))
    });
    let mut chunks = Vec::new();
    let mut page_ink = page.ink.as_ref();
    for container in containers {
        chunks.push(Chunk::Break);
        match container {
            Container::Title { items, .. } => {
                items
                    .iter()
                    .for_each(|item| item_chunks(item, true, &words, &mut chunks));
                let line = date_time(items);
                if !line.is_empty() {
                    chunks.push(Chunk::Text(vec![line]));
                }
            }
            Container::Outline { items, .. } => {
                items
                    .iter()
                    .for_each(|item| item_chunks(item, false, &words, &mut chunks));
            }
            Container::Image { image, .. } => {
                chunks.push(Chunk::Text(vec![image_md(image, false)]));
            }
            Container::EmbeddedFile { file, .. } => {
                chunks.push(Chunk::Text(vec![file_md(file, false)]));
            }
            // The page's ink is one drawing of all its ink containers, so it goes once, where
            // the first container that holds strokes is.
            Container::Ink { bounds, .. } => {
                if bounds.is_some()
                    && let Some(ink) = page_ink.take()
                {
                    chunks.push(Chunk::Text(vec![ink_md(ink, &words, false)]));
                }
            }
        }
    }
    if let Some(ink) = page_ink {
        chunks.push(Chunk::Break);
        chunks.push(Chunk::Text(vec![ink_md(ink, &words, false)]));
    }
    let stray = words.stray();
    if !stray.is_empty() {
        chunks.push(Chunk::Break);
        chunks.push(Chunk::Text(vec![escape(&collapse(&stray), true)]));
    }
    join(chunks, false).join("\n")
}

/// A container's place: the title before all else, then its vertical offset, then its
/// horizontal one, a missing offset as the page's origin. An ink container's offsets don't
/// place its strokes, which hold their own coordinates on the page, so its place is its
/// strokes' top left corner, in half-inch units as offsets are.
fn place(container: &Container) -> (u8, f32, f32) {
    let offsets = match container {
        Container::Title { .. } => return (0, 0.0, 0.0),
        Container::Ink {
            bounds: Some(bounds),
            ..
        } => {
            return (
                1,
                bounds.y / UNITS_PER_HALF_INCH,
                bounds.x / UNITS_PER_HALF_INCH,
            );
        }
        Container::Outline { offsets, .. }
        | Container::Image { offsets, .. }
        | Container::EmbeddedFile { offsets, .. }
        | Container::Ink { offsets, .. } => offsets,
    };
    (
        1,
        offsets.offset_vertical.unwrap_or(0.0),
        offsets.offset_horizontal.unwrap_or(0.0),
    )
}

/// A page's recognised ink words as its drawings take them: each drawing the words its strokes
/// belong to, and the words no drawing rendered so far has taken.
struct Words<'a> {
    words: &'a [Word],
    /// The ids of the words a rendered drawing has taken.
    carried: RefCell<HashSet<u32>>,
}

impl<'a> Words<'a> {
    fn new(words: &'a [Word]) -> Self {
        Self {
            words,
            carried: RefCell::new(HashSet::new()),
        }
    }

    /// A drawing's words, in the recognition's order, joined by spaces; each is carried once
    /// the drawing is rendered.
    fn of(&self, ink: &Ink) -> String {
        self.carried.borrow_mut().extend(&ink.words);
        self.words
            .iter()
            .filter(|word| ink.words.contains(&word.id))
            .map(|word| word.text.as_str())
            .collect::<Vec<_>>()
            .join(" ")
    }

    /// The words no rendered drawing carries, in the recognition's order, joined by spaces.
    fn stray(&self) -> String {
        let carried = self.carried.borrow();
        self.words
            .iter()
            .filter(|word| !carried.contains(&word.id))
            .map(|word| word.text.as_str())
            .collect::<Vec<_>>()
            .join(" ")
    }
}

/// A paragraph's style id, lowercased; a paragraph without one is `p`.
fn style(p: &Paragraph) -> String {
    p.style.as_deref().unwrap_or("p").to_ascii_lowercase()
}

/// The title area's date and time paragraphs, OneNote's PageDateTime, as one line: each
/// paragraph's text, joined by one space. Empty when there are none.
fn date_time(items: &[Item]) -> String {
    let mut line = String::new();
    date_time_items(items, &mut line);
    line
}

fn date_time_items(items: &[Item], line: &mut String) {
    for item in items {
        match item {
            Item::Group { items } => date_time_items(items, line),
            Item::Element { blocks, children } => {
                for block in blocks {
                    if let Block::Paragraph(p) = block
                        && style(p) == "pagedatetime"
                    {
                        let text = inline(&p.runs, line.is_empty(), false).join(" ");
                        if !text.is_empty() {
                            if !line.is_empty() {
                                line.push(' ');
                            }
                            line.push_str(&text);
                        }
                    }
                }
                date_time_items(children, line);
            }
        }
    }
}

/// A rendered block, before the blocks around it are joined to it.
enum Chunk {
    /// Where a container starts: lists on either side of it are separate lists.
    Break,
    /// A block's lines.
    Text(Vec<String>),
    /// A code paragraph's lines, unfenced: code paragraphs side by side share one fence.
    Code(Vec<String>),
    /// A quote or citation's lines, without their `>`: those side by side share one quote.
    Quote(Vec<String>),
    Item(ListItem),
}

/// A list item: its marker, numbered when it is joined to its list, and its content's lines.
struct ListItem {
    /// None for a bullet; for a number, the number its list restarts at, if it does.
    number: Option<Option<i32>>,
    /// For a task item, whether it is done.
    task: Option<bool>,
    body: Vec<String>,
}

/// An outline item's chunks. An element with a list marker or a To Do tag on its first
/// paragraph is one list item holding its blocks and, nested, its children; any other
/// element's blocks and children are blocks at its own level, since Markdown can indent only
/// under a list item. `title` says the item is in the page's title area.
fn item_chunks(item: &Item, title: bool, words: &Words, out: &mut Vec<Chunk>) {
    match item {
        Item::Group { items } => items
            .iter()
            .for_each(|item| item_chunks(item, title, words, out)),
        Item::Element { blocks, children } => {
            let marker = match blocks.first() {
                Some(Block::Paragraph(p)) if p.list.is_some() || task(&p.tags).is_some() => {
                    Some((p.list.as_ref().and_then(number), task(&p.tags)))
                }
                _ => None,
            };
            let mut inner = Vec::new();
            for (index, block) in blocks.iter().enumerate() {
                let marked = marker.is_some() && index == 0;
                block_chunks(block, title, marked, words, &mut inner);
            }
            for child in children {
                item_chunks(child, title, words, &mut inner);
            }
            match marker {
                Some((number, task)) => {
                    let body = join(inner, true);
                    if !body.is_empty() || task.is_some() {
                        out.push(Chunk::Item(ListItem { number, task, body }));
                    }
                }
                None => out.extend(inner),
            }
        }
    }
}

/// A list marker's number, the number its list restarts at, if it is a numbered one.
fn number(list: &List) -> Option<Option<i32>> {
    list.numbered.then_some(list.restart)
}

/// The first To Do tag's state: whether it is done.
fn task(tags: &[Tag]) -> Option<bool> {
    tags.iter().find(|tag| tag.task).map(|tag| tag.completed)
}

/// A block's chunk. `marked` says its element is a list item that this block leads, so its
/// first To Do tag is the item's task box, not a prefix.
fn block_chunks(block: &Block, title: bool, marked: bool, words: &Words, out: &mut Vec<Chunk>) {
    let chunk = match block {
        Block::Paragraph(p) => paragraph(p, title, marked, words),
        Block::Table(table) => table_md(table, words),
        Block::Image(image) => Some(Chunk::Text(vec![image_md(image, false)])),
        Block::EmbeddedFile(file) => Some(Chunk::Text(vec![file_md(file, false)])),
        Block::Ink { ink } => ink
            .as_ref()
            .map(|ink| Chunk::Text(vec![ink_md(ink, words, false)])),
    };
    out.extend(chunk);
}

/// A paragraph's chunk; none for an empty paragraph, which OneNote writes for a blank line, or
/// for the title area's date and time, which its own line holds (`date_time`). The page title
/// is the `#` heading in the title area, `title`; a paragraph of its style elsewhere is text,
/// so the title is rendered once, and so is a date and time paragraph. A code paragraph holding
/// handwriting is text, so its drawing is rendered.
fn paragraph(p: &Paragraph, title: bool, marked: bool, words: &Words) -> Option<Chunk> {
    let style = style(p);
    if title && style == "pagedatetime" {
        return None;
    }
    let prefix = tag_prefix(&p.tags, marked);
    if style == "code" && p.ink.is_none() {
        let mut lines = code_lines(&p.runs);
        if let Some(first) = lines.first_mut() {
            first.insert_str(0, &prefix);
        }
        return (!lines.is_empty()).then_some(Chunk::Code(lines));
    }
    let mut lines = inline(&p.runs, true, false);
    if let Some(ink) = &p.ink {
        lines.push(ink_md(ink, words, false));
    }
    if lines.is_empty() {
        return None;
    }
    lines[0].insert_str(0, &prefix);
    let heading = match style.as_str() {
        "pagetitle" if title => Some(1),
        "pagetitle" => None,
        _ => style
            .strip_prefix('h')
            .and_then(|n| n.parse::<usize>().ok())
            .filter(|n| (1..=6).contains(n))
            .map(|n| (n + 1).min(6)),
    };
    if let Some(level) = heading {
        return Some(Chunk::Text(vec![format!(
            "{} {}",
            "#".repeat(level),
            lines.join(" ")
        )]));
    }
    if matches!(style.as_str(), "quote" | "blockquote" | "citation" | "cite") {
        return Some(Chunk::Quote(hard_breaks(lines)));
    }
    Some(Chunk::Text(hard_breaks(lines)))
}

/// A paragraph's tags as the prefix of its text: `[ ]` or `[x]` for a To Do, unless `marked`
/// makes the first one its list item's task box, then every tag's `[Label]`, a To Do's after
/// its box: the box carries the task's state, not the label's words.
fn tag_prefix(tags: &[Tag], marked: bool) -> String {
    let mut prefix = String::new();
    let mut boxed = marked;
    for tag in tags {
        if tag.task {
            if boxed {
                boxed = false;
            } else {
                prefix.push_str(if tag.completed { "[x] " } else { "[ ] " });
            }
        }
        if let Some(label) = &tag.label {
            prefix.push('[');
            prefix.push_str(&escape(label, false));
            prefix.push_str("] ");
        }
    }
    prefix
}

/// A paragraph's lines in flow, each but the last ending in a hard break.
fn hard_breaks(mut lines: Vec<String>) -> Vec<String> {
    let last = lines.len().saturating_sub(1);
    for line in &mut lines[..last] {
        line.push_str("  ");
    }
    lines
}

/// A code paragraph's lines, as stored: unescaped and unmarked, an equation as its LaTeX.
fn code_lines(runs: &[Run]) -> Vec<String> {
    let mut text = String::new();
    for run in runs {
        match &run.latex {
            Some(latex) => text.push_str(latex),
            None => text.push_str(&run.text),
        }
    }
    let text = text.trim_end_matches(is_break);
    if text.is_empty() {
        return Vec::new();
    }
    text.replace("\r\n", "\n")
        .split(is_break)
        .map(|line| line.trim_end().to_owned())
        .collect()
}

/// A line break in stored text: OneNote's vertical tab, or a newline.
fn is_break(c: char) -> bool {
    matches!(c, '\n' | '\r' | '\u{b}')
}

/// Bold, italic and strikethrough, in the order they open.
#[derive(Clone, Copy, PartialEq)]
enum Mark {
    Strike,
    Bold,
    Italic,
}

impl Mark {
    fn delimiter(self) -> &'static str {
        match self {
            Mark::Strike => "~~",
            Mark::Bold => "**",
            Mark::Italic => "*",
        }
    }
}

fn marks(run: &Run) -> Vec<Mark> {
    [
        (run.strikethrough, Mark::Strike),
        (run.bold, Mark::Bold),
        (run.italic, Mark::Italic),
    ]
    .into_iter()
    .filter_map(|(set, mark)| set.then_some(mark))
    .collect()
}

/// Runs as Markdown lines, one per line of their text, each trimmed, with empty lines left
/// out. `block` says a line's start is a block's, where its first characters are escaped so
/// they can't start a heading, a list item, a quote or a rule; in a table cell they can't.
/// `cell` says the runs are in a table cell, where a `|` in LaTeX or a destination is escaped.
fn inline(runs: &[Run], block: bool, cell: bool) -> Vec<String> {
    let mut inline = Inline {
        block,
        cell,
        lines: Vec::new(),
        line: String::new(),
        open: Vec::new(),
    };
    for run in runs {
        inline.run(run);
    }
    inline.end_line();
    inline.lines
}

/// The state of runs being written: the lines done, the line being written, and the marks
/// open on it.
struct Inline {
    block: bool,
    cell: bool,
    lines: Vec<String>,
    line: String,
    open: Vec<Mark>,
}

impl Inline {
    fn run(&mut self, run: &Run) {
        if let Some(latex) = &run.latex {
            self.close_to(0);
            let fence = if run.math == Some("display") {
                "$$"
            } else {
                "$"
            };
            self.line.push_str(fence);
            self.line.push_str(&pipes(latex, self.cell));
            self.line.push_str(fence);
            return;
        }
        for (index, segment) in run.text.replace("\r\n", "\n").split(is_break).enumerate() {
            if index > 0 {
                self.end_line();
            }
            self.segment(segment, run);
        }
    }

    /// Text without a line break, in its run's formatting. Its leading and trailing spaces go
    /// outside its marks, where emphasis can close and open.
    fn segment(&mut self, text: &str, run: &Run) {
        let text = if self.line.is_empty() {
            text.trim_start()
        } else {
            text
        };
        let core = text.trim();
        if core.is_empty() {
            self.line.push_str(text);
            return;
        }
        let start = text.len() - text.trim_start().len();
        let (lead, trail) = (&text[..start], &text[start + core.len()..]);
        let wanted = marks(run);
        if let Some(link) = &run.link {
            self.close_to(0);
            self.line.push_str(lead);
            self.line.push('[');
            self.open(&wanted);
            self.line.push_str(&escape(core, false));
            self.close_to(0);
            self.line.push_str("](");
            self.line.push_str(&target(link, self.cell));
            self.line.push(')');
        } else {
            let kept = self
                .open
                .iter()
                .zip(&wanted)
                .take_while(|(open, wanted)| open == wanted)
                .count();
            self.close_to(kept);
            self.line.push_str(lead);
            self.open(&wanted[kept..]);
            let at_start = self.block && self.line.is_empty();
            self.line.push_str(&escape(core, at_start));
        }
        self.line.push_str(trail);
    }

    fn open(&mut self, marks: &[Mark]) {
        for &mark in marks {
            self.line.push_str(mark.delimiter());
            self.open.push(mark);
        }
    }

    /// Closes the open marks above the first `keep`, inside the line's trailing spaces.
    fn close_to(&mut self, keep: usize) {
        if self.open.len() <= keep {
            return;
        }
        let trimmed = self.line.trim_end().len();
        let spaces = self.line.split_off(trimmed);
        while self.open.len() > keep {
            let mark = self.open.pop().unwrap();
            self.line.push_str(mark.delimiter());
        }
        self.line.push_str(&spaces);
    }

    fn end_line(&mut self) {
        self.close_to(0);
        let line = std::mem::take(&mut self.line);
        let line = line.trim_end();
        if !line.is_empty() {
            self.lines.push(line.to_owned());
        }
    }
}

/// A link's or an image's target as a Markdown destination, by the shared destination writer;
/// in a table cell, `cell`, its `|` escaped too.
fn target(target: &str, cell: bool) -> String {
    pipes(&destination(target), cell)
}

/// Text that isn't literal text, in a table cell, `cell`, with each `|` written `\|`: GFM
/// splits a row at every unescaped `|` before it reads a cell's spans, and takes `\|` back to
/// `|` inside them.
fn pipes(text: &str, cell: bool) -> String {
    if cell {
        text.replace('|', "\\|")
    } else {
        text.to_owned()
    }
}

/// Text with its whitespace one space, trimmed.
fn collapse(text: &str) -> String {
    text.split_whitespace().collect::<Vec<_>>().join(" ")
}

/// Text for a link's or an image's label: its whitespace one space, escaped.
fn label(text: &str) -> String {
    escape(&collapse(text), false)
}

/// An image's alt text, then the sentences of its OCR text that the alt text lacks, by the
/// shared content rule over the two as plain text; none when it has neither.
fn image_alt(image: &Image) -> Option<String> {
    let alt = image.alt_text.as_deref().unwrap_or("");
    let missing = match &image.ocr_text {
        Some(ocr) => content::missing(&Text::plain(ocr), &Text::plain(alt)),
        None => Vec::new(),
    };
    let parts: Vec<&str> = std::iter::once(alt)
        .filter(|alt| !alt.trim().is_empty())
        .chain(missing.iter().map(String::as_str))
        .collect();
    (!parts.is_empty()).then(|| parts.join(" "))
}

/// An image, `![alt](file)`; without a file, its data missing, `[image: alt]`, its alt text
/// else its filename. A hyperlinked image is a link around it, `[![alt](file)](hyperlink)`.
fn image_md(image: &Image, cell: bool) -> String {
    let alt = image_alt(image);
    let text = match &image.file {
        Some(file) => format!(
            "![{}]({})",
            label(alt.as_deref().unwrap_or("")),
            target(file, cell)
        ),
        None => match alt.as_ref().or(image.filename.as_ref()) {
            Some(name) => format!("image: {}", label(name)),
            None => "image".to_owned(),
        },
    };
    match &image.hyperlink {
        Some(link) => format!("[{text}]({})", target(link, cell)),
        None if image.file.is_some() => text,
        None => format!("[{text}]"),
    }
}

/// An embedded file, `[filename](file)`, named by its file when it has no filename; without a
/// file, `[file: filename]`.
fn file_md(file: &EmbeddedFile, cell: bool) -> String {
    match (&file.file, &file.filename) {
        (Some(stored), name) => format!(
            "[{}]({})",
            label(name.as_deref().unwrap_or(stored)),
            target(stored, cell)
        ),
        (None, Some(name)) => format!("[file: {}]", label(name)),
        (None, None) => "[file]".to_owned(),
    }
}

/// An ink drawing, `![ink: words](file)` with the recognised words its strokes belong to, or
/// `![ink](file)` with none.
fn ink_md(ink: &Ink, words: &Words, cell: bool) -> String {
    let text = words.of(ink);
    let alt = if text.is_empty() {
        "ink".to_owned()
    } else {
        format!("ink: {}", label(&text))
    };
    format!("![{alt}]({})", target(&ink.file, cell))
}

/// A GFM table: its first row the header, its rows padded to its widest, and a cell's blocks
/// joined by `<br>`. Its tags go on a line before it.
fn table_md(table: &Table, words: &Words) -> Option<Chunk> {
    let width = table.rows.iter().map(Vec::len).max().unwrap_or(0);
    if width == 0 {
        return None;
    }
    let mut lines = Vec::new();
    let tags = tag_prefix(&table.tags, false);
    if !tags.is_empty() {
        lines.push(tags.trim_end().to_owned());
    }
    let row = |cells: Vec<String>| format!("| {} |", cells.join(" | "));
    for (index, cells) in table.rows.iter().enumerate() {
        let mut texts: Vec<String> = cells.iter().map(|cell| cell_md(cell, words)).collect();
        texts.resize(width, String::new());
        lines.push(row(texts));
        if index == 0 {
            lines.push(row(vec!["---".to_owned(); width]));
        }
    }
    Some(Chunk::Text(lines))
}

/// A table cell's blocks, each on one line, joined by `<br>`.
fn cell_md(cell: &Cell, words: &Words) -> String {
    let mut parts = Vec::new();
    cell_items(&cell.items, words, &mut parts);
    parts.join("<br>")
}

/// A cell's items as one line each: a list item's marker as text, numbered among its
/// siblings, and its children after it.
fn cell_items(items: &[Item], words: &Words, parts: &mut Vec<String>) {
    let mut count: Option<i32> = None;
    for item in items {
        match item {
            Item::Group { items } => cell_items(items, words, parts),
            Item::Element { blocks, children } => {
                for (index, block) in blocks.iter().enumerate() {
                    let marker = match block {
                        Block::Paragraph(Paragraph {
                            list: Some(list), ..
                        }) if index == 0 => match number(list) {
                            Some(restart) => {
                                let n = restart.unwrap_or(count.map_or(1, |n| n + 1));
                                count = Some(n);
                                format!("{n}. ")
                            }
                            None => "- ".to_owned(),
                        },
                        _ => String::new(),
                    };
                    if let Some(text) = cell_block(block, words) {
                        parts.push(marker + &text);
                    }
                }
                cell_items(children, words, parts);
            }
        }
    }
}

/// A block on one line of a table cell: a paragraph's lines joined by `<br>`, a code
/// paragraph as a code span, its `|` escaped, a table inside a cell as its rows of cells.
fn cell_block(block: &Block, words: &Words) -> Option<String> {
    let text = match block {
        Block::Paragraph(p) => {
            let prefix = tag_prefix(&p.tags, false);
            let mut text = if style(p) == "code" {
                pipes(&code_span(&code_lines(&p.runs).join(" ")), true)
            } else {
                inline(&p.runs, false, true).join("<br>")
            };
            if let Some(ink) = &p.ink {
                if !text.is_empty() {
                    text.push_str("<br>");
                }
                text.push_str(&ink_md(ink, words, true));
            }
            if text.is_empty() {
                return None;
            }
            prefix + &text
        }
        Block::Table(table) => table
            .rows
            .iter()
            .map(|row| {
                row.iter()
                    .map(|cell| cell_md(cell, words))
                    .collect::<Vec<_>>()
                    .join(" \\| ")
            })
            .collect::<Vec<_>>()
            .join("<br>"),
        Block::Image(image) => image_md(image, true),
        Block::EmbeddedFile(file) => file_md(file, true),
        Block::Ink { ink } => ink_md(ink.as_ref()?, words, true),
    };
    (!text.is_empty()).then_some(text)
}

/// Code as a code span, its backticks one more than its longest run of them.
fn code_span(code: &str) -> String {
    if code.is_empty() {
        return String::new();
    }
    let ticks = "`".repeat(longest_backticks(code) + 1);
    let pad = if code.starts_with('`') || code.ends_with('`') {
        " "
    } else {
        ""
    };
    format!("{ticks}{pad}{code}{pad}{ticks}")
}

fn longest_backticks(text: &str) -> usize {
    text.split(|c| c != '`').map(str::len).max().unwrap_or(0)
}

/// Joins chunks into lines, a blank line between blocks. Code paragraphs side by side share a
/// fence and quotes a quote. A list's items are joined without blank lines and numbered from
/// its restart, each item's content indented under its marker; where a list's items would run
/// into the list before them, its restart or a container's start between them, an HTML
/// comment, CommonMark's own way to part two lists, keeps them apart. With `in_item`, the
/// chunks are a list item's content, and a list right after its first paragraph is nested
/// without a blank line when it can interrupt a paragraph.
fn join(chunks: Vec<Chunk>, in_item: bool) -> Vec<String> {
    let mut lines: Vec<String> = Vec::new();
    // The last item's list kind and number, while a list is open.
    let mut list: Option<(bool, i64)> = None;
    let mut parted = false;
    let mut prev_text = false;
    // How many blocks and items are joined so far.
    let mut count = 0;
    for chunk in merge(chunks) {
        let (block, item) = match chunk {
            Chunk::Break => {
                parted = true;
                continue;
            }
            Chunk::Item(item) => (None, Some(item)),
            Chunk::Text(text) => (Some(text), None),
            Chunk::Code(code) => (Some(fence(code)), None),
            Chunk::Quote(quote) => (
                Some(
                    quote
                        .into_iter()
                        .map(|line| {
                            if line.is_empty() {
                                ">".to_owned()
                            } else {
                                format!("> {line}")
                            }
                        })
                        .collect(),
                ),
                None,
            ),
        };
        if let Some(block) = block {
            if !lines.is_empty() {
                lines.push(String::new());
            }
            prev_text = true;
            list = None;
            lines.extend(block);
        } else if let Some(item) = item {
            let ordered = item.number.is_some();
            let continues = list.filter(|(kind, _)| *kind == ordered);
            let counted = continues.filter(|_| !parted);
            let n = match item.number {
                Some(restart) => match (restart, counted) {
                    (Some(restart), _) => i64::from(restart),
                    (None, Some((_, last))) => last + 1,
                    (None, None) => 1,
                },
                None => 0,
            };
            match continues {
                Some((_, last)) if parted || (ordered && n != last + 1) => {
                    lines.extend([String::new(), "<!-- -->".to_owned(), String::new()]);
                }
                Some(_) => {}
                None if lines.is_empty() => {}
                None if in_item && count == 1 && prev_text && (!ordered || n == 1) => {}
                None => lines.push(String::new()),
            }
            let marker = if ordered {
                format!("{n}. ")
            } else {
                "- ".to_owned()
            };
            let boxed = match item.task {
                Some(true) => "[x] ",
                Some(false) => "[ ] ",
                None => "",
            };
            let indent = " ".repeat(marker.len());
            let mut body = item.body.into_iter();
            let head = format!("{marker}{boxed}{}", body.next().unwrap_or_default());
            lines.push(head.trim_end().to_owned());
            for line in body {
                lines.push(if line.is_empty() {
                    line
                } else {
                    format!("{indent}{line}")
                });
            }
            list = Some((ordered, n));
            prev_text = false;
        }
        parted = false;
        count += 1;
    }
    lines
}

/// Code paragraphs side by side as one, and quotes side by side as one, a blank line between
/// their paragraphs.
fn merge(chunks: Vec<Chunk>) -> Vec<Chunk> {
    let mut merged: Vec<Chunk> = Vec::with_capacity(chunks.len());
    for chunk in chunks {
        match (merged.last_mut(), chunk) {
            (Some(Chunk::Code(lines)), Chunk::Code(more)) => lines.extend(more),
            (Some(Chunk::Quote(lines)), Chunk::Quote(more)) => {
                lines.push(String::new());
                lines.extend(more);
            }
            (_, chunk) => merged.push(chunk),
        }
    }
    merged
}

/// Code lines in a fence of backticks, one more than the longest run of them in the code and
/// three at least.
fn fence(code: Vec<String>) -> Vec<String> {
    let longest = code.iter().map(|line| longest_backticks(line)).max();
    let ticks = "`".repeat(longest.unwrap_or(0).max(2) + 1);
    let mut lines = vec![ticks.clone()];
    lines.extend(code);
    lines.push(ticks);
    lines
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ink::Bounds;
    use crate::page::Offsets;

    fn text(text: &str) -> Run {
        Run {
            text: text.to_owned(),
            ..Run::default()
        }
    }

    fn para(style: &str, runs: Vec<Run>) -> Paragraph {
        Paragraph {
            style: Some(style.to_owned()),
            list: None,
            tags: Vec::new(),
            runs,
            ink: None,
        }
    }

    fn element(blocks: Vec<Block>, children: Vec<Item>) -> Item {
        Item::Element { blocks, children }
    }

    /// An element of one paragraph of style `p` and the text `t`.
    fn line(t: &str) -> Item {
        element(vec![Block::Paragraph(para("p", vec![text(t)]))], Vec::new())
    }

    fn at(vertical: f32, horizontal: f32) -> Offsets {
        Offsets {
            offset_horizontal: Some(horizontal),
            offset_vertical: Some(vertical),
        }
    }

    fn outline(vertical: f32, horizontal: f32, items: Vec<Item>) -> Container {
        Container::Outline {
            offsets: at(vertical, horizontal),
            items,
        }
    }

    fn page_of(content: Vec<Container>) -> Page {
        Page {
            content,
            ink: None,
            words: Vec::new(),
        }
    }

    /// The Markdown of one outline holding `items`.
    fn md(items: Vec<Item>) -> String {
        page(&page_of(vec![outline(1.0, 1.0, items)]))
    }

    fn listed(list: List, t: &str, children: Vec<Item>) -> Item {
        let mut p = para("p", vec![text(t)]);
        p.list = Some(list);
        element(vec![Block::Paragraph(p)], children)
    }

    fn bullet() -> List {
        List {
            numbered: false,
            restart: None,
        }
    }

    fn numbered(restart: Option<i32>) -> List {
        List {
            numbered: true,
            restart,
        }
    }

    /// A tag labelled `label`, a To Do when `task`, completed when `completed`.
    fn tag(label: &str, task: bool, completed: bool) -> Tag {
        Tag {
            label: Some(label.to_owned()),
            task,
            completed,
        }
    }

    fn tagged(tags: Vec<Tag>, t: &str) -> Item {
        let mut p = para("p", vec![text(t)]);
        p.tags = tags;
        element(vec![Block::Paragraph(p)], Vec::new())
    }

    fn image(file: Option<&str>, alt: Option<&str>, filename: Option<&str>) -> Image {
        Image {
            file: file.map(str::to_owned),
            filename: filename.map(str::to_owned),
            alt_text: alt.map(str::to_owned),
            ocr_text: None,
            hyperlink: None,
        }
    }

    /// A drawing whose strokes belong to the words `words`, by id.
    fn ink(file: &str, words: &[u32]) -> Ink {
        Ink {
            file: file.to_owned(),
            words: words.to_vec(),
        }
    }

    /// Recognised words, their ids 10, 11, and so on, in the recognition's order.
    fn words(texts: &[&str]) -> Vec<Word> {
        (10..)
            .zip(texts)
            .map(|(id, text)| Word {
                id,
                text: (*text).to_owned(),
            })
            .collect()
    }

    /// A paragraph of handwriting, its drawing `ink`.
    fn handwriting(ink: Ink) -> Item {
        let mut p = para("p", Vec::new());
        p.ink = Some(ink);
        element(vec![Block::Paragraph(p)], Vec::new())
    }

    /// A page ink container whose strokes start `y` ink units down the page.
    fn ink_at(y: f32) -> Container {
        Container::Ink {
            offsets: at(0.0, 0.0),
            bounds: Some(Bounds {
                x: 1270.0,
                y,
                width: 10.0,
                height: 10.0,
            }),
        }
    }

    fn cell(items: Vec<Item>) -> Cell {
        Cell { items }
    }

    #[test]
    fn containers_go_title_first_then_top_to_bottom_then_left_to_right() {
        let title = Container::Title {
            items: vec![element(
                vec![Block::Paragraph(para("PageTitle", vec![text("Title")]))],
                Vec::new(),
            )],
        };
        let page = page_of(vec![
            outline(5.0, 1.0, vec![line("third")]),
            outline(2.0, 7.0, vec![line("second")]),
            title,
            outline(2.0, 1.0, vec![line("first")]),
            Container::Image {
                offsets: Offsets {
                    offset_horizontal: None,
                    offset_vertical: None,
                },
                image: image(Some("origin.png"), Some("at the origin"), None),
            },
        ]);
        assert_eq!(
            super::page(&page),
            "# Title\n\n![at the origin](origin.png)\n\nfirst\n\nsecond\n\nthird"
        );
    }

    /// A page's ink goes once, where its first ink container's strokes are: their bounds, in
    /// ink units on the page, place it, since its offsets don't. Its alt text is the words its
    /// strokes belong to.
    #[test]
    fn page_ink_goes_once_where_its_strokes_are() {
        let mut page = page_of(vec![
            ink_at(10.0 * UNITS_PER_HALF_INCH),
            outline(2.0, 1.0, vec![line("above")]),
            Container::Ink {
                offsets: at(0.0, 0.0),
                bounds: None,
            },
            ink_at(4.0 * UNITS_PER_HALF_INCH),
            outline(20.0, 1.0, vec![line("below")]),
        ]);
        page.ink = Some(ink("page.svg", &[10, 11, 12]));
        page.words = words(&["Hello", "World", "a*b"]);
        assert_eq!(
            super::page(&page),
            "above\n\n![ink: Hello World a\\*b](page.svg)\n\nbelow"
        );
        page.words.clear();
        assert!(super::page(&page).contains("\n![ink](page.svg)\n"));
    }

    /// A paragraph's handwriting alone, with no page ink: its drawing's alt text is its own
    /// strokes' words, in the recognition's order, not the order its strokes name them in.
    #[test]
    fn paragraph_ink_alone_takes_its_own_words() {
        let mut page = page_of(vec![outline(
            1.0,
            1.0,
            vec![line("before"), handwriting(ink("hand.svg", &[11, 10]))],
        )]);
        page.words = words(&["Dear", "diary"]);
        assert_eq!(super::page(&page), "before\n\n![ink: Dear diary](hand.svg)");
    }

    /// Page ink and paragraph ink together: each drawing takes the words its own strokes belong
    /// to, and a word whose strokes are in both is in both; none is stray.
    #[test]
    fn page_and_paragraph_ink_each_take_their_own_words() {
        let mut page = page_of(vec![
            outline(
                1.0,
                1.0,
                vec![
                    handwriting(ink("first.svg", &[10, 11])),
                    handwriting(ink("second.svg", &[11, 13])),
                ],
            ),
            ink_at(10.0 * UNITS_PER_HALF_INCH),
        ]);
        page.ink = Some(ink("page.svg", &[12]));
        page.words = words(&["one", "two", "three", "four"]);
        assert_eq!(
            super::page(&page),
            "![ink: one two](first.svg)\n\n![ink: two four](second.svg)\n\n\
             ![ink: three](page.svg)"
        );
    }

    /// A recognised word no drawing carries, its strokes in none, is written once, as one
    /// plain line after the page's content, beside the words a drawing does carry; a drawing in
    /// a table cell carries its words too.
    #[test]
    fn a_stray_recognised_word_is_one_line_after_the_content() {
        let table = Table {
            tags: Vec::new(),
            rows: vec![vec![cell(vec![handwriting(ink("cell.svg", &[12]))])]],
        };
        let mut page = page_of(vec![
            outline(
                1.0,
                1.0,
                vec![
                    handwriting(ink("hand.svg", &[11])),
                    element(vec![Block::Table(table)], Vec::new()),
                ],
            ),
            outline(5.0, 1.0, vec![line("after")]),
        ]);
        page.words = words(&["#lost", "kept", "boxed", "*also*"]);
        assert_eq!(
            super::page(&page),
            "![ink: kept](hand.svg)\n\n| ![ink: boxed](cell.svg) |\n| --- |\n\nafter\n\n\
             \\#lost \\*also\\*"
        );
        // With no drawing at all, every word is the one line.
        let mut page = page_of(Vec::new());
        page.words = words(&["only", "words"]);
        assert_eq!(super::page(&page), "only words");
    }

    /// The title is `#`, an hN paragraph one level down, capped at 6, and a page-title
    /// paragraph outside the title area is text.
    #[test]
    fn headings_sit_below_the_title() {
        let title = Container::Title {
            items: vec![element(
                vec![Block::Paragraph(para("PageTitle", vec![text("Notes")]))],
                Vec::new(),
            )],
        };
        let heading = |style: &str| {
            element(
                vec![Block::Paragraph(para(style, vec![text(style)]))],
                Vec::new(),
            )
        };
        let page = page_of(vec![
            title,
            outline(
                1.0,
                1.0,
                vec![
                    heading("h1"),
                    heading("h2"),
                    heading("h5"),
                    heading("h6"),
                    heading("PageTitle"),
                ],
            ),
        ]);
        assert_eq!(
            super::page(&page),
            "# Notes\n\n## h1\n\n### h2\n\n###### h5\n\n###### h6\n\nPageTitle"
        );
    }

    #[test]
    fn bold_italic_strikethrough_and_links_are_marked() {
        let runs = vec![
            Run {
                bold: true,
                ..text("Lorem ")
            },
            text("ipsum "),
            Run {
                italic: true,
                ..text("dolor")
            },
            text(" "),
            Run {
                strikethrough: true,
                ..text("sit")
            },
            text(" "),
            Run {
                bold: true,
                italic: true,
                ..text("amet")
            },
            Run {
                bold: true,
                ..text(" consetetur")
            },
            text(", "),
            Run {
                link: Some("https://example.com/a_(b)".to_owned()),
                ..text("a [link]")
            },
            text(" and "),
            Run {
                link: Some("https://example.com".to_owned()),
                bold: true,
                ..text("bold link")
            },
        ];
        assert_eq!(
            md(vec![element(
                vec![Block::Paragraph(para("p", runs))],
                Vec::new()
            )]),
            "**Lorem** ipsum *dolor* ~~sit~~ ***amet* consetetur**, \
             [a \\[link\\]](https://example.com/a_\\(b\\)) and [**bold link**](https://example.com)"
        );
    }

    /// A line break in a paragraph is a hard break, and empty paragraphs, OneNote's blank
    /// lines, are left out.
    #[test]
    fn line_breaks_are_hard_breaks() {
        let items = vec![
            line("one\u{b}  # two\n\nthree"),
            element(vec![Block::Paragraph(para("p", Vec::new()))], Vec::new()),
            line("four"),
        ];
        assert_eq!(md(items), "one  \n\\# two  \nthree\n\nfour");
    }

    /// Code paragraphs side by side share one fence, their text as stored; a fence is longer
    /// than any run of backticks in its code.
    #[test]
    fn code_paragraphs_are_fenced() {
        let code = |t: &str| {
            element(
                vec![Block::Paragraph(para("code", vec![text(t)]))],
                Vec::new(),
            )
        };
        assert_eq!(
            md(vec![code("let a = *b;"), code("  [x] | y"), line("after")]),
            "```\nlet a = *b;\n  [x] | y\n```\n\nafter"
        );
        assert_eq!(md(vec![code("a ``` b")]), "````\na ``` b\n````");
    }

    #[test]
    fn quotes_and_citations_are_quoted() {
        let quote = |style: &str, t: &str| {
            element(
                vec![Block::Paragraph(para(style, vec![text(t)]))],
                Vec::new(),
            )
        };
        assert_eq!(
            md(vec![
                quote("Quote", "said"),
                quote("Quote", "more"),
                line("between"),
                quote("Citation", "cited"),
            ]),
            "> said\n>\n> more\n\nbetween\n\n> cited"
        );
    }

    #[test]
    fn lists_nest_under_their_items() {
        let items = vec![
            listed(
                bullet(),
                "one",
                vec![listed(
                    bullet(),
                    "one.one",
                    vec![listed(numbered(None), "deep", Vec::new())],
                )],
            ),
            listed(bullet(), "two", Vec::new()),
        ];
        assert_eq!(md(items), "- one\n  - one.one\n    1. deep\n- two");
    }

    /// Numbers count from a list's restart, else from 1; a restart that breaks the count
    /// starts a new list, parted from the one before it by an HTML comment, and so does a
    /// container's start.
    #[test]
    fn lists_are_numbered_from_their_restart() {
        let items = vec![
            listed(numbered(Some(3)), "three", Vec::new()),
            listed(numbered(None), "four", Vec::new()),
            listed(numbered(Some(5)), "five", Vec::new()),
            listed(numbered(Some(1)), "one again", Vec::new()),
        ];
        assert_eq!(
            md(items),
            "3. three\n4. four\n5. five\n\n<!-- -->\n\n1. one again"
        );
        let page = page_of(vec![
            outline(1.0, 1.0, vec![listed(numbered(None), "a", Vec::new())]),
            outline(2.0, 1.0, vec![listed(numbered(None), "b", Vec::new())]),
        ]);
        assert_eq!(super::page(&page), "1. a\n\n<!-- -->\n\n1. b");
    }

    /// An element without a list marker can't indent in Markdown, so its children are blocks
    /// at its level; a non-list block in a list item is indented under it.
    #[test]
    fn indented_text_outside_a_list_stays_at_its_level() {
        let items = vec![
            element(
                vec![Block::Paragraph(para("p", vec![text("parent")]))],
                vec![line("child")],
            ),
            listed(bullet(), "item", vec![line("inside")]),
        ];
        assert_eq!(md(items), "parent\n\nchild\n\n- item\n\n  inside");
    }

    /// A To Do tag makes a task item, `[ ]` open and `[x]` done, a bullet unless its paragraph
    /// is numbered. Every tag's label is a `[Label]` prefix, a To Do's after its box.
    #[test]
    fn to_do_tags_are_task_items_and_others_prefixes() {
        let mut numbered_task = para("p", vec![text("numbered")]);
        numbered_task.list = Some(numbered(None));
        numbered_task.tags = vec![tag("To Do", true, false)];
        let items = vec![
            tagged(vec![tag("To Do", true, false)], "open"),
            tagged(
                vec![tag("To Do", true, true), tag("Important", false, false)],
                "done",
            ),
            tagged(vec![tag("Important", false, true)], "starred"),
            tagged(vec![tag("Question *", false, false)], "asked"),
            element(vec![Block::Paragraph(numbered_task)], Vec::new()),
        ];
        assert_eq!(
            md(items),
            "- [ ] [To Do] open\n- [x] [To Do] [Important] done\n\n[Important] starred\n\n\
             [Question \\*] asked\n\n1. [ ] [To Do] numbered"
        );
    }

    /// A table is GFM's: its first row the header, short rows padded, a cell's blocks joined
    /// by `<br>`, and a pipe in a cell escaped.
    #[test]
    fn tables_are_gfm() {
        let table = Table {
            tags: vec![tag("Important", false, false)],
            rows: vec![
                vec![
                    cell(vec![line("A")]),
                    cell(vec![line("B")]),
                    cell(vec![line("C")]),
                ],
                vec![
                    cell(vec![line("a|b"), line("- not a list")]),
                    cell(Vec::new()),
                ],
                vec![
                    cell(vec![listed(bullet(), "x", Vec::new())]),
                    cell(vec![element(
                        vec![Block::Image(image(Some("i.png"), Some("pic"), None))],
                        Vec::new(),
                    )]),
                    cell(vec![line("one\u{b}two")]),
                ],
            ],
        };
        assert_eq!(
            md(vec![element(vec![Block::Table(table)], Vec::new())]),
            "[Important]\n| A | B | C |\n| --- | --- | --- |\n\
             | a\\|b<br>- not a list |  |  |\n| - x | ![pic](i.png) | one<br>two |"
        );
    }

    /// A tag without a label is no prefix; a To Do's box still is. A table's tags, on the
    /// line before it, take the same prefix, a To Do's box first.
    #[test]
    fn a_tags_label_follows_its_box() {
        let unlabelled = Tag {
            label: None,
            task: true,
            completed: false,
        };
        let table = Table {
            tags: vec![tag("To Do", true, true), tag("Idea", false, false)],
            rows: vec![vec![cell(vec![line("A")])]],
        };
        assert_eq!(
            md(vec![
                tagged(vec![unlabelled], "bare"),
                element(vec![Block::Table(table)], Vec::new()),
            ]),
            "- [ ] bare\n\n[x] [To Do] [Idea]\n| A |\n| --- |"
        );
    }

    /// The title area's date and time paragraphs are one line after the title heading, joined
    /// by one space, escaped as a line's start; outside the title area, such a paragraph is text.
    #[test]
    fn the_titles_date_and_time_are_one_line_after_it() {
        let paragraph = |style: &str, t: &str| {
            element(
                vec![Block::Paragraph(para(style, vec![text(t)]))],
                Vec::new(),
            )
        };
        let title = Container::Title {
            items: vec![
                paragraph("PageTitle", "Notes"),
                paragraph("PageDateTime", "27. October 2020"),
                paragraph("PageDateTime", "10:41"),
            ],
        };
        let page = page_of(vec![
            outline(1.0, 1.0, vec![paragraph("PageDateTime", "Monday")]),
            title,
        ]);
        assert_eq!(
            super::page(&page),
            "# Notes\n\n27\\. October 2020 10:41\n\nMonday"
        );
        // An untitled page's date and time are its title area's one line.
        let page = page_of(vec![Container::Title {
            items: vec![
                paragraph("PageDateTime", "Tuesday"),
                paragraph("PageDateTime", "09:00"),
            ],
        }]);
        assert_eq!(super::page(&page), "Tuesday 09:00");
    }

    /// A hyperlinked image is a link around the image; without a file, around its text.
    #[test]
    fn a_hyperlinked_image_is_a_link() {
        let linked = |file: Option<&str>, alt: Option<&str>| Image {
            hyperlink: Some("https://example.com/a_(b)".to_owned()),
            ..image(file, alt, None)
        };
        let blocks = vec![
            Block::Image(linked(Some("a.png"), Some("a cat"))),
            Block::Image(linked(None, Some("lost"))),
            Block::Image(linked(None, None)),
        ];
        assert_eq!(
            md(vec![element(blocks, Vec::new())]),
            "[![a cat](a.png)](https://example.com/a_\\(b\\))\n\n\
             [image: lost](https://example.com/a_\\(b\\))\n\n\
             [image](https://example.com/a_\\(b\\))"
        );
    }

    /// Destinations are the shared writer's: one holding a space is written in `<…>`, so a
    /// spaced link and a hyperlinked image with a spaced target stay links, and each `\` is
    /// escaped, so a UNC path keeps both its leading backslashes. In a table cell, a
    /// `|` in a destination, a code span or LaTeX is escaped, so GFM keeps the cell whole.
    #[test]
    fn destinations_go_through_the_shared_writer_and_cells_escape_pipes() {
        let spaced = Run {
            link: Some("\\\\server\\Team Notes\\plan.docx".to_owned()),
            ..text("the plan")
        };
        let piped = Run {
            link: Some("https://example.com/?a|b".to_owned()),
            ..text("piped")
        };
        let linked = || Image {
            hyperlink: Some("https://example.com/Team Notes/<plan>".to_owned()),
            ..image(Some("i.png"), Some("pic"), None)
        };
        let latex = Run {
            math: Some("inline"),
            latex: Some("\\|x\\| = |y|".to_owned()),
            ..Run::default()
        };
        let code = para("code", vec![text("a | b")]);
        let link_md = "[the plan](<\\\\\\\\server\\\\Team Notes\\\\plan.docx>)";
        let image_md = "[![pic](i.png)](<https://example.com/Team Notes/\\<plan\\>>)";
        assert_eq!(
            md(vec![element(
                vec![
                    Block::Paragraph(para("p", vec![spaced.clone()])),
                    Block::Image(linked()),
                ],
                Vec::new()
            )]),
            format!("{link_md}\n\n{image_md}")
        );
        let table = Table {
            tags: Vec::new(),
            rows: vec![
                vec![cell(vec![line("A")]), cell(vec![line("B")])],
                vec![
                    cell(vec![element(
                        vec![Block::Paragraph(para("p", vec![spaced, piped]))],
                        Vec::new(),
                    )]),
                    cell(vec![element(vec![Block::Image(linked())], Vec::new())]),
                ],
                vec![
                    cell(vec![element(
                        vec![Block::Paragraph(para("p", vec![text("so "), latex]))],
                        Vec::new(),
                    )]),
                    cell(vec![element(vec![Block::Paragraph(code)], Vec::new())]),
                ],
            ],
        };
        assert_eq!(
            md(vec![element(vec![Block::Table(table)], Vec::new())]),
            format!(
                "| A | B |\n| --- | --- |\n\
                 | {link_md}[piped](https://example.com/?a\\|b) | {image_md} |\n\
                 | so $\\\\|x\\\\| = \\|y\\|$ | `a \\| b` |"
            )
        );
    }

    /// An image's alt text is followed by its OCR text's sentences the alt text lacks, by the
    /// shared content rule: a sentence whose words the alt text holds, in order, adds nothing,
    /// whatever its punctuation or spacing. With no alt text, the OCR text is the alt text.
    #[test]
    fn an_images_ocr_sentences_follow_its_alt_text() {
        let scanned = |alt: Option<&str>, ocr: &str| Image {
            ocr_text: Some(ocr.to_owned()),
            ..image(Some("s.png"), alt, None)
        };
        let blocks = vec![
            Block::Image(scanned(
                Some("Quarterly plan, draft"),
                "Quarterly plan -- draft.\nOwner: Ana.\r\nDue  Friday.",
            )),
            Block::Image(scanned(Some("All of it"), "All of it.")),
            Block::Image(scanned(None, "Only OCR [here].")),
        ];
        assert_eq!(
            md(vec![element(blocks, Vec::new())]),
            "![Quarterly plan, draft Owner: Ana. Due Friday.](s.png)\n\n\
             ![All of it](s.png)\n\n![Only OCR \\[here\\].](s.png)"
        );
    }

    #[test]
    fn math_is_dollar_delimited() {
        let math = |mode: &'static str, latex: &str| Run {
            math: Some(mode),
            latex: Some(latex.to_owned()),
            ..Run::default()
        };
        let items = vec![
            element(
                vec![Block::Paragraph(para(
                    "p",
                    vec![text("so "), math("inline", "x_1^2"), text(" holds")],
                ))],
                Vec::new(),
            ),
            element(
                vec![Block::Paragraph(para(
                    "p",
                    vec![math("display", "\\frac{a}{b}")],
                ))],
                Vec::new(),
            ),
        ];
        assert_eq!(md(items), "so $x_1^2$ holds\n\n$$\\frac{a}{b}$$");
    }

    /// Images, embedded files and ink name their files; without a file, its data missing,
    /// each is text.
    #[test]
    fn media_name_their_files() {
        let file = |file: Option<&str>, filename: Option<&str>| EmbeddedFile {
            file: file.map(str::to_owned),
            filename: filename.map(str::to_owned),
        };
        let mut inked = para("p", Vec::new());
        inked.ink = Some(ink("hand.svg", &[]));
        let blocks = vec![
            Block::Image(image(Some("a.png"), Some("a [cat]"), None)),
            Block::Image(image(Some("b.png"), None, Some("b.png"))),
            Block::Image(image(None, Some("lost"), Some("lost.png"))),
            Block::Image(image(None, None, Some("lost.png"))),
            Block::Image(image(None, None, None)),
            Block::EmbeddedFile(file(Some("c.pdf"), Some("report_1.pdf"))),
            Block::EmbeddedFile(file(Some("d"), None)),
            Block::EmbeddedFile(file(None, Some("gone.docx"))),
            Block::EmbeddedFile(file(None, None)),
            Block::Ink {
                ink: Some(ink("drawn.svg", &[])),
            },
            Block::Ink { ink: None },
            Block::Paragraph(inked),
        ];
        assert_eq!(
            md(vec![element(blocks, Vec::new())]),
            "![a \\[cat\\]](a.png)\n\n![](b.png)\n\n[image: lost]\n\n[image: lost.png]\n\n\
             [image]\n\n[report\\_1.pdf](c.pdf)\n\n[d](d)\n\n[file: gone.docx]\n\n[file]\n\n\
             ![ink](drawn.svg)\n\n![ink](hand.svg)"
        );
    }

    /// Literal text is escaped by the shared escape: the characters Markdown reads as markup
    /// anywhere, and at a line's start the ones that would make it a heading, a rule, a quote
    /// or a list item. `$` is escaped since math is written between `$`s.
    #[test]
    fn literal_text_is_escaped_by_the_shared_escape() {
        let paragraphs = [
            "Hi *all* _x_ `c` [a](b) <t> & a|b ~~s~~ \\ $5 end",
            "# not a heading",
            "- not an item",
            "> not a quote",
            "1) not an item",
            "2. not an item",
            "-----",
            "===",
            "+ x",
            "a < b, 1 + 1 = 2, x > y, 3.5 and </p> <!-- c -->",
        ];
        let items = paragraphs.iter().map(|p| line(p)).collect();
        assert_eq!(
            md(items),
            "Hi \\*all\\* \\_x\\_ \\`c\\` \\[a\\](b) \\<t> \\& a\\|b \\~\\~s\\~\\~ \\\\ \\$5 end\n\n\
             \\# not a heading\n\n\\- not an item\n\n\\> not a quote\n\n1\\) not an item\n\n\
             2\\. not an item\n\n\\-----\n\n\\===\n\n\\+ x\n\n\
             a < b, 1 + 1 = 2, x > y, 3.5 and \\</p> \\<!-- c -->"
        );
    }

    /// A line's start is escaped only where it starts a line, not after a mark or in a cell.
    #[test]
    fn only_a_lines_start_is_escaped_as_one() {
        let bold = Run {
            bold: true,
            ..text("# bold")
        };
        assert_eq!(
            md(vec![element(
                vec![Block::Paragraph(para("p", vec![bold]))],
                Vec::new()
            )]),
            "**# bold**"
        );
    }

    #[test]
    fn nothing_to_render_is_empty() {
        let page = page_of(vec![outline(
            1.0,
            1.0,
            vec![element(
                vec![Block::Paragraph(para("p", Vec::new()))],
                Vec::new(),
            )],
        )]);
        assert_eq!(super::page(&page), "");
    }
}
