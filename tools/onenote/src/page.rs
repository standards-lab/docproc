//! A page's content as the reader reads it (src/content.rs): the Markdown's private input
//! (src/markdown.rs), never written as it is. It keeps only what the Markdown reads; the page
//! line's media are recorded beside it as they are stored.

use crate::ink::Bounds;

/// A page's content: its containers, its own ink drawing, and its recognised ink words.
pub struct Page {
    /// The page's containers, its title first, then the rest in stored order.
    pub content: Vec<Container>,
    /// The page's own ink, every ink container's strokes in one drawing at their offsets; none
    /// when the page has no strokes.
    pub ink: Option<Ink>,
    /// The page's recognised ink words, over its recognition's lines in order.
    pub words: Vec<Word>,
}

/// A recognised ink word: its best guess, and the id its strokes name it by.
pub struct Word {
    /// Unique among the page's words; meaningless across pages.
    pub id: u32,
    pub text: String,
}

/// An ink drawing: its SVG's name in the media directory, and the ids of the recognised words
/// its strokes belong to, each once.
pub struct Ink {
    pub file: String,
    pub words: Vec<u32>,
}

/// A container on a page. Offsets are from the page's origin, in half-inch units, as stored; a
/// container without one has none.
pub enum Container {
    /// The page's title area: its outlines' items, in order.
    Title {
        items: Vec<Item>,
    },
    Outline {
        offsets: Offsets,
        items: Vec<Item>,
    },
    Image {
        offsets: Offsets,
        image: Image,
    },
    EmbeddedFile {
        offsets: Offsets,
        file: EmbeddedFile,
    },
    /// One stored ink container, drawn in the page's ink, and the box its strokes span on the
    /// page, which strokes without points don't have.
    Ink {
        offsets: Offsets,
        bounds: Option<Bounds>,
    },
}

pub struct Offsets {
    pub offset_horizontal: Option<f32>,
    pub offset_vertical: Option<f32>,
}

/// An entry of an outline, nested as stored.
pub enum Item {
    /// An outline element: its blocks, then the items indented below it.
    Element {
        blocks: Vec<Block>,
        children: Vec<Item>,
    },
    /// An outline group: items OneNote keeps together at their own indentation.
    Group { items: Vec<Item> },
}

/// One block of an outline element.
pub enum Block {
    Paragraph(Paragraph),
    Table(Table),
    Image(Image),
    EmbeddedFile(EmbeddedFile),
    /// Ink in an outline, drawn alone; ink with no strokes has no drawing.
    Ink {
        ink: Option<Ink>,
    },
}

/// An image. Its file is the name of its content in the media directory; an image whose data is
/// missing or invalid has none.
pub struct Image {
    pub file: Option<String>,
    /// The name of the file it was inserted from, as stored; never used as a path.
    pub filename: Option<String>,
    pub alt_text: Option<String>,
    /// The text OneNote recognised in the image, as stored; the Markdown adds what of it the
    /// alt text lacks.
    pub ocr_text: Option<String>,
    /// The target the image links to.
    pub hyperlink: Option<String>,
}

/// An embedded file, its file named as an image's is.
pub struct EmbeddedFile {
    pub file: Option<String>,
    /// The name it was attached under, as stored; never used as a path.
    pub filename: Option<String>,
}

pub struct Paragraph {
    /// The paragraph style's id as stored: "p", "h1" to "h6", "PageTitle", "Quote", "code"...
    pub style: Option<String>,
    /// The list marker of the outline element, on its first paragraph.
    pub list: Option<List>,
    pub tags: Vec<Tag>,
    /// The visible runs, in order; hidden runs are left out.
    pub runs: Vec<Run>,
    /// The paragraph's handwriting, its inks laid out as they read in one drawing. The parser
    /// gives such a paragraph no text, so it has no runs.
    pub ink: Option<Ink>,
}

/// A list marker: a bullet, or a number.
pub struct List {
    pub numbered: bool,
    /// The number the list restarts at.
    pub restart: Option<i32>,
}

/// A note tag.
pub struct Tag {
    pub label: Option<String>,
    /// Whether it is a To Do, a task: its icon is a check box.
    pub task: bool,
    /// Whether its action item is completed.
    pub completed: bool,
}

/// A table: its rows of cells, in order.
pub struct Table {
    pub tags: Vec<Tag>,
    pub rows: Vec<Vec<Cell>>,
}

/// A table cell: its outline elements, as an outline's items.
pub struct Cell {
    pub items: Vec<Item>,
}

/// A run of text with one formatting, or an equation. Bold, italic and strikethrough are the
/// formatting the Markdown marks; the rest is plain text, and isn't read.
///
/// An equation, a paragraph's consecutive math-formatted runs, is one run of its own: its
/// `math` and its `latex`, with no text, flags or link. Its stored text is RichEdit's linear
/// format, its markers and math alphanumerics meaningful only as the LaTeX reads them.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct Run {
    pub text: String,
    /// An equation's mode: "display" when it is its paragraph's only content, else "inline".
    pub math: Option<&'static str>,
    /// An equation's LaTeX, bare, without `$` or `\(` around it.
    pub latex: Option<String>,
    pub bold: bool,
    pub italic: bool,
    pub strikethrough: bool,
    /// The link's target.
    pub link: Option<String>,
}
