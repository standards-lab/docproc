//! A page's content: its title and its containers, mapped from onenote_parser's model into the
//! crate's own (src/page.rs), with outline items nested as stored, paragraphs split into runs,
//! equations read as LaTeX, and images', embedded files' and ink drawings' contents named, and
//! written, by the media directory, each recorded in the page's media as it is stored.

use std::io::Read;
use std::ops::Range;

use docproc_envelope::{Fault, Media, extension_of};
use onenote_parser::contents::{
    Content, EmbeddedFile, FileDataStatus, Image, List, NoteTag, Outline, OutlineElement,
    OutlineItem, ParagraphStyling, RichText, Table,
};
use onenote_parser::page::{Page, PageContent};
use onenote_parser::property::embedded_file::FileType;

use crate::ink::{self, Stroke};
use crate::math::{self, Object};
use crate::model::{InkAlternatives, Medium};
use crate::page::{self, Block, Container, Item, Offsets};
use crate::section::text;
use crate::warn;

/// A page's read: its content, the Markdown's input, and what its line carries beside the
/// Markdown.
pub struct PageRead {
    pub page: page::Page,
    /// Its images, embedded files and ink drawings, in the order they were read.
    pub media: Vec<Medium>,
    pub ink_alternatives: Vec<InkAlternatives>,
}

/// The page's title, when it has one, then its containers in stored order. A container of a
/// kind onenote_parser doesn't know is left out. Images', embedded files' and ink drawings'
/// contents go to `media` as they are read, so each is written before the page line that
/// names it; `place` names the page in warnings. The page's own ink, the strokes of all its
/// ink containers, is one drawing, stored after the rest.
pub fn page(page: &Page, media: &mut Media, place: &str) -> Result<PageRead, Fault> {
    let mut reader = Reader {
        media,
        place,
        read: Vec::new(),
        images: 0,
        files: 0,
        equations: 0,
    };
    let mut page_strokes = Vec::new();
    let mut containers = Vec::new();
    if let Some(title) = page.title() {
        let mut items = Vec::new();
        for outline in title.contents() {
            items.extend(reader.items(outline.items())?);
        }
        containers.push(Container::Title { items });
    }
    for content in page.contents() {
        containers.push(match content {
            PageContent::Outline(outline) => Container::Outline {
                offsets: outline_offsets(outline),
                items: reader.items(outline.items())?,
            },
            PageContent::Image(image) => Container::Image {
                offsets: Offsets {
                    offset_horizontal: image.offset_horizontal(),
                    offset_vertical: image.offset_vertical(),
                },
                image: reader.image(image)?,
            },
            PageContent::EmbeddedFile(file) => Container::EmbeddedFile {
                offsets: Offsets {
                    offset_horizontal: file.offset_horizontal(),
                    offset_vertical: file.offset_vertical(),
                },
                file: reader.embedded_file(file)?,
            },
            PageContent::Ink(ink) => {
                let mut strokes = Vec::new();
                ink::strokes(ink, true, &mut strokes);
                let container = Container::Ink {
                    offsets: Offsets {
                        offset_horizontal: ink.offset_horizontal(),
                        offset_vertical: ink.offset_vertical(),
                    },
                    bounds: ink::extent(&strokes),
                };
                page_strokes.extend(strokes);
                container
            }
            PageContent::Unknown => continue,
        });
    }
    let ink = reader.drawing(&page_strokes)?;
    let (words, ink_alternatives) = recognition(page);
    Ok(PageRead {
        page: page::Page {
            content: containers,
            ink,
            words,
        },
        media: reader.read,
        ink_alternatives,
    })
}

/// A page's recognised ink words, each its best guess and its id, over its recognition's lines
/// in order, and the other guesses of those that have them, each by its index among the words.
/// A word with no guess is left out.
fn recognition(page: &Page) -> (Vec<page::Word>, Vec<InkAlternatives>) {
    let mut words = Vec::new();
    let mut alternatives = Vec::new();
    let Some(recognition) = page.ink_recognition() else {
        return (words, alternatives);
    };
    for word in recognition.lines().iter().flat_map(|line| line.words()) {
        let Some((best, rest)) = word.alternatives().split_first() else {
            continue;
        };
        if !rest.is_empty() {
            alternatives.push(InkAlternatives {
                word: words.len(),
                alternatives: rest.to_vec(),
            });
        }
        words.push(page::Word {
            id: word.id(),
            text: best.clone(),
        });
    }
    (words, alternatives)
}

