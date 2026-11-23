//! The .pst reader: outlook-pst opens the store, and the walk writes the store line, then each
//! folder depth-first from the IPM subtree's root, its messages in contents-table order before
//! its subfolders, each message's attachments in its attachment table's order.
//!
//! Every record is read whole inside [`guarded`] before its line is written, so a folder,
//! message or attachment that fails, or panics, inside outlook-pst becomes a skipped line and a
//! warning, and the walk goes on. Known outlook-pst 1.2.0 faults this has to absorb:
//! - a message node without a sub-node tree fails ("Missing Sub-Node Tree on message"), as the
//!   contacts of java-libpst's dist-list.pst and passworded.pst do;
//! - opening an embedded-message attachment (method 5) locks a std Mutex the reader already
//!   holds and deadlocks, and real stores' embedded messages, as in dist-list.pst and Tika's
//!   testPST.pst, omit PidTagAttachMethod on the attachment object, so opening them fails. The
//!   attach method is read from the attachment table instead, and an attachment whose row says
//!   method 5, or gives no method, is never opened: it is listed from its row, without
//!   content, with a warning;
//! - a panic while outlook-pst holds its file's mutex poisons the mutex, and every later read
//!   fails with "Failed to lock PST file". After a panic the walk reopens the store, so the
//!   records after it are still read.
//!
//! The mapping of a message or an attachment, the Markdown's derivation included, runs inside the
//! guard too, so a panic there is that record's skipped line; the Markdown is made once the
//! message's attachments are read, so that its images link their media names. stdout is flushed
//! after each message's lines, so a run that ends early has written every message before it. An
//! attachment's content is hashed and written from the reader's own buffer, never copied.
//!
//! A hierarchy table names a folder by its node id, and a corrupt one can name a folder already
//! read, an ancestor among them, which would repeat the walk without end. The walk keeps the node
//! ids of the folders it has entered, and a hierarchy row naming one is a skipped line of kind
//! `folder`, "already read", with a warning; the `--after` pre-walk keeps its own, and passes such
//! a row over.
//!
//! With `--after <id>` the walk resumes after the message whose node id is `id`: every folder
//! line, and every skipped line of a folder or a folder's table, is still written, since a
//! consumer places messages by their folders, but a message's line, and the skipped lines of
//! the message and its attachments, are written only for the messages after the cursor's, in
//! the whole read's order. A message at or before the cursor is passed over by its node id in
//! its folder's contents table and never opened, so none of its properties, recipients,
//! attachments or warnings are read: [`Walk::message`] returns before its read. Before any
//! output, the folders are walked once for the cursor's node id in their contents tables, and an
//! id no folder holds is refused.
//!
//! The store is kept by its file's encoding, since outlook-pst opens an attachment only through the
//! concrete message type, which only the concrete store opens. outlook-pst 1.2.0 gives a table it
//! can't read, or can't find, as no table at all, and its table reader isn't public, so a lost
//! table is told from an empty one only by the folder's own count; a folder whose count is wrong
//! the other way, claiming no items or no subfolders while it has some, still loses them silently.
//! A column outlook-pst can't read, as the empty PidTagAttachFilename of libyal's store's rows is,
//! costs only that column. A message opened before the store's last reopen holds the poisoned
//! store, so it is opened again before its next attachment. A place names a folder by its node id,
//! never by its path: a display name can hold ": " or a line break, and the folder's line already
//! joins the id to its path.

use std::collections::HashSet;
use std::convert::Infallible;
use std::fs::File;
use std::io::{self, Write};
use std::path::{Path, PathBuf};
use std::rc::Rc;

use docproc_envelope::{Failure, Fault, Lines, Media, guarded};
use outlook_pst::ltp::prop_context::PropertyValue;
use outlook_pst::ltp::table_context::{TableContext, TableRowId};
use outlook_pst::messaging::attachment::{
    AnsiAttachment, Attachment, AttachmentData, UnicodeAttachment,
};
use outlook_pst::messaging::message::{AnsiMessage, Message as PstMessage, UnicodeMessage};
use outlook_pst::messaging::store::{AnsiStore, EntryId, Store, UnicodeStore};
use outlook_pst::ndb::node_id::NodeId;
use outlook_pst::{AnsiPstFile, UnicodePstFile};

