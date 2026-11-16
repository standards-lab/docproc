//! A section: its section line and its page lines, read through onenote_parser at each page's
//! latest revision, the only one the parser reads.

use std::fs::File;
use std::io::Write;
use std::os::unix::ffi::OsStrExt;
use std::path::Path;

use docproc_envelope::{Fault, Lines, Media, guarded};
use onenote_parser::Parser;
use onenote_parser::page::Page;
use onenote_parser::property::common::Color;
use typed_path::{PathType, TypedPath};

use crate::content;
use crate::encryption;
use crate::markdown;
use crate::model::{self, SCHEMA};
use crate::{notebook, warn};

/// Reads a OneStore input: a section's lines, or, when the parser finds a table of contents,
/// the notebook's, unless `file_only` refuses it. The parser reads a table of contents' header
/// and type alone before it fails, so a refused one has had no section read. A section that
/// fails and holds an encryption key is refused as encrypted, else with the parser's reason. The
/// media directory is created once the section is read, before its first line.
pub fn read(
    path: &Path,
    file_only: bool,
    media: &mut Media,
    out: &mut Lines<impl Write>,
) -> Result<(), Fault> {
    let parser = Parser::new();
    let section = match guarded(|| parser.parse_section(typed(path))) {
        Ok(section) => section,
        Err(failure) if !failure.panicked && is_toc(&failure.reason) => {
            if file_only {
                return Err(Fault::Refused(crate::TOC_REFUSED.to_owned()));
            }
            return notebook::read(path, media, out);
        }
        Err(failure) => return Err(Fault::Refused(failed(path, failure.reason))),
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
    let place = format!("{place}, page {id}");
    let read = content::page(page, media, &place)?;
    Ok(model::Page {
        line: "page",
        section: section.to_owned(),
        level: page.level(),
        title: text(page.title_text()),
        creation_time: creation_time(page.created_time(), &place),
        modification_time: iso8601(page.updated_time()),
        author: text(page.author()),
        markdown: markdown::page(&read.page),
        media: read.media,
        ink_alternatives: read.ink_alternatives,
        id,
    })
}

/// The parser's time for a creation time it couldn't read: the Unix epoch, which it gives with
/// a warning of its own.
const UNREAD_TIME: time::UtcDateTime = time::UtcDateTime::UNIX_EPOCH;

/// A page's creation time; none when it is the parser's fallback for one it couldn't read, which
/// the page's `place` is warned of.
fn creation_time(time: time::UtcDateTime, place: &str) -> Option<String> {
    if time == UNREAD_TIME {
        warn(place, "its creation time can't be read; it is left out");
        return None;
    }
    Some(iso8601(time))
}

/// A section's place in a warning: its path from the notebook's root, "/Group/Section". The
/// warning writer escapes it, so a name holding a line break or `": "` keeps the warning one line
/// that splits where docproc splits it.
pub fn place(path: &[String]) -> String {
    path.iter().map(|name| format!("/{name}")).collect()
}

/// The reason a section at `path` that failed to parse with `reason` is refused or skipped with:
/// the encrypted section's, when the file holds 2.5.19's key, else the parser's own. A file the
/// scan can't read keeps the parser's reason.
fn failed(path: &Path, reason: String) -> String {
    match File::open(path).and_then(encryption::holds_key) {
        Ok(true) => encryption::REASON.to_owned(),
        Ok(false) | Err(_) => reason,
    }
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

    /// The parser's 1970 fallback is no creation time; any other time is one, the epoch's
    /// next second among them.
    #[test]
    fn the_parsers_fallback_is_no_creation_time() {
        assert_eq!(
            creation_time(utc_datetime!(1970-01-01 0:00), "/Section, page 1"),
            None
        );
        assert_eq!(
            creation_time(utc_datetime!(1970-01-01 0:00:01), "/Section, page 1"),
            Some("1970-01-01T00:00:01Z".to_owned())
        );
    }

    #[test]
    fn page_guids_take_file_identity_form() {
        assert_eq!(
            guid("{5EAF76B1-8DDE-8745-A8A7-1A9865CC478A}"),
            "5eaf76b1-8dde-8745-a8a7-1a9865cc478a"
        );
    }
}