fn outline_offsets(outline: &Outline) -> Offsets {
    Offsets {
        offset_horizontal: outline.offset_horizontal(),
        offset_vertical: outline.offset_vertical(),
    }
}

/// One page's read: the media its images, embedded files and drawings go to, the record of each
/// in the order it is read, and the counts that name each image, embedded file and equation in
/// a warning, in the order they are read.
struct Reader<'a> {
    media: &'a mut Media,
    place: &'a str,
    read: Vec<Medium>,
    images: usize,
    files: usize,
    equations: usize,
}

impl Reader<'_> {
    fn items(&mut self, items: &[OutlineItem]) -> Result<Vec<Item>, Fault> {
        items
            .iter()
            .map(|item| match item {
                OutlineItem::Element(element) => self.element(element),
                OutlineItem::Group(group) => Ok(Item::Group {
                    items: self.items(group.outlines())?,
                }),
            })
            .collect()
    }

    /// An outline element. Its list marker goes on its first block when that is a paragraph:
    /// onenote_parser keeps the marker on the element, and an element holds one paragraph in
    /// every file seen.
    fn element(&mut self, element: &OutlineElement) -> Result<Item, Fault> {
        let mut list = element.list_contents().first().and_then(marker);
        let mut blocks = Vec::new();
        for (index, content) in element.contents().iter().enumerate() {
            blocks.push(match content {
                Content::RichText(text) => {
                    let marker = if index == 0 { list.take() } else { None };
                    let mut paragraph = self.paragraph(text, marker);
                    if !text.embedded_objects().is_empty() {
                        let strokes = ink::flow(ink::pieces(text.embedded_objects()));
                        paragraph.ink = self.drawing(&strokes)?;
                    }
                    Block::Paragraph(paragraph)
                }
                Content::Table(table) => Block::Table(self.table(table)?),
                Content::Image(image) => Block::Image(self.image(image)?),
                Content::EmbeddedFile(file) => Block::EmbeddedFile(self.embedded_file(file)?),
                Content::Ink(drawn) => {
                    let mut strokes = Vec::new();
                    ink::strokes(drawn, false, &mut strokes);
                    Block::Ink {
                        ink: self.drawing(&strokes)?,
                    }
                }
                Content::Unknown => continue,
            });
        }
        Ok(Item::Element {
            blocks,
            children: self.items(element.children())?,
        })
    }

    fn paragraph(&mut self, text: &RichText, list: Option<page::List>) -> page::Paragraph {
        page::Paragraph {
            style: text
                .paragraph_style()
                .style_id()
                .filter(|style| !style.is_empty())
                .map(str::to_owned),
            list,
            tags: tags(text.note_tags()),
            runs: self.runs(text),
            ink: None,
        }
    }

    /// A paragraph's visible runs. Each stored formatting covers the text up to its run index,
    /// counted in UTF-16 code units, and the last covers the rest. Hidden runs are left out,
    /// an equation is one run, and neighbours left with the same formatting are joined.
    fn runs(&mut self, text: &RichText) -> Vec<page::Run> {
        let units: Vec<u16> = text.text().encode_utf16().collect();
        let styles = text.text_run_formatting();
        let default = ParagraphStyling::default();
        // A paragraph with text and no formatting is one run, unformatted.
        let styles: Vec<&ParagraphStyling> = if styles.is_empty() {
            vec![&default]
        } else {
            styles.iter().collect()
        };
        let stored = ranges(units.len(), text.text_run_indices(), styles.len())
            .into_iter()
            .zip(styles)
            .map(|(range, style)| {
                let stored = if style.math_formatting() {
                    Stored::Math
                } else if range.is_empty() || style.hidden() {
                    Stored::Skipped
                } else {
                    Stored::Text(
                        page::Run {
                            bold: style.bold(),
                            italic: style.italic(),
                            strikethrough: style.strikethrough(),
                            ..page::Run::default()
                        },
                        style.hyperlink(),
                    )
                };
                (range, stored)
            })
            .collect();
        let objects: Vec<Object> = text
            .math_inline_objects()
            .iter()
            .map(Object::from)
            .collect();
        let mut pieces = pieces(&units, stored, &objects);
        let links: Vec<(Range<usize>, String)> = text
            .hyperlinks()
            .iter()
            .map(|link| {
                (
                    link.start() as usize..link.end() as usize,
                    link.target().to_owned(),
                )
            })
            .collect();
        link(&mut pieces, &links);
        for piece in &mut pieces {
            if let Some(parts) = piece.equation.take() {
                piece.run.math = Some("inline");
                piece.run.latex = Some(self.latex(parts));
            }
        }
        let mut runs = join(pieces);
        math_modes(&mut runs);
        runs
    }

    /// An equation's LaTeX. One whose markers don't parse is the LaTeX of its text alone,
    /// with a warning naming it by its number on the page.
    fn latex(&mut self, parts: Vec<(String, Object)>) -> String {
        self.equations += 1;
        let text: String = parts.iter().map(|(text, _)| text.as_str()).collect();
        math::latex(parts).unwrap_or_else(|reason| {
            let place = format!("{}, equation {}", self.place, self.equations);
            warn(&place, &format!("its structure is lost: {reason}"));
            math::latex_of_text(&text)
        })
    }

    fn table(&mut self, table: &Table) -> Result<page::Table, Fault> {
        let mut rows = Vec::new();
        for row in table.contents() {
            let mut cells = Vec::new();
            for cell in row.contents() {
                cells.push(page::Cell {
                    items: cell
                        .contents()
                        .iter()
                        .map(|element| self.element(element))
                        .collect::<Result<_, _>>()?,
                });
            }
            rows.push(cells);
        }
        Ok(page::Table {
            tags: tags(table.note_tags()),
            rows,
        })
    }

    /// An image, the first of the reader's three store sites. Its file is named by its own
    /// stored extension, else its filename's.
    fn image(&mut self, image: &Image) -> Result<page::Image, Fault> {
        self.images += 1;
        let place = format!("{}, image {}", self.place, self.images);
        let extension = image
            .extension()
            .map(|ext| ext.strip_prefix('.').unwrap_or(ext))
            .filter(|ext| !ext.is_empty());
        let filename = text(image.image_filename());
        let available = image.data_status() == FileDataStatus::Available;
        let file = self.file(
            image.data_status(),
            image.read(),
            extension.or_else(|| filename.as_deref().and_then(extension_of)),
            &place,
        )?;
        self.read.push(Medium {
            kind: "image",
            file: file.clone(),
            filename: filename.clone(),
            size: image.size().filter(|_| available),
            extension: extension.map(str::to_owned),
            file_type: None,
        });
        Ok(page::Image {
            file,
            filename,
            alt_text: text(image.alt_text()),
            ocr_text: text(image.text()),
            hyperlink: text(image.hyperlink_url()),
        })
    }

    /// An embedded file, the second store site. Its file is named by its filename's extension.
    fn embedded_file(&mut self, file: &EmbeddedFile) -> Result<page::EmbeddedFile, Fault> {
        self.files += 1;
        let place = format!("{}, embedded file {}", self.place, self.files);
        let filename = text(Some(file.filename()));
        let available = file.data_status() == FileDataStatus::Available;
        let stored = self.file(
            file.data_status(),
            Some(file.read()),
            filename.as_deref().and_then(extension_of),
            &place,
        )?;
        self.read.push(Medium {
            kind: "embedded_file",
            file: stored.clone(),
            filename: filename.clone(),
            size: available.then(|| file.size()),
            extension: None,
            file_type: match file.file_type() {
                FileType::Audio => Some("audio"),
                FileType::Video => Some("video"),
                FileType::Unknown => None,
            },
        });
        Ok(page::EmbeddedFile {
            file: stored,
            filename,
        })
    }

    /// A drawing of `strokes`, the third store site: its SVG stored in the media, and its
    /// record, with the recognised words its strokes belong to. Strokes with no points draw
    /// nothing and have none, so their words are no drawing's.
    fn drawing(&mut self, strokes: &[Stroke]) -> Result<Option<page::Ink>, Fault> {
        let Some(svg) = ink::svg(strokes) else {
            return Ok(None);
        };
        let file = self.media.store(svg.as_slice(), Some("svg"))?;
        self.read.push(Medium {
            kind: "ink",
            file: Some(file.clone()),
            filename: None,
            size: None,
            extension: None,
            file_type: None,
        });
        let mut words: Vec<u32> = strokes.iter().filter_map(|stroke| stroke.word).collect();
        words.sort_unstable();
        words.dedup();
        Ok(Some(page::Ink { file, words }))
    }

    /// A record's file: its content streamed into the media and named. Data that is missing or
    /// invalid gives no file and a warning at `place`; data that fails partway through its read
    /// is a media fault, exit 1, as the store makes every payload it can't read.
    fn file(
        &mut self,
        status: FileDataStatus,
        read: Option<Box<dyn Read>>,
        extension: Option<&str>,
        place: &str,
    ) -> Result<Option<String>, Fault> {
        let problem = match (status, read) {
            (FileDataStatus::Available, Some(read)) => {
                return self.media.store(read, extension).map(Some);
            }
            (FileDataStatus::Invalid, _) => "its data is invalid",
            _ => "its data is missing",
        };
        warn(place, &format!("no file: {problem}"));
        Ok(None)
    }
}