use crate::codepage::{message_code_page, store_code_page};
use crate::props::{AF_EMBEDDED_MESSAGE, Bag, Value, integer, tag};
use crate::sniff::Encoding;
use crate::{mapping, markdown, model, warn, warn_all};

/// Reads the store at `path`, whose header names `encoding`, writes its lines to `out`, those of
/// its messages after the message with node id `after` when it is given, and its attachments'
/// contents through `files`.
pub fn read(
    path: &Path,
    encoding: Encoding,
    after: Option<u32>,
    out: &mut Lines<impl Write>,
    files: &mut Media,
) -> Result<(), Fault> {
    let pst = Pst::open(path, encoding).map_err(Fault::Refused)?;
    let root = guarded(|| {
        let entry_id = pst.store().properties().ipm_sub_tree_entry_id()?;
        read_folder(pst.store(), &entry_id)
    })
    .map_err(|failure| {
        Fault::Refused(format!("the IPM subtree can't be read: {}", failure.reason))
    })?;
    let props = bag(pst.store().properties().iter());
    let code_page = store_code_page(&props);
    let mut walk = Walk {
        path: path.to_path_buf(),
        encoding,
        pst,
        reopened: 0,
        code_page,
        after,
        entered: HashSet::new(),
        out,
        files,
    };
    if let Some(after) = after
        && !walk.holds(&root, after, &mut HashSet::new())
    {
        return Err(Fault::Refused(format!(
            "--after {after}: no message in the store has that node id"
        )));
    }
    walk.files.create()?;

    let (display_name, notes) = mapping::store_name(&props, code_page);
    warn_all("store", notes);
    walk.out
        .write(&model::Store {
            encoding: Some(encoding.name()),
            password: Some(integer(&props, tag::PST_PASSWORD).is_some_and(|crc| crc != 0)),
            display_name,
            ..model::Store::new("pst")
        })
        .map_err(Fault::Output)?;
    walk.folder(root, None, Vec::new())
}

enum Pst {
    Unicode(Rc<UnicodeStore>),
    Ansi(Rc<AnsiStore>),
}

impl Pst {
    fn open(path: &Path, encoding: Encoding) -> Result<Self, String> {
        let file = File::open(path).map_err(|err| format!("the input can't be read: {err}"))?;
        let failed = |what: &str| {
            let what = what.to_owned();
            move |failure: Failure| format!("{what}: {}", failure.reason)
        };
        let header = "the PST header can't be read";
        let root = "the store's root can't be read";
        Ok(match encoding {
            Encoding::Unicode => {
                let pst = guarded(|| UnicodePstFile::read_from(Box::new(file)))
                    .map_err(failed(header))?;
                Self::Unicode(guarded(|| UnicodeStore::read(Rc::new(pst))).map_err(failed(root))?)
            }
            Encoding::Ansi => {
                let pst =
                    guarded(|| AnsiPstFile::read_from(Box::new(file))).map_err(failed(header))?;
                Self::Ansi(guarded(|| AnsiStore::read(Rc::new(pst))).map_err(failed(root))?)
            }
        })
    }

    fn store(&self) -> &dyn Store {
        match self {
            Self::Unicode(store) => &**store,
            Self::Ansi(store) => &**store,
        }
    }

    fn open_message(&self, id: u32) -> io::Result<OpenMessage> {
        let entry_id = self.store().properties().make_entry_id(NodeId::from(id))?;
        Ok(match self {
            Self::Unicode(store) => {
                OpenMessage::Unicode(UnicodeMessage::read(store.clone(), &entry_id, None)?)
            }
            Self::Ansi(store) => {
                OpenMessage::Ansi(AnsiMessage::read(store.clone(), &entry_id, None)?)
            }
        })
    }
}

enum OpenMessage {
    Unicode(Rc<UnicodeMessage>),
    Ansi(Rc<AnsiMessage>),
}

impl OpenMessage {
    fn message(&self) -> &dyn PstMessage {
        match self {
            Self::Unicode(message) => &**message,
            Self::Ansi(message) => &**message,
        }
    }

    fn attachment(&self, id: u32) -> io::Result<Rc<dyn Attachment>> {
        let id = NodeId::from(id);
        Ok(match self {
            Self::Unicode(message) => UnicodeAttachment::read(message.clone(), id, None)?,
            Self::Ansi(message) => AnsiAttachment::read(message.clone(), id, None)?,
        })
    }
}

