//! The lines of schema "2". Each line is one JSON object whose "line" field names it: a notebook
//! line first, then its section group and section lines depth-first, each section followed by
//! its page lines, and a skipped line for each section the read left out. Fields serialize in
//! declaration order, and a field with no value, or an empty one, is left out, never written as
//! null.
//!
//! A page's content is not a line's: its text is in the page's `markdown`, rendered from the
//! crate's own reading of it (src/page.rs), and its media are the page's flattened `media`.

use serde::Serialize;

/// The schema every line of this output follows.
pub const SCHEMA: &str = "2";

/// The first line: what the input is.
#[derive(Serialize)]
pub struct Notebook {
    pub line: &'static str,
    pub schema: &'static str,
    /// "section", "notebook" or "package".
    pub input: &'static str,
    /// The notebook directory's name, the package's, or the section file's stem.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub name: String,
    /// For a notebook or a package, its .onetoc2's FileIdentity.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub id: Option<String>,
    /// For a notebook or a package, its colour.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub color: Option<String>,
}

/// A section group of a notebook, written before the entries it holds. Its name, the group's
/// directory name, is its path's last.
#[derive(Serialize)]
pub struct SectionGroup {
    pub line: &'static str,
    /// The FileIdentity of the group's own .onetoc2.
    pub id: String,
    /// The id of the group this one is in; a group at the notebook's root has none.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub parent: Option<String>,
    /// The display names from the notebook's root down to this group.
    pub path: Vec<String>,
}

/// A record the read left out.
#[derive(Serialize)]
pub struct Skipped {
    pub line: &'static str,
    /// What was left out: "section".
    pub kind: &'static str,
    /// Its name as the notebook's table of contents gives it: a section's file name.
    pub name: String,
    /// Why, in the parser's words, or the encrypted section's reason (src/encryption.rs).
    pub reason: String,
    /// Whether the section is encrypted; written only when it is.
    #[serde(skip_serializing_if = "is_false")]
    pub encrypted: bool,
}

/// One section, written before its pages. Its display name is its path's last.
#[derive(Serialize)]
pub struct Section {
    pub line: &'static str,
    /// The section's FileIdentity.
    pub id: String,
    /// The id of the section group the section is in; a section at the notebook's root, or a
    /// section input, has none.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub parent: Option<String>,
    /// The display names from the notebook's root down to this section.
    pub path: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub color: Option<String>,
}

/// One page, at its latest revision.
#[derive(Serialize)]
pub struct Page {
    pub line: &'static str,
    /// The page's GUID.
    pub id: String,
    /// The id of the section the page is in.
    pub section: String,
    /// The page's level in its section's page tree: 1 for a page, 2 for its subpage, and so on.
    pub level: i32,
    /// The page's title; an untitled page's is its first line, as the parser gives it.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub title: Option<String>,
    /// ISO 8601 UTC, to the second, as is the modification time. None when the parser couldn't
    /// read it and gave its 1970 fallback in its place.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub creation_time: Option<String>,
    /// As the parser gives it: a page with no stored modification time has its creation time.
    pub modification_time: String,
    /// The author, as stored.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub author: Option<String>,
    /// The page as Markdown (src/markdown.rs); none when nothing on the page renders.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub markdown: String,
    /// The page's images, embedded files and ink drawings, in the order the reader visits
    /// them, which is not the Markdown's: the reader goes in stored order, the Markdown by place.
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub media: Vec<Medium>,
    /// The recognised ink words that have other guesses, each with them.
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub ink_alternatives: Vec<InkAlternatives>,
}

/// One of a page's media: an image, an embedded file, or an ink drawing. Its file is the name of
/// its content in the media directory; a record whose data is missing or invalid has none, and
/// keeps its other fields.
#[derive(Serialize, Debug, PartialEq)]
pub struct Medium {
    /// "image", "embedded_file" or "ink".
    pub kind: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub file: Option<String>,
    /// The name it was inserted or attached under, as stored; never used as a path.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub filename: Option<String>,
    /// In bytes, when its data is there.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub size: Option<u64>,
    /// An image's own stored extension, without its leading dot.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub extension: Option<String>,
    /// An embedded file's "audio" or "video", for a recording; other files have none.
    #[serde(rename = "type", skip_serializing_if = "Option::is_none")]
    pub file_type: Option<&'static str>,
}

/// A recognised ink word's other guesses.
#[derive(Serialize)]
pub struct InkAlternatives {
    /// The word's index among the page's recognised words, from 0, over its recognition's lines
    /// in order: the words the Markdown's ink alt texts hold.
    pub word: usize,
    /// Its other guesses, best first; its best is the word itself.
    pub alternatives: Vec<String>,
}

fn is_false(flag: &bool) -> bool {
    !flag
}
