//! The mbox reader: a store line, then each message in file order, read one at a time by
//! mail-parser's mbox iterator, so memory follows the message being read, not the file. A
//! message's id is its 1-based ordinal; no message has a folder. A message that fails the
//! header-shape check, or that mail-parser can't parse, gives a skipped line in its place, with a
//! warning, and the run goes on; a read that fails partway gives one for the message it was
//! reading, and ends there.
//!
//! The iterator reads mboxrd: a line quoted ">From ", ">>From " and so on loses one ">". An
//! mboxo is read the same way, so a body line that was ">From " to begin with loses its ">" too.
//! A message runs to the next line starting "From ", and mboxcl's Content-Length is not read, so
//! in an mboxcl or mboxcl2 an unquoted body line starting "From " starts a new message.
//!
//! An mbox is only an mbox once one of its entries is a message: the store line is held until
//! the first message parses, and the skipped lines of the entries before it with it, so a text
//! file that merely starts "From " and holds no message is refused, as any other input that is
//! no mail is, with nothing on stdout. A leading byte-order mark is skipped, before the file's
//! first separator line and before each entry's header check. The parse and the mapping run
//! inside the record guard, so a panic in either, the Markdown's derivation included, is that
//! message's skipped line, and stdout is flushed after each message's lines, so a run that ends
//! early has written every message before it.
//!
//! With `--after <n>` the read resumes after message n: the iterator scans the separators of
//! messages 1 to n from the start of the file, parsing none of them, so none of their lines,
//! warnings or attachments are written, and the store line and the lines of the messages after n
//! follow, as a whole read writes them. An n past the file's last ordinal, empty entries counted,
//! is refused before any output. The messages before n were an mbox's, so a resumed read writes
//! its store line whether or not a message after n parses.

use std::convert::Infallible;
use std::fs::File;
use std::io::{self, BufRead, BufReader, Write};
use std::path::Path;

use docproc_envelope::{Fault, Lines, Media, guarded};
use mail_parser::MessageBuffers;
use mail_parser::mailbox::mbox::{Message, MessageIterator};

use crate::model::{self, Skipped, Store};
use crate::{mime, sniff, unreadable, warn, warn_below};

/// Reads the mbox at `path` and writes its store line and the lines of its messages, those after
/// ordinal `after` when it is given, to `out`.
pub fn read(
    path: &Path,
    after: Option<u32>,
    out: &mut Lines<impl Write>,
    files: &mut Media,
) -> Result<(), Fault> {
    let mut file = BufReader::new(File::open(path).map_err(unreadable)?);
    if file.fill_buf().map_err(unreadable)?.starts_with(sniff::BOM) {
        file.consume(sniff::BOM.len());
    }
    let mut entries = (1u32..).zip(MessageIterator::new(file));
    let ended = match after {
        Some(after) => skip_to(&mut entries, after)
            .map_err(|reason| Fault::Refused(format!("--after {after}: {reason}")))?,
        None => false,
    };
    let mut held = Held {
        out,
        store: true,
        skipped: Vec::new(),
    };
    if after.is_some() {
        held.release(files)?;
    }
    if ended {
        return Ok(());
    }
    let parser = mime::parser();
    let mut buffers = MessageBuffers::new();
    for (ordinal, entry) in entries {
        let entry = match entry {
            Ok(entry) => entry,
            Err(err) => {
                held.skip(ordinal, format!("the mbox can't be read: {err}"))?;
                break;
            }
        };
        let contents = sniff::without_bom(unseparated(entry.contents()));
        // A separator line with nothing after it is no message: it writes no line and no
        // warning, but keeps its ordinal, so the messages after it keep theirs.
        if contents.is_empty() {
            continue;
        }
        if !sniff::header_shape(contents).unwrap_or(false) {
            let reason = "it has no header block with a From or Date field".to_owned();
            held.skip(ordinal, reason)?;
            continue;
        }
        let parsed =
            match guarded(|| Ok::<_, Infallible>(parser.parse_with(contents, &mut buffers))) {
                Ok(Some(parsed)) => parsed,
                Ok(None) => {
                    held.skip(ordinal, "it can't be parsed".to_owned())?;
                    continue;
                }
                Err(failure) => {
                    held.skip(ordinal, failure.reason)?;
                    continue;
                }
            };
        held.release(files)?;
        let out = &mut *held.out;
        let place = format!("message {ordinal}");
        let mut read = match mime::guarded_message(&parsed.root(), files)? {
            Ok(read) => read,
            Err(reason) => {
                skip(out, ordinal, reason)?;
                continue;
            }
        };
        read.message.id = Some(ordinal);
        warn_below(&place, read.notes);
        out.write(&read.message).map_err(Fault::Output)?;
        for (id, reason) in read.skipped {
            out.write(&Skipped {
                line: "skipped",
                kind: "attachment",
                folder: None,
                message: Some(ordinal),
                id: Some(id),
                reason,
            })
            .map_err(Fault::Output)?;
        }
        out.flush().map_err(Fault::Output)?;
        buffers = parsed.into_buffers();
    }
    if held.store {
        if held.skipped.is_empty() {
            // No entry at all, or empty ones alone: nothing failed, and the mbox is empty.
            held.release(files)?;
        } else {
            let (ordinal, reason) = held.skipped.swap_remove(0);
            return Err(Fault::Refused(format!(
                "neither a .pst, a .msg, an .eml nor an mbox: no entry of it is a message \
                 (message {ordinal}: {reason})"
            )));
        }
    }
    Ok(())
}