struct FolderData {
    id: u32,
    props: Bag,
    messages: Vec<u32>,
    subfolders: Vec<u32>,
    lost_contents: Option<String>,
    lost_hierarchy: Option<String>,
}

fn read_folder(store: &dyn Store, entry_id: &EntryId) -> io::Result<FolderData> {
    let folder = store.open_folder(entry_id)?;
    let props = bag(folder.properties().iter());
    let contents = folder.contents_table();
    let hierarchy = folder.hierarchy_table();
    let lost_contents = (contents.is_none()
        && integer(&props, tag::CONTENT_COUNT).is_some_and(|count| count > 0))
    .then(|| lost("contents", "PidTagContentCount gives it items"));
    let lost_hierarchy = (hierarchy.is_none()
        && matches!(props.get(&tag::SUBFOLDERS), Some(Value::Boolean(true))))
    .then(|| lost("hierarchy", "PidTagSubfolders says it has subfolders"));
    Ok(FolderData {
        id: u32::from(folder.properties().node_id()),
        messages: row_ids(contents),
        subfolders: row_ids(hierarchy),
        props,
        lost_contents,
        lost_hierarchy,
    })
}

fn lost(table: &str, count: &str) -> String {
    format!("the {table} table can't be read, though {count} (outlook-pst 1.2.0 gives no reason)")
}

fn row_ids(table: Option<&Rc<dyn TableContext>>) -> Vec<u32> {
    table
        .map(|table| table.rows_matrix().map(|row| u32::from(row.id())).collect())
        .unwrap_or_default()
}

struct MessageData {
    message: OpenMessage,
    props: Bag,
    recipients: Vec<Bag>,
    attachments: Vec<u32>,
}

fn read_message(pst: &Pst, id: u32) -> io::Result<MessageData> {
    let message = pst.open_message(id)?;
    let inner = message.message();
    Ok(MessageData {
        props: bag(inner.properties().iter()),
        recipients: recipients(inner)?,
        attachments: row_ids(inner.attachment_table()),
        message,
    })
}

fn recipients(message: &dyn PstMessage) -> io::Result<Vec<Bag>> {
    let Some(table) = message.recipient_table() else {
        return Ok(Vec::new());
    };
    let context = table.context();
    table
        .rows_matrix()
        .map(|row| {
            let mut bag = Bag::new();
            for (column, value) in context.columns().iter().zip(row.columns(context)?) {
                if let Some(value) = value {
                    let value = table.read_column(&value, column.prop_type())?;
                    bag.insert(column.prop_id(), convert(&value));
                }
            }
            Ok(bag)
        })
        .collect()
}

struct AttachmentRead {
    props: Bag,
    opened: Option<Rc<dyn Attachment>>,
    notes: Vec<String>,
}

impl AttachmentRead {
    fn content(&self) -> Option<&[u8]> {
        match self.opened.as_ref()?.data() {
            Some(AttachmentData::Binary(data)) => Some(data.buffer()),
            _ => None,
        }
    }
}

fn read_attachment(message: &OpenMessage, id: u32) -> io::Result<AttachmentRead> {
    let table = message
        .message()
        .attachment_table()
        .ok_or_else(|| io::Error::other("the message has no attachment table"))?;
    let context = table.context();
    let row = table.find_row(TableRowId::new(id))?;
    let mut props = Bag::new();
    let mut notes = Vec::new();
    for (column, value) in context.columns().iter().zip(row.columns(context)?) {
        let Some(value) = value else { continue };
        match table.read_column(&value, column.prop_type()) {
            Ok(value) => {
                props.insert(column.prop_id(), convert(&value));
            }
            Err(err) => notes.push(format!(
                "attachment table column 0x{:04X} can't be read ({err}); left out",
                column.prop_id()
            )),
        }
    }
    if integer(&props, tag::ATTACH_METHOD).is_none_or(|method| method == AF_EMBEDDED_MESSAGE) {
        return Ok(AttachmentRead {
            props,
            opened: None,
            notes,
        });
    }
    let attachment = message.attachment(id)?;
    let props = attachment
        .properties()
        .iter()
        .filter(|(id, _)| **id != tag::ATTACH_DATA)
        .map(|(id, value)| (*id, convert(value)))
        .collect();
    Ok(AttachmentRead {
        props,
        opened: Some(attachment),
        notes: Vec::new(),
    })
}

