//! A section: its section line and its page lines, read through onenote_parser at each page's
//! latest revision, the only one the parser reads.

use std::io::Write;
use std::os::unix::ffi::OsStrExt;
use std::path::Path;

use docproc_envelope::{Fault, Lines, Media, guarded};
use onenote_parser::Parser;
use onenote_parser::page::Page;
use onenote_parser::property::common::Color;
use typed_path::{PathType, TypedPath};

use crate::content;
use crate::markdown;
use crate::model::{self, SCHEMA};
use crate::{notebook, warn};

/// Reads a OneStore input: a section's lines, or, when the parser finds a table of contents,
/// the notebook's. The media directory is created once the section is read, before its first
/// line.
pub fn read(path: &Path, media: &mut Media, out: &mut Lines<impl Write>) -> Result<(), Fault> {
    let parser = Parser::new();
    let section = match guarded(|| parser.parse_section(typed(path))) {
        Ok(section) => section,
        Err(failure) if !failure.panicked && is_toc(&failure.reason) => {
            return notebook::read(path, media, out);
        }
        Err(failure) => {
            return Err(Fault::Refused(format!(
                "{}: {}",
                path.display(),
                failure.reason
            )));
        }
    };
    media.create()?;
    let stem = path.file_stem().unwrap_or_default();
    out.write(&model::Notebook {
        line: "notebook",
        schema: SCHEMA,
        input: "section",
        name: stem.to_string_lossy().into_owned(),
        id: None,
        color: None,
    })
    .map_err(Fault::Output)?;
    write(&section, None, &[], media, out)
}

/// Writes a section's line, under the group `parent` at the names `above`, then its pages in
/// the order of its page series, each page's media stored before its line. The warnings of the
/// section's report go to stderr first.
pub fn write(
    section: &onenote_parser::section::Section,
    parent: Option<&str>,
    above: &[String],
    media: &mut Media,
    out: &mut Lines<impl Write>,
) -> Result<(), Fault> {
    let id = section.file_identity().to_string();
    let mut path = above.to_vec();
    path.push(section.display_name().to_owned());
    let place = place(&path);
    for warning in section.report().warnings() {
        match warning.page() {
            Some((page, _)) => warn(&format!("{place}, page {page}"), warning.message()),
            None => warn(&place, warning.message()),
        }
    }
    out.write(&model::Section {
        line: "section",
        id: id.clone(),
        parent: parent.map(str::to_owned),
        path,
        display_name: section.display_name().to_owned(),
        color: section.color().and_then(hex),
    })
    .map_err(Fault::Output)?;
    for page in section
        .page_series()
        .iter()
        .flat_map(|series| series.pages())
    {
        let line = page_line(page, &id, &place, media)?;
        out.write(&line).map_err(Fault::Output)?;
    }
    Ok(())
}

/// A page's line, its media stored first. `place` names its section in warnings.
fn page_line(
    page: &Page,
    section: &str,
    place: &str,
    media: &mut Media,
) -> Result<model::Page, Fault> {
    let id = guid(page.link_target_id());
    let read = content::page(page, media, &format!("{place}, page {id}"))?;
    let mut line = model::Page {
        line: "page",
        id,
        section: section.to_owned(),
        level: page.level(),
        title: text(page.title_text()),
        creation_time: iso8601(page.created_time()),
        modification_time: iso8601(page.updated_time()),
        author: text(page.author()),
        content: read.content,
        ink: read.ink,
        ink_text: ink_text(page),
        markdown: String::new(),
    };
    line.markdown = markdown::page(&line);
    Ok(line)
}