/// A stored run, by what it becomes.
enum Stored {
    /// Visible text: its formatting, and whether it is styled as a link.
    Text(page::Run, bool),
    /// A math-formatted run: a part of an equation.
    Math,
    /// A hidden run, which holds a link's target among others, or an empty one: left out.
    Skipped,
}

/// A run as it is cut from the paragraph: its UTF-16 range, whether it is styled as a link,
/// and, for an equation, its stored runs, each with its math object.
struct Piece {
    range: Range<usize>,
    run: page::Run,
    hyperlink: bool,
    equation: Option<Vec<(String, Object)>>,
}

/// The pieces of a paragraph's stored runs over its UTF-16 `units`. Consecutive math runs are
/// one equation, ended by any other stored run. The math objects go to the math runs in order,
/// one each, as onenote_parser reads them; a math run past the last object has none, which
/// fails its equation's parse if the run holds a marker.
fn pieces(units: &[u16], stored: Vec<(Range<usize>, Stored)>, objects: &[Object]) -> Vec<Piece> {
    let mut objects = objects.iter().copied();
    let mut pieces: Vec<Piece> = Vec::new();
    let mut in_equation = false;
    for (range, stored) in stored {
        let text = String::from_utf16_lossy(&units[range.clone()]);
        match stored {
            Stored::Math => {
                let part = (text, objects.next().unwrap_or_default());
                match pieces.last_mut() {
                    Some(Piece {
                        range: last,
                        equation: Some(parts),
                        ..
                    }) if in_equation => {
                        last.end = range.end;
                        parts.push(part);
                    }
                    _ => pieces.push(Piece {
                        range,
                        run: page::Run::default(),
                        hyperlink: false,
                        equation: Some(vec![part]),
                    }),
                }
                in_equation = true;
            }
            Stored::Text(run, hyperlink) => {
                pieces.push(Piece {
                    range,
                    run: page::Run { text, ..run },
                    hyperlink,
                    equation: None,
                });
                in_equation = false;
            }
            Stored::Skipped => in_equation = false,
        }
    }
    pieces
}