struct Walk<'a, W: Write> {
    path: PathBuf,
    encoding: Encoding,
    pst: Pst,
    reopened: u32,
    code_page: u16,
    after: Option<u32>,
    entered: HashSet<u32>,
    out: &'a mut Lines<W>,
    files: &'a mut Media,
}

impl<W: Write> Walk<'_, W> {
    fn folder(
        &mut self,
        data: FolderData,
        parent: Option<u32>,
        mut path: Vec<String>,
    ) -> Result<(), Fault> {
        self.entered.insert(data.id);
        let (display_name, container_class, notes) = mapping::folder(&data.props, self.code_page);
        path.push(display_name.unwrap_or_default());
        let place = format!("folder {}", data.id);
        warn_all(&place, notes);
        self.out
            .write(&model::Folder {
                line: "folder",
                id: data.id,
                parent,
                path: path.clone(),
                container_class,
            })
            .map_err(Fault::Output)?;

        if let Some(reason) = data.lost_contents {
            warn(
                &format!("{place}, contents table"),
                &format!("skipped: {reason}"),
            );
            self.skipped("contents", data.id, None, None, reason)?;
        }
        for id in data.messages {
            self.message(data.id, id, &place)?;
        }
        if let Some(reason) = data.lost_hierarchy {
            warn(
                &format!("{place}, hierarchy table"),
                &format!("skipped: {reason}"),
            );
            self.skipped("hierarchy", data.id, None, None, reason)?;
        }
        for id in data.subfolders {
            if self.entered.contains(&id) {
                warn(&format!("{place}, folder {id}"), "skipped: already read");
                self.skipped("folder", data.id, None, Some(id), "already read".to_owned())?;
                continue;
            }
            match self.subfolder(id) {
                Ok(sub) => self.folder(sub, Some(data.id), path.clone())?,
                Err(reason) => {
                    warn(
                        &format!("{place}, folder {id}"),
                        &format!("skipped: {reason}"),
                    );
                    self.skipped("folder", data.id, None, Some(id), reason)?;
                }
            }
        }
        Ok(())
    }

    fn holds(&mut self, data: &FolderData, id: u32, entered: &mut HashSet<u32>) -> bool {
        entered.insert(data.id);
        if data.messages.contains(&id) {
            return true;
        }
        data.subfolders.iter().any(|&sub| {
            !entered.contains(&sub)
                && self
                    .subfolder(sub)
                    .is_ok_and(|sub| self.holds(&sub, id, entered))
        })
    }

    fn subfolder(&mut self, id: u32) -> Result<FolderData, String> {
        self.read(|pst| {
            let entry_id = pst.store().properties().make_entry_id(NodeId::from(id))?;
            read_folder(pst.store(), &entry_id)
        })
    }

    fn message(&mut self, folder: u32, id: u32, place: &str) -> Result<(), Fault> {
        if let Some(after) = self.after {
            if id == after {
                self.after = None;
            }
            return Ok(());
        }
        let place = format!("{place}, message {id}");
        let data = match self.read(|pst| read_message(pst, id)) {
            Ok(data) => data,
            Err(reason) => {
                warn(&place, &format!("skipped: {reason}"));
                self.skipped("message", folder, None, Some(id), reason)?;
                return self.out.flush().map_err(Fault::Output);
            }
        };
        let code_page = message_code_page(&data.props, self.code_page);
        let mapped = guarded(|| {
            Ok::<_, Infallible>(mapping::message(&data.props, &data.recipients, code_page))
        });
        let (mut message, bodies, notes) = match mapped {
            Ok(mapped) => mapped,
            Err(failure) => {
                warn(&place, &format!("skipped: {}", failure.reason));
                self.skipped("message", folder, None, Some(id), failure.reason)?;
                return self.out.flush().map_err(Fault::Output);
            }
        };
        message.id = Some(id);
        message.folder = Some(folder);
        warn_all(&place, notes);

        let mut open = data.message;
        let mut opened_in = self.reopened;
        let mut skipped = Vec::new();
        let mut locations = Vec::new();
        for attachment in data.attachments {
            let here = format!("{place}, attachment {attachment}");
            let reopened = self.reopened;
            let read = self.read(|pst| {
                if opened_in != reopened {
                    open = pst.open_message(id)?;
                    opened_in = reopened;
                }
                read_attachment(&open, attachment)
            });
            let mut read = match read {
                Ok(read) => read,
                Err(reason) => {
                    warn(&here, &format!("skipped: {reason}"));
                    skipped.push((attachment, reason));
                    continue;
                }
            };
            warn_all(&here, std::mem::take(&mut read.notes));
            if read.opened.is_none() {
                warn(&here, not_opened(&read.props));
            }
            let mapped = guarded(|| {
                Ok::<_, Infallible>(mapping::attachment(&read.props, read.content(), code_page))
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
                    skipped.push((attachment, failure.reason));
                    continue;
                }
            };
            warn_all(&here, notes);
            if let Some(content) = read.content() {
                record.file = Some(self.files.store_bytes(content, extension.as_deref())?);
            }
            message.attachments.push(record);
            locations.push(location);
        }
        let files = &mut *self.files;
        let made = guarded(|| {
            let mut notes = Vec::new();
            let made = markdown::make(&mut message, &bodies, &locations, files, &mut notes);
            Ok::<_, Infallible>(made.map(|()| notes))
        });
        match made {
            Ok(made) => warn_all(&place, made?),
            Err(failure) => {
                warn(&place, &format!("skipped: {}", failure.reason));
                self.skipped("message", folder, None, Some(id), failure.reason)?;
                return self.out.flush().map_err(Fault::Output);
            }
        }
        self.out.write(&message).map_err(Fault::Output)?;
        for (attachment, reason) in skipped {
            self.skipped("attachment", folder, Some(id), Some(attachment), reason)?;
        }
        self.out.flush().map_err(Fault::Output)
    }

    fn read<T>(&mut self, read: impl FnOnce(&Pst) -> io::Result<T>) -> Result<T, String> {
        let pst = &self.pst;
        guarded(|| read(pst)).map_err(|failure| {
            if failure.panicked {
                self.reopen();
            }
            failure.reason
        })
    }

    fn reopen(&mut self) {
        match Pst::open(&self.path, self.encoding) {
            Ok(pst) => {
                self.pst = pst;
                self.reopened += 1;
            }
            Err(reason) => warn(
                "store",
                &format!(
                    "can't be reopened after a reader panic ({reason}); \
                     the records after it may be skipped"
                ),
            ),
        }
    }

    fn skipped(
        &mut self,
        kind: &'static str,
        folder: u32,
        message: Option<u32>,
        id: Option<u32>,
        reason: String,
    ) -> Result<(), Fault> {
        self.out
            .write(&model::Skipped {
                folder: Some(folder),
                message,
                id,
                ..model::Skipped::new(kind, reason)
            })
            .map_err(Fault::Output)
    }
}

