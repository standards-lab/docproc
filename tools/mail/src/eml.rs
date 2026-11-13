//! The .eml reader: one RFC 5322 message, parsed whole by mail-parser and mapped by
//! [`crate::mime`], then a skipped line for each of its attachments that can't be read. The
//! input has passed the header-shape check before it gets here; one that mail-parser still can't
//! parse, or one of 4 GiB or more, which it never parses, is refused.

use std::io::Write;
use std::path::Path;

use docproc_envelope::{Fault, Lines, Media};

use crate::model::{self, Skipped, Store};
use crate::{mime, unreadable, warn_below};

/// The size mail-parser parses up to: its offsets are 32-bit.
const SIZE_MAX: u64 = 1 << 32;

/// Reads the .eml at `path` and writes its store line, its message line and its skipped
/// attachments' lines to `out`.
pub fn read(path: &Path, out: &mut Lines<impl Write>, files: &mut Media) -> Result<(), Fault> {
    let size = std::fs::metadata(path).map_err(unreadable)?.len();
    if size >= SIZE_MAX {
        return Err(Fault::Refused(
            "an .eml of 4 GiB or more is not read".to_owned(),
        ));
    }
    let raw = std::fs::read(path).map_err(unreadable)?;
    let Some(parsed) = mime::parser().parse(&raw) else {
        return Err(Fault::Refused("the message can't be parsed".to_owned()));
    };
    files.create()?;
    out.write(&Store {
        line: "store",
        schema: model::SCHEMA,
        format: "eml",
        encoding: None,
        password: None,
        display_name: None,
    })
    .map_err(Fault::Output)?;
    let read = mime::message(&parsed.root(), files)?;
    warn_below("message", read.notes);
    out.write(&read.message).map_err(Fault::Output)?;
    for (id, reason) in read.skipped {
        out.write(&Skipped {
            line: "skipped",
            kind: "attachment",
            folder: None,
            message: None,
            id: Some(id),
            reason,
        })
        .map_err(Fault::Output)?;
    }
    Ok(())
}