/// The display mode of a paragraph's runs' equations: "display" for an equation that is its
/// paragraph's only run, "inline" for any other.
fn math_modes(runs: &mut [page::Run]) {
    let display = runs.len() == 1;
    for run in runs.iter_mut().filter(|run| run.math.is_some()) {
        run.math = Some(if display { "display" } else { "inline" });
    }
}

/// The UTF-16 ranges of `count` runs over `len` code units, ended by `indices`. The last run
/// takes the rest of the text, so none of it is lost when the indices run short; ranges are
/// kept inside the text and never run backwards.
fn ranges(len: usize, indices: &[u32], count: usize) -> Vec<Range<usize>> {
    let mut start = 0;
    (0..count)
        .map(|index| {
            let end = if index + 1 == count {
                len
            } else {
                indices.get(index).map_or(len, |&end| end as usize)
            };
            let end = end.clamp(start, len);
            let range = start..end;
            start = end;
            range
        })
        .collect()
}

/// Gives each run but an equation its link target. A run that starts inside one of onenote_parser's
/// hyperlinks, whose targets come from the hidden HYPERLINK fields, takes its target. A run
/// styled as a link outside any of them is a link OneNote recognised in typed text, and its
/// target is its own text, joined across the neighbouring link-styled runs it spans.
fn link(pieces: &mut [Piece], links: &[(Range<usize>, String)]) {
    for piece in pieces.iter_mut().filter(|piece| piece.equation.is_none()) {
        piece.run.link = links
            .iter()
            .find(|(range, _)| range.contains(&piece.range.start))
            .map(|(_, target)| target.clone());
    }
    let mut index = 0;
    while index < pieces.len() {
        if !pieces[index].hyperlink || pieces[index].run.link.is_some() {
            index += 1;
            continue;
        }
        let mut end = index + 1;
        while end < pieces.len()
            && pieces[end].hyperlink
            && pieces[end].run.link.is_none()
            && pieces[end].range.start == pieces[end - 1].range.end
        {
            end += 1;
        }
        let target: String = pieces[index..end]
            .iter()
            .map(|piece| piece.run.text.as_str())
            .collect();
        let target = target.trim().to_owned();
        if !target.is_empty() {
            for piece in &mut pieces[index..end] {
                piece.run.link = Some(target.clone());
            }
        }
        index = end;
    }
}