fn not_opened(row: &Bag) -> &'static str {
    if integer(row, tag::ATTACH_METHOD) == Some(AF_EMBEDDED_MESSAGE) {
        "an embedded message (attach method 5), listed without its content: \
         outlook-pst 1.2.0 can't open one"
    } else {
        "the attachment table gives no attach method; listed without its content, \
         never opened, as it may be an embedded message"
    }
}

fn bag<'a>(props: impl Iterator<Item = (&'a u16, &'a PropertyValue)>) -> Bag {
    props.map(|(id, value)| (*id, convert(value))).collect()
}

fn convert(value: &PropertyValue) -> Value {
    match value {
        PropertyValue::Integer16(value) => Value::Integer(i64::from(*value)),
        PropertyValue::Integer32(value) => Value::Integer(i64::from(*value)),
        PropertyValue::Integer64(value) => Value::Integer(*value),
        PropertyValue::Boolean(value) => Value::Boolean(*value),
        PropertyValue::Time(value) => Value::Time(*value),
        PropertyValue::Binary(value) => Value::Binary(value.buffer().to_vec()),
        PropertyValue::String8(value) => Value::String8(value.buffer().to_vec()),
        PropertyValue::Unicode(value) => Value::Unicode(utf16le(value.buffer())),
        _ => Value::Other,
    }
}

fn utf16le(units: &[u16]) -> Vec<u8> {
    units.iter().flat_map(|unit| unit.to_le_bytes()).collect()
}
