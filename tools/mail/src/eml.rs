//! The .eml reader: one RFC 5322 message, parsed whole by mail-parser and mapped by
//! [`crate::mime`], then a skipped line for each of its attachments that can't be read. The
//! input has passed the header-shape check before it gets here, a leading byte-order mark
//! skipped, as it is here; one that mail-parser still can't parse, or one of 4 GiB or more,
//! which it never parses, is refused. The parse and the mapping run inside the record guard, so
//! a panic in either, the Markdown's derivation included, is the message's skipped line, after
//! the store line, with a warning.

use std::convert::Infallible;
use std::io::Write;
use std::path::Path;

use docproc_envelope::{Fault, Lines, Media, guarded};

use crate::model::{Skipped, Store};
use crate::{mime, sniff, unreadable, warn, warn_below};

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
    let raw = sniff::without_bom(&raw);
    let parser = mime::parser();
    let parsed = match guarded(|| Ok::<_, Infallible>(parser.parse(raw))) {
        Ok(Some(parsed)) => Ok(parsed),
        Ok(None) => return Err(Fault::Refused("the message can't be parsed".to_owned())),
        Err(failure) => Err(failure.reason),
    };
    files.create()?;
    out.write(&Store::new("eml")).map_err(Fault::Output)?;
    let read = match parsed {
        Ok(parsed) => mime::guarded_message(&parsed.root(), files)?,
        Err(reason) => Err(reason),
    };
    let read = match read {
        Ok(read) => read,
        Err(reason) => {
            warn("message", &format!("skipped: {reason}"));
            return out
                .write(&Skipped::new("message", reason))
                .map_err(Fault::Output);
        }
    };
    warn_below("message", read.notes);
    out.write(&read.message).map_err(Fault::Output)?;
    for (id, reason) in read.skipped {
        out.write(&Skipped {
            id: Some(id),
            ..Skipped::new("attachment", reason)
        })
        .map_err(Fault::Output)?;
    }
    Ok(())
}