/// The runs, with neighbours of the same formatting and link joined; an equation is never
/// joined.
fn join(pieces: Vec<Piece>) -> Vec<page::Run> {
    let mut runs: Vec<page::Run> = Vec::with_capacity(pieces.len());
    for piece in pieces {
        match runs.last_mut() {
            Some(last)
                if last.math.is_none()
                    && piece.run.math.is_none()
                    && same_format(last, &piece.run) =>
            {
                last.text.push_str(&piece.run.text)
            }
            _ => runs.push(piece.run),
        }
    }
    runs
}

fn same_format(a: &page::Run, b: &page::Run) -> bool {
    page::Run {
        text: String::new(),
        ..a.clone()
    } == page::Run {
        text: String::new(),
        ..b.clone()
    }
}

/// A list marker. onenote_parser gives the stored format string less its length: a bullet
/// is its character; a number is U+FFFD followed by its MSONFC format code, with the text
/// written before and after the number around them. Only whether it is numbered, and where it
/// restarts, are read.
fn marker(list: &List) -> Option<page::List> {
    list_of(list.list_format(), list.list_restart())
}

/// The list marker of a stored format string and restart; none for an empty format.
fn list_of(format: &[char], restart: Option<i32>) -> Option<page::List> {
    let numbered = format
        .iter()
        .position(|&c| c == '\u{fffd}')
        .is_some_and(|at| at + 1 < format.len());
    (!format.is_empty()).then_some(page::List { numbered, restart })
}

/// Note tags: each its label, whether it is a To Do, and whether it is completed.
fn tags(tags: &[NoteTag]) -> Vec<page::Tag> {
    tags.iter()
        .map(|tag| {
            let definition = tag.definition();
            page::Tag {
                label: definition
                    .map(|d| d.label())
                    .filter(|label| !label.is_empty())
                    .map(str::to_owned),
                task: definition.is_some_and(|d| is_task(&format!("{:?}", d.shape()))),
                completed: tag.item_status().completed(),
            }
        })
        .collect()
}

