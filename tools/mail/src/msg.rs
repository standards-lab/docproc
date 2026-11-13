//! The .msg reader: the CLI's own MS-OXMSG reader over cfb. A .msg is a compound file whose
//! storages hold one message (MS-OXMSG 2.2):
//! - a `__properties_version1.0` stream of the message's, a recipient's or an attachment's
//!   properties: a header, 32 bytes on the top-level message, 24 on an embedded one and 8 on a
//!   recipient or an attachment, then one 16-byte entry per property, fixed-size values inline;
//! - a `__substg1.0_XXXXYYYY` stream for each variable-size value, XXXX its property id and YYYY
//!   its type;
//! - a `__recip_version1.0_#NNNNNNNN` storage per recipient and an `__attach_version1.0_#NNNNNNNN`
//!   storage per attachment, numbered in hex;
//! - in an attachment, a `__substg1.0_3701000D` storage for an embedded message, a message of
//!   its own with the embedded form of the header, or for afStorage, the stored storage.
//!
//! The named properties' mapping, `__nameid_version1.0`, is not read, since no field reads a
//! named property, and a multi-valued property is kept as [`Value::Other`], unread, since no
//! field reads one either. Properties fill the same bags the .pst reader fills and go through the one mapping, so a .msg
//! message line is what a .pst gives for the same properties.
//!
//! Every read runs inside [`guarded`]: a recipient, attachment or embedded message that can't be
//! read, or panics in cfb, is left out with a warning, and an attachment of the top-level message
//! also gets a skipped line after the message's. A panic may leave cfb's lock poisoned, so after
//! one the file is reopened. The top-level message itself is the input's root: a compound file
//! without one, or with one whose properties can't be read, is refused. The mapping, the
//! Markdown's derivation included, runs inside the guard too: a panic in a message's is that
//! message's skipped line, or for an embedded message, a warning on its attachment, and a panic
//! in an attachment's is that attachment's. A message's Markdown is made once its attachments,
//! embedded messages included, are read, so that its images link their media names.

use std::convert::Infallible;
use std::fs::File;
use std::io::{self, Cursor, Read, Write};
use std::path::{Path, PathBuf};

use cfb::CompoundFile;
use docproc_envelope::{Fault, Lines, Media, guarded};

use crate::codepage::msg_code_page;
use crate::model::{self, Message};
use crate::props::{Bag, Value, integer, tag};
use crate::{mapping, markdown, warn, warn_all};

/// The stream of a message's, a recipient's or an attachment's properties.
const PROPERTIES: &str = "__properties_version1.0";
/// The prefixes of a recipient's and an attachment's storage, before its number in hex.
const RECIPIENT: &str = "__recip_version1.0_#";
const ATTACHMENT: &str = "__attach_version1.0_#";
/// The storage of PidTagAttachDataObject: an embedded message, or afStorage's storage.
const ATTACH_OBJECT: &str = "__substg1.0_3701000D";

/// How deep embedded messages are read: an embedded message nested deeper is listed without
/// its message, with a warning. A compound file's directory can't loop (cfb refuses one that
/// does), so the cap bounds the walk's depth and its stack on a crafted file; a forwarded
/// chain in real mail is far shallower.
pub const DEPTH_MAX: usize = 16;

/// PidTagAttachMethod's afEmbeddedMessage and afStorage (MS-OXCMSG 2.2.2.9).
const AF_EMBEDDED_MESSAGE: i64 = 5;
const AF_STORAGE: i64 = 6;

/// Property types (MS-OXCDATA 2.11.1) the reader reads; any other, a multi-valued one among them,
/// is kept as [`Value::Other`].
const PT_SHORT: u16 = 0x0002;
const PT_LONG: u16 = 0x0003;
const PT_BOOLEAN: u16 = 0x000B;
const PT_I8: u16 = 0x0014;
const PT_STRING8: u16 = 0x001E;
const PT_UNICODE: u16 = 0x001F;
const PT_SYSTIME: u16 = 0x0040;
const PT_BINARY: u16 = 0x0102;

/// The header before a property stream's entries (MS-OXMSG 2.4.1).
#[derive(Clone, Copy, Debug)]
pub enum Header {
    /// The top-level message's: reserved, the next recipient and attachment ids, the recipient
    /// and attachment counts, reserved.
    TopLevel,
    /// An embedded message's: the top-level form without its last 8 reserved bytes.
    Embedded,
    /// A recipient's or an attachment's: 8 reserved bytes.
    Child,
}