/// A page's recognised ink text: its recognition's lines of words, each word its best guess,
/// its other guesses and its language. A word with no guess is left out.
fn ink_text(page: &Page) -> Vec<Vec<model::Word>> {
    let Some(recognition) = page.ink_recognition() else {
        return Vec::new();
    };
    recognition
        .lines()
        .iter()
        .map(|line| {
            line.words()
                .iter()
                .filter_map(|word| {
                    let (best, rest) = word.alternatives().split_first()?;
                    Some(model::Word {
                        text: best.clone(),
                        alternatives: rest.to_vec(),
                        language: word.language_id(),
                    })
                })
                .collect::<Vec<_>>()
        })
        .filter(|words| !words.is_empty())
        .collect()
}

/// A section's place in a warning: its path from the notebook's root, "/Group/Section". The
/// warning writer escapes it, so a name holding a line break or `": "` keeps the warning one line
/// that splits where docproc splits it.
pub fn place(path: &[String]) -> String {
    path.iter().map(|name| format!("/{name}")).collect()
}

/// Whether the parser refused a section because its store is a table of contents, by its error's
/// reason. Its error kind isn't public, so this reads ErrorKind::NotASectionFile's message.
fn is_toc(reason: &str) -> bool {
    reason.starts_with("Not a section file: ")
}

/// A stored string, or none when it is empty.
pub fn text(value: Option<&str>) -> Option<String> {
    value.filter(|text| !text.is_empty()).map(str::to_owned)
}

/// A host path as onenote_parser takes it, from its bytes, so a path that isn't UTF-8 reads.
pub fn typed(path: &Path) -> TypedPath<'_> {
    TypedPath::new(path.as_os_str().as_bytes(), PathType::Unix)
}

/// A page GUID as the parser gives it, "{5EAF76B1-8DDE-...}", in FileIdentity's form:
/// lowercase and hyphenated, without braces.
fn guid(stored: &str) -> String {
    stored
        .trim_start_matches('{')
        .trim_end_matches('}')
        .to_ascii_lowercase()
}

/// A colour as "#rrggbb". The parser turns MS-ONE 2.2.7's stored top byte into an alpha of
/// 255 less it, so an alpha of 0 is the stored 0xFF of a COLORREF's "no colour", and has none.
pub fn hex(color: Color) -> Option<String> {
    (color.alpha() != 0).then(|| format!("#{:02x}{:02x}{:02x}", color.r(), color.g(), color.b()))
}

/// A time as ISO 8601 UTC, to the second; a fraction of a second is cut, not rounded.
pub fn iso8601(time: time::UtcDateTime) -> String {
    format!(
        "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}Z",
        time.year(),
        u8::from(time.month()),
        time.day(),
        time.hour(),
        time.minute(),
        time.second()
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use time::macros::utc_datetime;

    #[test]
    fn times_are_cut_to_the_second() {
        assert_eq!(
            iso8601(utc_datetime!(2020-11-02 13:45:49.897)),
            "2020-11-02T13:45:49Z"
        );
    }

    /// A section named with `": "` and a message holding a line break make one warning line, the
    /// name's colon escaped so the line splits at its first three `": "` as docproc splits it.
    #[test]
    fn a_warning_at_a_place_is_one_line_that_splits_at_its_where() {
        let path = ["Group".to_owned(), "Notes: 2024".to_owned()];
        let line = docproc_envelope::stderr::warning_line(
            crate::NAME,
            &format!("{}, page 1", place(&path)),
            "the first line\nthe second",
        );
        assert_eq!(
            line,
            "docproc-onenote: warning: /Group/Notes:\\ 2024, page 1: the first line\\nthe second\n"
        );
        let parts: Vec<&str> = line.trim_end().splitn(4, ": ").collect();
        assert_eq!(parts[2], "/Group/Notes:\\ 2024, page 1");
        assert_eq!(parts[3], "the first line\\nthe second");
    }

    #[test]
    fn page_guids_take_file_identity_form() {
        assert_eq!(
            guid("{5EAF76B1-8DDE-8745-A8A7-1A9865CC478A}"),
            "5eaf76b1-8dde-8745-a8a7-1a9865cc478a"
        );
    }
}