/// The lines a whole read holds until its first message parses: the store line, and the skipped
/// lines of the entries before that message, with their warnings.
struct Held<'a, W: Write> {
    out: &'a mut Lines<W>,
    /// Whether the store line is still held.
    store: bool,
    /// The held skipped messages, by ordinal and reason.
    skipped: Vec<(u32, String)>,
}

impl<W: Write> Held<'_, W> {
    /// Writes a message's skipped line, and its warning, or holds them behind the store line.
    fn skip(&mut self, ordinal: u32, reason: String) -> Result<(), Fault> {
        if self.store {
            self.skipped.push((ordinal, reason));
            Ok(())
        } else {
            skip(self.out, ordinal, reason)
        }
    }

    /// Writes the store line, once, and then the skipped lines held behind it: the input is an
    /// mbox.
    fn release(&mut self, files: &Media) -> Result<(), Fault> {
        if !std::mem::take(&mut self.store) {
            return Ok(());
        }
        files.create()?;
        self.out
            .write(&Store {
                line: "store",
                schema: model::SCHEMA,
                format: "mbox",
                encoding: None,
                password: None,
                display_name: None,
            })
            .map_err(Fault::Output)?;
        for (ordinal, reason) in std::mem::take(&mut self.skipped) {
            skip(self.out, ordinal, reason)?;
        }
        Ok(())
    }
}

/// Scans past messages 1 to `after`, separators only, so that the next entry is message
/// `after + 1`, and says whether the read ends there. An entry that can't be read ends a whole
/// read with its skipped line, so one before `after` leaves `after` unreachable and is refused,
/// with the reason; one at `after` is the cursor's own, and the read ends at it, its tail empty,
/// as the whole read's is.
fn skip_to(
    entries: &mut impl Iterator<Item = (u32, io::Result<Message>)>,
    after: u32,
) -> Result<bool, String> {
    let mut last = 0;
    for (ordinal, entry) in entries.by_ref() {
        last = ordinal;
        if let Err(err) = &entry
            && ordinal < after
        {
            return Err(format!(
                "message {ordinal}, before it, can't be read: {err}"
            ));
        }
        if ordinal == after {
            return Ok(entry.is_err());
        }
    }
    Err(match last {
        0 => "the mbox has no messages".to_owned(),
        last => format!("the mbox's last message is {last}"),
    })
}

/// A message's contents without the blank line that ends it, which the mbox writer adds before
/// the next separator line and after the last message, so the message is its source byte for
/// byte. Contents that don't end in a blank line are left as they are.
fn unseparated(contents: &[u8]) -> &[u8] {
    let Some(rest) = contents.strip_suffix(b"\n") else {
        return contents;
    };
    let rest = rest.strip_suffix(b"\r").unwrap_or(rest);
    if rest.is_empty() || rest.ends_with(b"\n") {
        rest
    } else {
        contents
    }
}

/// Writes a message's skipped line, and its warning, and flushes it.
fn skip(out: &mut Lines<impl Write>, ordinal: u32, reason: String) -> Result<(), Fault> {
    warn(&format!("message {ordinal}"), &format!("skipped: {reason}"));
    out.write(&Skipped {
        line: "skipped",
        kind: "message",
        folder: None,
        message: None,
        id: Some(ordinal),
        reason,
    })
    .map_err(Fault::Output)?;
    out.flush().map_err(Fault::Output)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_blank_line_ending_a_message_is_the_separators() {
        for (contents, message) in [
            (
                &b"Subject: a\r\n\r\nbody\r\n\n"[..],
                &b"Subject: a\r\n\r\nbody\r\n"[..],
            ),
            (b"Subject: a\n\nbody\n\n", b"Subject: a\n\nbody\n"),
            (
                b"Subject: a\r\n\r\nbody\r\n\r\n",
                b"Subject: a\r\n\r\nbody\r\n",
            ),
            (b"Subject: a\n\nbody\n", b"Subject: a\n\nbody\n"),
            (b"Subject: a\n\nbody", b"Subject: a\n\nbody"),
            (b"\n", b""),
            (b"\r\n", b""),
            (b"", b""),
        ] {
            assert_eq!(unseparated(contents), message, "{contents:?}");
        }
    }
}