impl Header {
    pub fn size(self) -> usize {
        match self {
            Self::TopLevel => 32,
            Self::Embedded => 24,
            Self::Child => 8,
        }
    }
}

/// Reads the .msg at `path`, writes its store line and its message line to `out`, and its
/// attachments' contents, nested messages' included, through `files`.
pub fn read(path: &Path, out: &mut Lines<impl Write>, files: &mut Media) -> Result<(), Fault> {
    let file = guarded(|| cfb::open(path)).map_err(|failure| {
        Fault::Refused(format!(
            "the compound file can't be read: {}",
            failure.reason
        ))
    })?;
    let root = Path::new("/");
    let message = guarded(|| Ok::<_, Infallible>(file.is_stream(root.join(PROPERTIES))));
    if !message.is_ok_and(|message| message) {
        return Err(Fault::Refused(format!(
            "a compound file, but not an Outlook message: it has no {PROPERTIES} stream"
        )));
    }
    let mut walk = Walk {
        path: path.to_path_buf(),
        file,
        files,
    };
    let (props, notes) = walk
        .read(|file| properties_at(file, root, Header::TopLevel))
        .map_err(|reason| {
            Fault::Refused(format!("the message's properties can't be read: {reason}"))
        })?;
    walk.files.create()?;
    out.write(&model::Store {
        line: "store",
        schema: model::SCHEMA,
        format: "msg",
        encoding: None,
        password: None,
        display_name: None,
    })
    .map_err(Fault::Output)?;

    let place = "message";
    warn_all(place, notes);
    let mapped = guarded(|| Ok::<_, Infallible>(walk.message(root, &props, None, 0, place)));
    let (message, skipped) = match mapped {
        Ok(mapped) => mapped?,
        Err(failure) => {
            warn(place, &format!("skipped: {}", failure.reason));
            return out
                .write(&model::Skipped {
                    line: "skipped",
                    kind: "message",
                    folder: None,
                    message: None,
                    id: None,
                    reason: failure.reason,
                })
                .map_err(Fault::Output);
        }
    };
    out.write(&message).map_err(Fault::Output)?;
    for (id, reason) in skipped {
        out.write(&model::Skipped {
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

struct Walk<'a> {
    path: PathBuf,
    file: CompoundFile<File>,
    files: &'a mut Media,
}

impl Walk<'_> {
    /// Maps the message whose storage is `storage` and whose properties are `props` to its
    /// model, its recipients and its attachments read, embedded messages recursively, and its
    /// attachments' files written. Returns it with the attachments that couldn't be read, by
    /// number and reason; each has had its warning.
    fn message(
        &mut self,
        storage: &Path,
        props: &Bag,
        parent: Option<u16>,
        depth: usize,
        place: &str,
    ) -> Result<(Message, Vec<(u32, String)>), Fault> {
        let code_page = msg_code_page(props, parent);
        let children = match self.read(|file| children(file, storage)) {
            Ok(children) => children,
            Err(reason) => {
                warn(
                    place,
                    &format!("its recipients and attachments can't be listed: {reason}"),
                );
                Children::default()
            }
        };
        warn_all(place, children.notes);

        let mut recipients = Vec::new();
        for (number, path) in children.recipients {
            let here = format!("{place}, recipient {number}");
            match self.read(|file| properties_at(file, &path, Header::Child)) {
                Ok((bag, notes)) => {
                    warn_all(&here, notes);
                    recipients.push(bag);
                }
                Err(reason) => warn(&here, &format!("skipped: {reason}")),
            }
        }
        let (mut message, bodies, notes) = mapping::message(props, &recipients, code_page);
        warn_all(place, notes);

        let mut skipped = Vec::new();
        let mut locations = Vec::new();
        for (number, path) in children.attachments {
            let here = format!("{place}, attachment {number}");
            let (mut props, notes) =
                match self.read(|file| properties_at(file, &path, Header::Child)) {
                    Ok(read) => read,
                    Err(reason) => {
                        warn(&here, &format!("skipped: {reason}"));
                        skipped.push((number, reason));
                        continue;
                    }
                };
            warn_all(&here, notes);
            // The content is kept apart from the properties, as stored: PidTagAttachDataBinary
            // for afByValue; PidTagAttachDataObject is the storage beside them.
            let mut content = match props.remove(&tag::ATTACH_DATA) {
                Some(Value::Binary(bytes)) => Some(bytes),
                _ => None,
            };
            let object = path.join(ATTACH_OBJECT);
            let method = integer(&props, tag::ATTACH_METHOD);
            let mut nested = None;
            if method == Some(AF_EMBEDDED_MESSAGE)
                || method.is_none() && self.read(|file| Ok(file.is_storage(&object))) == Ok(true)
            {
                nested = self.embedded(&object, code_page, depth + 1, &here)?;
            } else if method == Some(AF_STORAGE) {
                match self.read(|file| storage_bytes(file, &object)) {
                    Ok(bytes) => content = Some(bytes),
                    Err(reason) => warn(
                        &here,
                        &format!("its storage can't be read ({reason}); listed without it"),
                    ),
                }
            }
            let mapped = guarded(|| {
                Ok::<_, Infallible>(mapping::attachment(&props, content.as_deref(), code_page))
            });
            let mapping::Mapped {
                mut record,
                extension,
                location,
                notes,
            } = match mapped {
                Ok(mapped) => mapped,
                Err(failure) => {
                    warn(&here, &format!("skipped: {}", failure.reason));
                    skipped.push((number, failure.reason));
                    continue;
                }
            };
            warn_all(&here, notes);
            if let Some(content) = &content {
                record.file = Some(self.files.store(content.as_slice(), extension.as_deref())?);
            }
            record.message = nested.map(Box::new);
            message.attachments.push(record);
            locations.push(location);
        }
        let mut notes = Vec::new();
        markdown::make(&mut message, &bodies, &locations, self.files, &mut notes)?;
        warn_all(place, notes);
        Ok((message, skipped))
    }

    /// The message embedded in the storage `object`, at `depth`, or None, with a warning, when
    /// it is nested too deep or can't be read. What can't be read inside it is left out with a
    /// warning; it has no line of its own to be skipped from.
    fn embedded(
        &mut self,
        object: &Path,
        parent: u16,
        depth: usize,
        place: &str,
    ) -> Result<Option<Message>, Fault> {
        if depth > DEPTH_MAX {
            warn(
                place,
                &format!(
                    "an embedded message nested deeper than {DEPTH_MAX} messages; \
                     listed without it"
                ),
            );
            return Ok(None);
        }
        let (props, notes) = match self.read(|file| properties_at(file, object, Header::Embedded)) {
            Ok(read) => read,
            Err(reason) => {
                warn(
                    place,
                    &format!("the embedded message can't be read ({reason}); listed without it"),
                );
                return Ok(None);
            }
        };
        let inner = format!("{place}, message");
        warn_all(&inner, notes);
        let mapped = guarded(|| {
            Ok::<_, Infallible>(self.message(object, &props, Some(parent), depth, &inner))
        });
        let (mut message, _) = match mapped {
            Ok(mapped) => mapped?,
            Err(failure) => {
                warn(
                    place,
                    &format!(
                        "the embedded message can't be read ({}); listed without it",
                        failure.reason
                    ),
                );
                return Ok(None);
            }
        };
        message.line = "";
        Ok(Some(message))
    }

    /// Runs one read against the file inside [`guarded`]. A panic may have poisoned cfb's
    /// lock, so after one the file is reopened for the reads that follow.
    fn read<T>(
        &mut self,
        read: impl FnOnce(&mut CompoundFile<File>) -> io::Result<T>,
    ) -> Result<T, String> {
        let file = &mut self.file;
        guarded(|| read(file)).map_err(|failure| {
            if failure.panicked {
                self.reopen();
            }
            failure.reason
        })
    }

    fn reopen(&mut self) {
        match guarded(|| cfb::open(&self.path)) {
            Ok(file) => self.file = file,
            Err(failure) => warn(
                "message",
                &format!(
                    "can't be reopened after a reader panic ({}); \
                     the records after it may be skipped",
                    failure.reason
                ),
            ),
        }
    }
}

/// A storage's recipient and attachment storages, each by number, in number order.
#[derive(Default)]
struct Children {
    recipients: Vec<(u32, PathBuf)>,
    attachments: Vec<(u32, PathBuf)>,
    notes: Vec<String>,
}

fn children(file: &CompoundFile<File>, storage: &Path) -> io::Result<Children> {
    let mut children = Children::default();
    for entry in file.read_storage(storage)? {
        if !entry.is_storage() {
            continue;
        }
        let name = entry.name();
        let (list, number) = if let Some(number) = name.strip_prefix(RECIPIENT) {
            (&mut children.recipients, number)
        } else if let Some(number) = name.strip_prefix(ATTACHMENT) {
            (&mut children.attachments, number)
        } else {
            continue;
        };
        match u32::from_str_radix(number, 16) {
            Ok(number) => list.push((number, entry.path().to_path_buf())),
            Err(_) => children
                .notes
                .push(format!("storage {name} has no number in hex; left out")),
        }
    }
    children.recipients.sort_unstable();
    children.attachments.sort_unstable();
    Ok(children)
}

/// A stream's bytes. An empty stream is read from its directory entry alone: OpenMcdf, which
/// MsgKit writes through, starts one at sector 0xFFFFFFFF, where MS-CFB has ENDOFCHAIN
/// (0xFFFFFFFE), and cfb 0.16.0 fails to follow that chain.
fn read_stream(file: &mut CompoundFile<File>, path: &Path) -> io::Result<Vec<u8>> {
    if file.entry(path)?.is_empty() {
        return Ok(Vec::new());
    }
    let mut bytes = Vec::new();
    file.open_stream(path)?.read_to_end(&mut bytes)?;
    Ok(bytes)
}

/// The properties of the storage `storage`, whose property stream has `header`, with the
/// notes of the values left out.
fn properties_at(
    file: &mut CompoundFile<File>,
    storage: &Path,
    header: Header,
) -> io::Result<(Bag, Vec<String>)> {
    let bytes = read_stream(file, &storage.join(PROPERTIES))?;
    properties(&bytes, header, |name| {
        let path = storage.join(name);
        if file.is_stream(&path) {
            read_stream(file, &path).map(Some)
        } else {
            Ok(None)
        }
    })
}

/// Reads a property stream: its `header`, then its 16-byte entries, each a property tag, flags
/// and a value of 8 bytes. A fixed-size value is the value's first bytes; a variable-size one
/// is in its own stream, which `stream` reads by name, None when there is none. A value that
/// can't be read is left out with a note, and the record keeps the rest; a stream that isn't
/// a header and whole entries can't be read at all. A property given twice keeps its first
/// entry.
pub fn properties(
    bytes: &[u8],
    header: Header,
    mut stream: impl FnMut(&str) -> io::Result<Option<Vec<u8>>>,
) -> io::Result<(Bag, Vec<String>)> {
    let size = header.size();
    let Some(entries) = bytes.get(size..) else {
        return Err(invalid(format!(
            "the property stream is {} bytes, short of its {size}-byte header",
            bytes.len()
        )));
    };
    if entries.len() % 16 != 0 {
        return Err(invalid(format!(
            "the property stream's {} bytes after its header are not whole 16-byte entries",
            entries.len()
        )));
    }
    let mut bag = Bag::new();
    let mut notes = Vec::new();
    let (entries, _) = entries.as_chunks::<16>();
    for entry in entries {
        // The property tag, the flags, which the reader doesn't need, and the value.
        let [t0, t1, t2, t3, _, _, _, _, inline @ ..] = *entry;
        let tag = u32::from_le_bytes([t0, t1, t2, t3]);
        let (id, kind) = ((tag >> 16) as u16, tag as u16);
        if bag.contains_key(&id) {
            continue;
        }
        match value(id, kind, inline, &mut stream) {
            Ok(value) => {
                bag.insert(id, value);
            }
            Err(reason) => notes.push(format!("property 0x{id:04X} {reason}; left out")),
        }
    }
    Ok((bag, notes))
}

/// One property's value, or why it can't be read, worded to follow its name. A multi-valued
/// property, whose values no field reads, is [`Value::Other`], its streams unread.
fn value(
    id: u16,
    kind: u16,
    inline: [u8; 8],
    stream: &mut impl FnMut(&str) -> io::Result<Option<Vec<u8>>>,
) -> Result<Value, String> {
    if let Some(value) = fixed(kind, &inline) {
        return Ok(value);
    }
    let name = format!("__substg1.0_{id:04X}{kind:04X}");
    let mut read = |name: &str| match stream(name) {
        Ok(Some(bytes)) => Ok(bytes),
        Ok(None) => Err(format!("has no stream {name}")),
        Err(err) => Err(format!("can't be read from {name} ({err})")),
    };
    match kind {
        PT_STRING8 | PT_UNICODE | PT_BINARY => Ok(variable(kind, read(&name)?)),
        _ => Ok(Value::Other),
    }
}

/// A fixed-size value from its first bytes, when `kind` is a fixed-size type the reader reads.
fn fixed(kind: u16, bytes: &[u8]) -> Option<Value> {
    let le = |n: usize| bytes.get(..n);
    Some(match kind {
        PT_SHORT => Value::Integer(i16::from_le_bytes(le(2)?.try_into().ok()?).into()),
        PT_LONG => Value::Integer(i32::from_le_bytes(le(4)?.try_into().ok()?).into()),
        PT_I8 => Value::Integer(i64::from_le_bytes(le(8)?.try_into().ok()?)),
        PT_SYSTIME => Value::Time(i64::from_le_bytes(le(8)?.try_into().ok()?)),
        PT_BOOLEAN => Value::Boolean(u16::from_le_bytes(le(2)?.try_into().ok()?) != 0),
        _ => return None,
    })
}

/// A variable-size value from its stream. A string's stream may end in its terminating null;
/// it ends at its first.
fn variable(kind: u16, mut bytes: Vec<u8>) -> Value {
    match kind {
        PT_STRING8 => {
            if let Some(end) = bytes.iter().position(|byte| *byte == 0) {
                bytes.truncate(end);
            }
            Value::String8(bytes)
        }
        PT_UNICODE => {
            let (units, _) = bytes.as_chunks::<2>();
            let end = units
                .iter()
                .position(|unit| *unit == [0, 0])
                .map_or(bytes.len() & !1, |unit| unit * 2);
            bytes.truncate(end);
            Value::Unicode(bytes)
        }
        _ => Value::Binary(bytes),
    }
}

/// An attachment's afStorage storage as a compound file of its own, as stored: its storages
/// and streams, CLSIDs, state bits and storage times, in the source's CFB version.
fn storage_bytes(file: &mut CompoundFile<File>, storage: &Path) -> io::Result<Vec<u8>> {
    let mut copy = CompoundFile::create_with_version(file.version(), Cursor::new(Vec::new()))?;
    let root = Path::new("/");
    copy.set_storage_clsid(root, *file.entry(storage)?.clsid())?;
    let entries: Vec<cfb::Entry> = file.walk_storage(storage)?.skip(1).collect();
    for entry in entries {
        let inner = entry
            .path()
            .strip_prefix(storage)
            .map_err(io::Error::other)?;
        let path = root.join(inner);
        if entry.is_storage() {
            copy.create_storage(&path)?;
            copy.set_storage_clsid(&path, *entry.clsid())?;
            copy.set_created_time(&path, entry.created())?;
            copy.set_modified_time(&path, entry.modified())?;
        } else {
            let mut stream = copy.create_stream(&path)?;
            io::copy(&mut file.open_stream(entry.path())?, &mut stream)?;
        }
        copy.set_state_bits(&path, entry.state_bits())?;
    }
    copy.flush()?;
    Ok(copy.into_inner().into_inner())
}

fn invalid(reason: String) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, reason)
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use super::*;

    /// A property stream: `header` zero bytes, then one entry per (tag, value).
    fn stream(header: Header, entries: &[(u32, [u8; 8])]) -> Vec<u8> {
        let mut bytes = vec![0; header.size()];
        for (tag, value) in entries {
            bytes.extend_from_slice(&tag.to_le_bytes());
            bytes.extend_from_slice(&6u32.to_le_bytes());
            bytes.extend_from_slice(value);
        }
        bytes
    }

    fn inline(bytes: &[u8]) -> [u8; 8] {
        let mut value = [0; 8];
        value[..bytes.len()].copy_from_slice(bytes);
        value
    }

    fn read(
        bytes: &[u8],
        header: Header,
        streams: &[(&str, &[u8])],
    ) -> io::Result<(Bag, Vec<String>)> {
        let streams: HashMap<&str, &[u8]> = streams.iter().copied().collect();
        properties(bytes, header, |name| {
            Ok(streams.get(name).map(|bytes| bytes.to_vec()))
        })
    }

    fn utf16(text: &str) -> Vec<u8> {
        text.encode_utf16().flat_map(u16::to_le_bytes).collect()
    }

    #[test]
    fn the_header_sizes_are_ms_oxmsgs() {
        assert_eq!(Header::TopLevel.size(), 32);
        assert_eq!(Header::Embedded.size(), 24);
        assert_eq!(Header::Child.size(), 8);
        let entry = [(0x0017_0003, inline(&2u32.to_le_bytes()))];
        for header in [Header::TopLevel, Header::Embedded, Header::Child] {
            let (bag, notes) = read(&stream(header, &entry), header, &[]).unwrap();
            assert_eq!(bag, Bag::from([(0x0017, Value::Integer(2))]), "{header:?}");
            assert!(notes.is_empty());
        }
        // Read with the wrong header, the entries are misaligned.
        let embedded = stream(Header::Embedded, &entry);
        assert!(read(&embedded, Header::TopLevel, &[]).is_err());
    }

    #[test]
    fn a_stream_short_of_its_header_or_whole_entries_is_unreadable() {
        let err = read(&[0; 20], Header::TopLevel, &[]).unwrap_err();
        assert_eq!(
            err.to_string(),
            "the property stream is 20 bytes, short of its 32-byte header"
        );
        let err = read(&[0; 8 + 15], Header::Child, &[]).unwrap_err();
        assert_eq!(
            err.to_string(),
            "the property stream's 15 bytes after its header are not whole 16-byte entries"
        );
        let (bag, notes) = read(&[0; 8], Header::Child, &[]).unwrap();
        assert!(bag.is_empty() && notes.is_empty());
    }

    #[test]
    fn fixed_size_values_are_inline() {
        let bytes = stream(
            Header::Child,
            &[
                (0x0001_0002, inline(&(-2i16).to_le_bytes())),
                (0x0002_0003, inline(&(-3i32).to_le_bytes())),
                (0x0003_0014, inline(&(1i64 << 40).to_le_bytes())),
                (0x0004_000B, inline(&[1, 0])),
                (
                    0x0005_0040,
                    inline(&130_378_746_040_000_000i64.to_le_bytes()),
                ),
                (0x0006_0005, inline(&1.5f64.to_le_bytes())),
            ],
        );
        let (bag, notes) = read(&bytes, Header::Child, &[]).unwrap();
        assert_eq!(
            bag,
            Bag::from([
                (1, Value::Integer(-2)),
                (2, Value::Integer(-3)),
                (3, Value::Integer(1 << 40)),
                (4, Value::Boolean(true)),
                (5, Value::Time(130_378_746_040_000_000)),
                (6, Value::Other),
            ])
        );
        assert!(notes.is_empty());
    }

    #[test]
    fn variable_size_values_are_in_their_streams() {
        let bytes = stream(
            Header::Child,
            &[
                (0x0037_001F, inline(&12u32.to_le_bytes())),
                (0x0E1D_001E, inline(&4u32.to_le_bytes())),
                (0x1013_0102, inline(&3u32.to_le_bytes())),
                (0x3001_001F, inline(&2u32.to_le_bytes())),
            ],
        );
        let subject = [utf16("Hello"), vec![0, 0]].concat();
        let (bag, notes) = read(
            &bytes,
            Header::Child,
            &[
                ("__substg1.0_0037001F", &subject),
                ("__substg1.0_0E1D001E", b"Hi\xE9\0"),
                ("__substg1.0_10130102", b"<p>"),
            ],
        )
        .unwrap();
        assert_eq!(
            bag,
            Bag::from([
                (0x0037, Value::Unicode(utf16("Hello"))),
                (0x0E1D, Value::String8(b"Hi\xE9".to_vec())),
                (0x1013, Value::Binary(b"<p>".to_vec())),
            ])
        );
        assert_eq!(
            notes,
            ["property 0x3001 has no stream __substg1.0_3001001F; left out"]
        );
    }

    /// A multi-valued property is kept unread, as Other, its streams never opened: a missing
    /// one is no note.
    #[test]
    fn multi_valued_values_are_other() {
        let bytes = stream(
            Header::Child,
            &[
                (0x8001_1003, inline(&8u32.to_le_bytes())),
                (0x8002_101F, inline(&8u32.to_le_bytes())),
                (0x8003_1102, inline(&8u32.to_le_bytes())),
            ],
        );
        let longs = [7i32.to_le_bytes(), (-1i32).to_le_bytes()].concat();
        let (bag, notes) =
            read(&bytes, Header::Child, &[("__substg1.0_80011003", &longs)]).unwrap();
        assert_eq!(
            bag,
            Bag::from([
                (0x8001, Value::Other),
                (0x8002, Value::Other),
                (0x8003, Value::Other),
            ])
        );
        assert!(notes.is_empty());
    }

    #[test]
    fn a_property_given_twice_keeps_its_first_entry() {
        let bytes = stream(
            Header::Child,
            &[
                (0x0017_0003, inline(&1u32.to_le_bytes())),
                (0x0017_0003, inline(&2u32.to_le_bytes())),
            ],
        );
        let (bag, _) = read(&bytes, Header::Child, &[]).unwrap();
        assert_eq!(bag[&0x0017], Value::Integer(1));
    }
}