/// Whether a tag's icon, by its name in onenote_parser's NoteTagShape, which the parser doesn't
/// export, is a check box: a To Do, a task.
fn is_task(shape: &str) -> bool {
    shape.contains("CheckBox")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn piece(range: Range<usize>, text: &str, hyperlink: bool) -> Piece {
        Piece {
            range,
            run: page::Run {
                text: text.to_owned(),
                ..page::Run::default()
            },
            hyperlink,
            equation: None,
        }
    }

    #[test]
    fn runs_split_on_utf16_indices() {
        // "𝑎=𝑏" is five UTF-16 code units: a surrogate pair, "=", a surrogate pair.
        let text = "𝑎=𝑏";
        let units: Vec<u16> = text.encode_utf16().collect();
        let ranges = ranges(units.len(), &[2, 3], 3);
        assert_eq!(ranges, vec![0..2, 2..3, 3..5]);
        let parts: Vec<String> = ranges
            .into_iter()
            .map(|range| String::from_utf16_lossy(&units[range]))
            .collect();
        assert_eq!(parts, ["𝑎", "=", "𝑏"]);
    }

    #[test]
    fn the_last_run_takes_the_rest_and_ranges_stay_in_the_text() {
        assert_eq!(ranges(10, &[4], 2), vec![0..4, 4..10]);
        // Too few indices: the middle run ends at the text's end, the last is empty.
        assert_eq!(ranges(10, &[4], 3), vec![0..4, 4..10, 10..10]);
        // An index past the end, or before the previous one, is clamped.
        assert_eq!(ranges(10, &[12, 3], 3), vec![0..10, 10..10, 10..10]);
        assert_eq!(ranges(0, &[], 1), vec![0..0]);
    }

    #[test]
    fn runs_inside_a_hyperlink_take_its_target() {
        let mut pieces = vec![
            piece(0..6, "go to ", false),
            piece(6..11, "magna", true),
            piece(11..12, ".", false),
        ];
        link(&mut pieces, &[(6..11, "https://example.com".to_owned())]);
        let links: Vec<Option<&str>> = pieces.iter().map(|p| p.run.link.as_deref()).collect();
        assert_eq!(links, [None, Some("https://example.com"), None]);
    }

    #[test]
    fn a_recognised_link_targets_its_own_text() {
        let mut pieces = vec![
            piece(0..4, "see ", false),
            piece(4..11, "http://", true),
            piece(11..23, "example.com/", true),
        ];
        link(&mut pieces, &[]);
        let links: Vec<Option<&str>> = pieces.iter().map(|p| p.run.link.as_deref()).collect();
        let target = Some("http://example.com/");
        assert_eq!(links, [None, target, target]);
    }

    #[test]
    fn neighbours_with_the_same_format_join() {
        let mut bold = piece(3..6, "def", false);
        bold.run.bold = true;
        let runs = join(vec![
            piece(0..2, "ab", false),
            piece(2..3, "c", false),
            bold,
        ]);
        let texts: Vec<&str> = runs.iter().map(|run| run.text.as_str()).collect();
        assert_eq!(texts, ["abc", "def"]);
    }

    fn math_object(kind: onenote_parser::contents::MathObjectType, args: u32) -> Object {
        Object {
            kind,
            args,
            ..Object::default()
        }
    }

    /// Consecutive math runs are one equation, which takes the math objects in order, one per
    /// math run; text, a hidden run or an empty one ends it.
    #[test]
    fn consecutive_math_runs_are_one_equation() {
        use onenote_parser::contents::MathObjectType as T;
        let text = "x: \u{fdd0}𝑎\u{fdee}2\u{fdef}+𝑏 or 𝑐";
        let units: Vec<u16> = text.encode_utf16().collect();
        // "x: ", then the superscript's three runs, "+𝑏", a hidden empty run, " or ", "𝑐".
        let stored = vec![
            (0..3, Stored::Text(page::Run::default(), false)),
            (3..4, Stored::Math),
            (4..7, Stored::Math),
            (7..9, Stored::Math),
            (9..12, Stored::Math),
            (12..12, Stored::Skipped),
            (12..16, Stored::Text(page::Run::default(), false)),
            (16..18, Stored::Math),
        ];
        let objects = [
            math_object(T::Superscript, 2),
            math_object(T::Superscript, 0),
            math_object(T::Superscript, 1),
            math_object(T::PlainText, 0),
            math_object(T::PlainText, 0),
        ];
        let pieces = pieces(&units, stored, &objects);
        let texts: Vec<&str> = pieces.iter().map(|p| p.run.text.as_str()).collect();
        assert_eq!(texts, ["x: ", "", " or ", ""]);
        let ranges: Vec<Range<usize>> = pieces.iter().map(|p| p.range.clone()).collect();
        assert_eq!(ranges, [0..3, 3..12, 12..16, 16..18]);
        let first = pieces[1].equation.as_ref().unwrap();
        let parts: Vec<&str> = first.iter().map(|(text, _)| text.as_str()).collect();
        assert_eq!(parts, ["\u{fdd0}", "𝑎\u{fdee}", "2\u{fdef}", "+𝑏"]);
        assert_eq!(first[0].1, objects[0]);
        assert_eq!(first[3].1, objects[3]);
        assert_eq!(math::latex(first.clone()), Ok("a^{2}+b".to_owned()));
        let second = pieces[3].equation.as_ref().unwrap();
        assert_eq!(second, &vec![("𝑐".to_owned(), objects[4])]);
    }

    /// A math run past the last stored object has none, and its equation's markers don't
    /// parse; its text alone is still read.
    #[test]
    fn a_math_run_without_an_object_has_none() {
        let text = "\u{fdd0}𝑥\u{fdef}";
        let units: Vec<u16> = text.encode_utf16().collect();
        let pieces = pieces(&units, vec![(0..4, Stored::Math)], &[]);
        let parts = pieces[0].equation.clone().unwrap();
        assert_eq!(parts[0].1, Object::default());
        assert!(math::latex(parts).is_err());
        assert_eq!(math::latex_of_text(text), "x");
    }

    fn equation(latex: &str) -> Piece {
        Piece {
            range: 0..0,
            run: page::Run {
                math: Some("inline"),
                latex: Some(latex.to_owned()),
                ..page::Run::default()
            },
            hyperlink: false,
            equation: None,
        }
    }

    /// Two equations side by side stay two runs, even with the same LaTeX.
    #[test]
    fn equations_never_join() {
        let runs = join(vec![equation("x"), equation("x"), piece(0..1, "y", false)]);
        assert_eq!(runs.len(), 3);
    }

    /// An equation is display math when it is its paragraph's only run, inline otherwise.
    #[test]
    fn an_equation_alone_is_display_math() {
        let mut alone = join(vec![equation("x")]);
        math_modes(&mut alone);
        assert_eq!(alone[0].math, Some("display"));
        let mut beside = join(vec![piece(0..2, "a ", false), equation("x")]);
        math_modes(&mut beside);
        assert_eq!(beside[1].math, Some("inline"));
        assert_eq!(beside[0].math, None);
    }

    /// Data that is missing or invalid gives no file, and nothing goes to the media, which a
    /// directory that can't be written would refuse; no committed fixture holds such a record.
    #[test]
    fn missing_or_invalid_data_has_no_file() {
        let mut media = Media::new(Some("/dev/null/media".into()));
        let mut reader = Reader {
            media: &mut media,
            place: "/Section, page 1",
            read: Vec::new(),
            images: 0,
            files: 0,
            equations: 0,
        };
        for status in [FileDataStatus::Missing, FileDataStatus::Invalid] {
            let read: Box<dyn Read> = Box::new(&b"bytes"[..]);
            let file = reader.file(status, Some(read), Some("png"), "/Section, page 1, image 1");
            assert!(matches!(file, Ok(None)), "{status:?}");
        }
        let file = reader.file(FileDataStatus::Available, None, None, "/Section, image 2");
        assert!(matches!(file, Ok(None)));
        assert!(reader.read.is_empty());
    }

    /// A list's format is a bullet's character, or U+FFFD and a number's format code between
    /// the text around the number: a number's when a code follows the U+FFFD. An empty format
    /// is no marker.
    #[test]
    fn a_list_marker_is_a_bullet_or_a_number() {
        let chars = |text: &str| text.chars().collect::<Vec<_>>();
        let read = |format: &str, restart| {
            list_of(&chars(format), restart).map(|list| (list.numbered, list.restart))
        };
        assert_eq!(read("•", None), Some((false, None)));
        assert_eq!(read("\u{fffd}\0.", None), Some((true, None)));
        assert_eq!(read("(\u{fffd}\u{4})", Some(3)), Some((true, Some(3))));
        // A U+FFFD with no code after it is a bullet's character.
        assert_eq!(read("\u{fffd}", None), Some((false, None)));
        assert_eq!(read("", Some(1)), None);
    }

    /// A tag is a task when its icon is one of the parser's check boxes, of any colour or mark.
    #[test]
    fn a_check_box_tag_is_a_task() {
        for shape in [
            "BlueCheckBox",
            "GreenStarCheckBox",
            "YellowCheckBox3",
            "RedFlagCheckBox",
        ] {
            assert!(is_task(shape), "{shape}");
        }
        for shape in ["YellowStar", "QuestionMark", "NoIcon"] {
            assert!(!is_task(shape), "{shape}");
        }
    }
}
