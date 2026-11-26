//! The mapping from a MIME message, as mail-parser parses it, to the message model,
//! shared by the .eml reader and the mbox reader. Like the MAPI mapping, it returns its notes for
//! the caller to warn about with the record's place.
//!
//! Each field is the header's: the sender is the first From mailbox, the recipients To's, Cc's
//! and Bcc's mailboxes, groups flattened, each address with address type SMTP, and the subject
//! decoded from its encoded words. The submit time is the Date header, and the delivery time the
//! date of the first Received header, the newest hop's, each in UTC. Message-ID, In-Reply-To and
//! References are kept as stored, unfolded, and the transport headers are the raw header block.
//! A MIME message has no message class, so none is written. Where a header appears more than
//! once, the last is read, as mail-parser's own getters read it.
//!
//! The threading and priority headers fill the fields a .pst or a .msg fills from MAPI:
//! Thread-Topic the conversation topic, Thread-Index the conversation index, Importance (else
//! X-Priority) the importance and Sensitivity the sensitivity, in the schema's words. A value
//! that doesn't map is left out, with a note.
//!
//! The bodies are mail-parser's body lists (RFC 8621's textBody and htmlBody): the text body is
//! the text parts of the one, and the HTML body the HTML parts of the other, each decoded from
//! its transfer encoding and charset and joined in order, and the Markdown is made from them as
//! for a .pst or a .msg, once the attachments, embedded messages included, are read; neither body
//! is part of the line. The attachments are the message's parts that aren't body text, in order:
//! its attachments, and the images, audio and video the bodies show. A part's Content-Location
//! matches an image's source to it, and is no field; its Content-Disposition is not read for
//! `inline`, which says whether the Markdown links it. A message/rfc822 or
//! message/global part is an embedded message, nested under its record to [`DEPTH_MAX`] messages.
//!
//! A TNEF part, one of type `application/ms-tnef` or `application/vnd.ms-tnef`, or named
//! `winmail.dat` compared without regard to case, is Outlook's own serialization of a message's
//! properties and attachments, read by [`tnef`]. Its attachments take its place among the
//! message's, numbered on from it, each mapped as a .msg attachment is, an embedded message
//! included; its stored bodies come ahead of the MIME message's: its HTML, else the MIME HTML, its
//! text body, else the MIME text body, and its RTF body's own text only where there is neither, so
//! a MIME text/plain part beside a TNEF part wins over the TNEF part's plain RTF. Where a message
//! has more than one, the first that reads gives the bodies. One that can't be read is a skipped
//! attachment, and the rest of the message is read.
//!
//! An opaque S/MIME part (an encrypted or opaquely signed `application/pkcs7-mime`) holds content
//! no reader here opens. It gives its message a note, so the message's Markdown, empty or
//! partial, is never read as all there is. A rights-managed message,
//! one whose Content-Class is `rpmsg.message` or that has an attachment named `message.rpmsg`,
//! each compared without regard to case, gets a note once, its content being that attachment's,
//! encrypted (MS-OXORMMS 2.2.2.1 and 2.2.3.1); it is still read, its placeholder body and the
//! attachment included.
//!
//! mail-parser decodes US-ASCII and ISO-8859-1 as windows-1252, as WHATWG labels them, and the
//! five bytes windows-1252 leaves undefined as U+FFFD. Whatever it reports as a decoding problem
//! (a malformed or unknown transfer encoding, an unknown charset, bytes not valid in the charset)
//! gives a note: the text is decoded lossily. It reports none for a header's encoded words.
//!
//! Importance is Importance's low, normal or high, and an unknown one falls back to X-Priority's
//! band, read from its leading number, as in "1 (Highest)": 1 to 2 high, 3 normal and 4 to 5 low.
//! Sensitivity's Personal, Private and Company-Confidential (RFC 2156) are personal, private and
//! confidential. The conversation index is Thread-Index base64-decoded, in lowercase hex, as a .pst
//! or a .msg writes PidTagConversationIndex. A header kept as stored is its raw bytes unfolded and
//! trimmed: a blank one is left out, and one that isn't valid UTF-8 is decoded lossily, with a
//! note. A date header with no date that reads, or one outside mail-parser's range, is left out,
//! with a note.
//!
//! A body list's parts of the other kind, the stand-ins RFC 8621 copies in for a missing version,
//! and its media are no part of a body, and its parts are joined by a line break, so that one's
//! last line never runs into the next one's first. A detached signature
//! (`application/pkcs7-signature`) is no opaque part: the content it signs is read. mail-parser
//! counts only encoded levels toward its nesting limit, so it leaves a transfer-encoded
//! message/rfc822 part unparsed only past [`DEPTH_MAX`] messages, and such a part is noted as too
//! deep. An attachment of an embedded message that can't be read has its warning, and no skipped
//! line: the embedded message has no line to give it one after.

use std::borrow::Cow;
use std::convert::Infallible;

use docproc_envelope::{Fault, Media, extension_of, guarded};
use mail_parser::{
    AddressList, DateTime, DecodeProblems, Encoding, Header, HeaderForm, HeaderName, Headers,
    MessageParser, MessagePart, MessageRef, PartKind, PartRole,
};

use crate::mapping;
use crate::markdown::{self, Bodies};
use crate::model::{Address, Attachment, Message};
use crate::msg::{DEPTH_MAX, too_deep};
use crate::props::{Value, tag};
use crate::tnef::{self, Embedded};

const SMTP: &str = "SMTP";

/// The note for a message holding an opaque S/MIME part, whose content no reader here opens:
/// a MIME message's `application/pkcs7-mime` part, or a .pst or .msg message whose class is
/// S/MIME's ([`mapping::message`]).
pub const OPAQUE_SMIME: &str = "it holds an S/MIME part (application/pkcs7-mime), encrypted or \
                                opaquely signed, whose content is not read";

/// The note for a rights-managed message, whose content is encrypted in its `message.rpmsg`
/// attachment, which no reader here opens: a MIME message's, or a .pst or .msg message's
/// ([`mapping::rights_managed`]).
pub const RIGHTS_MANAGED: &str = "it is a rights-managed message (message.rpmsg), whose \
                                  encrypted content is not read";

const RPMSG: &str = "message.rpmsg";

/// Whether an attachment's `name` is the one a rights-managed message's content is kept under,
/// compared without regard to case.
pub fn rpmsg(name: &str) -> bool {
    name.eq_ignore_ascii_case(RPMSG)
}

/// A message read: its line, its notes, each with its place below the message ("" for the
/// message itself, else ", attachment <n>" and ", message" for each level of nesting), and the
/// attachments that couldn't be read, by number and reason.
pub struct Read {
    pub message: Message,
    pub notes: Vec<(String, String)>,
    pub skipped: Vec<(u32, String)>,
}

/// The parser both readers parse with: mail-parser's own limits, but transfer-encoded
/// message/rfc822 parts parsed to [`DEPTH_MAX`] levels, as unencoded ones are, where its default
/// stops at 3. Each level decodes from its container, which base64 shrinks, so the memory a
/// message takes stays bounded by its size. Thread-Topic is parsed as unstructured text, its
/// encoded words decoded as Subject's are, where mail-parser keeps it raw.
pub fn parser() -> MessageParser {
    MessageParser::new()
        .max_encoded_nesting(DEPTH_MAX)
        .header(HeaderName::ThreadTopic, HeaderForm::Text)
}

/// [`message`] inside the record guard: a panic in the mapping, the Markdown's derivation
/// included, is the message's reason to be skipped, and the run goes on. A media fault still ends
/// the run.
pub fn guarded_message(
    parsed: &MessageRef<'_>,
    files: &mut Media,
) -> Result<Result<Read, String>, Fault> {
    match guarded(|| Ok::<_, Infallible>(message(parsed, files))) {
        Ok(read) => read.map(Ok),
        Err(failure) => Ok(Err(failure.reason)),
    }
}

/// Maps one parsed message to a message line, its attachments' contents stored through
/// `files`, which names them.
pub fn message(parsed: &MessageRef<'_>, files: &mut Media) -> Result<Read, Fault> {
    let mut notes = Vec::new();
    let (message, skipped) = read(parsed, 0, "", files, &mut notes)?;
    Ok(Read {
        message,
        notes,
        skipped,
    })
}

fn read(
    parsed: &MessageRef<'_>,
    depth: usize,
    at: &str,
    files: &mut Media,
    notes: &mut Vec<(String, String)>,
) -> Result<(Message, Vec<(u32, String)>), Fault> {
    let mut own = Vec::new();
    let mut message = headers(parsed, &mut own);
    let bodies = bodies(parsed, &mut own);
    own.extend(parsed.parts().filter_map(|part| unread(&part)));
    if rights_managed(parsed) {
        own.push(RIGHTS_MANAGED.to_owned());
    }
    notes.extend(own.into_iter().map(|note| (at.to_owned(), note)));
    let mut skipped = Vec::new();
    let mut locations = Vec::new();
    let mut stored = None;
    let mut number = 0u32;
    let parts = parsed
        .parts()
        .filter(|part| part.is_attachment() || matches!(part.role(), PartRole::MediaBody));
    for part in parts {
        number += 1;
        let here = format!("{at}, attachment {number}");
        if tnef(&part) {
            let (content, problems) = part.decoded_checked();
            let mut own = Vec::new();
            lossy_note(problems, "TNEF part's content", &mut own);
            notes.extend(own.into_iter().map(|note| (at.to_owned(), note)));
            let read = guarded(|| tnef::read(&content, depth)).and_then(|read| {
                guarded(|| Ok::<_, Infallible>(tnef_bodies(&read, None, at, notes)))
                    .map(|bodies| (read, bodies))
            });
            match read {
                Ok((read, bodies)) => {
                    number -= 1;
                    stored.get_or_insert(bodies);
                    let code_page = read.code_page(None);
                    let attached = Records {
                        message: &mut message,
                        locations: &mut locations,
                        number: &mut number,
                    };
                    tnef_attachments(&read, code_page, depth, at, attached, files, notes)?;
                }
                Err(failure) => {
                    let reason = format!("the TNEF part can't be read ({})", failure.reason);
                    notes.push((here, format!("skipped: {reason}")));
                    skipped.push((number, reason));
                }
            }
            continue;
        }
        let Part {
            mut record,
            location,
            content,
            nested,
            notes: own,
        } = match guarded(|| Ok::<_, Infallible>(attachment(&part, depth + 1 > DEPTH_MAX))) {
            Ok(read) => read,
            Err(failure) => {
                notes.push((here, format!("skipped: {}", failure.reason)));
                skipped.push((number, failure.reason));
                continue;
            }
        };
        notes.extend(own.into_iter().map(|note| (here.clone(), note)));
        if let Some(content) = &content {
            let extension = record.filename.as_deref().and_then(extension_of);
            record.file = Some(files.store_bytes(content, extension)?);
        }
        match nested {
            Some(_) if depth + 1 > DEPTH_MAX => notes.push((here, too_deep())),
            Some(nested) => {
                let place = format!("{here}, message");
                let (mut inner, _) = read(&nested, depth + 1, &place, files, notes)?;
                inner.line = "";
                record.message = Some(Box::new(inner));
            }
            None => {}
        }
        message.attachments.push(record);
        locations.push(location);
    }
    let mut own = Vec::new();
    let bodies = match stored {
        Some(stored) => stored.over(bodies, &mut own),
        None => bodies,
    };
    markdown::make(&mut message, &bodies, &locations, files, &mut own)?;
    notes.extend(own.into_iter().map(|note| (at.to_owned(), note)));
    Ok((message, skipped))
}

fn tnef(part: &MessagePart<'_>) -> bool {
    part.is_content_type("application", "ms-tnef")
        || part.is_content_type("application", "vnd.ms-tnef")
        || part
            .attachment_name()
            .is_some_and(|name| name.eq_ignore_ascii_case("winmail.dat"))
}

fn tnef_bodies(
    read: &tnef::Tnef,
    parent: Option<u16>,
    at: &str,
    notes: &mut Vec<(String, String)>,
) -> mapping::Stored {
    let (stored, own) = mapping::stored(&read.props, read.code_page(parent));
    let own = read.notes.iter().cloned().chain(own);
    notes.extend(own.map(|note| (at.to_owned(), note)));
    stored
}

struct Records<'a> {
    message: &'a mut Message,
    locations: &'a mut Vec<Option<String>>,
    number: &'a mut u32,
}

fn tnef_attachments(
    read: &tnef::Tnef,
    code_page: u16,
    depth: usize,
    at: &str,
    to: Records<'_>,
    files: &mut Media,
    notes: &mut Vec<(String, String)>,
) -> Result<(), Fault> {
    for attached in &read.attachments {
        *to.number += 1;
        let here = format!("{at}, attachment {}", to.number);
        let content = match attached.props.get(&tag::ATTACH_DATA) {
            Some(Value::Binary(content)) => Some(content.as_slice()),
            _ => None,
        };
        let mapping::Mapped {
            mut record,
            extension,
            location,
            notes: own,
        } = mapping::attachment(&attached.props, content, code_page);
        notes.extend(own.into_iter().map(|note| (here.clone(), note)));
        if let Some(content) = content {
            record.file = Some(files.store_bytes(content, extension.as_deref())?);
        }
        match &attached.embedded {
            Embedded::None => {}
            Embedded::Unread(note) => notes.push((here, note.clone())),
            Embedded::Read(inner) => {
                let place = format!("{here}, message");
                let message = tnef_message(inner, code_page, depth + 1, &place, files, notes)?;
                record.message = Some(Box::new(message));
            }
        }
        to.message.attachments.push(record);
        to.locations.push(location);
    }
    Ok(())
}

fn tnef_message(
    read: &tnef::Tnef,
    parent: u16,
    depth: usize,
    at: &str,
    files: &mut Media,
    notes: &mut Vec<(String, String)>,
) -> Result<Message, Fault> {
    let code_page = read.code_page(Some(parent));
    let (mut message, bodies, own) = mapping::message(&read.props, &read.recipients, code_page);
    let own = read.notes.iter().cloned().chain(own);
    notes.extend(own.map(|note| (at.to_owned(), note)));
    let mut locations = Vec::new();
    let mut number = 0;
    let attached = Records {
        message: &mut message,
        locations: &mut locations,
        number: &mut number,
    };
    tnef_attachments(read, code_page, depth, at, attached, files, notes)?;
    let mut own = mapping::rights_managed(&message);
    markdown::make(&mut message, &bodies, &locations, files, &mut own)?;
    notes.extend(own.into_iter().map(|note| (at.to_owned(), note)));
    message.line = "";
    Ok(message)
}

fn unread(part: &MessagePart<'_>) -> Option<String> {
    let smime = part.is_content_type("application", "pkcs7-mime")
        || part.is_content_type("application", "x-pkcs7-mime");
    smime.then(|| OPAQUE_SMIME.to_owned())
}

fn rights_managed(parsed: &MessageRef<'_>) -> bool {
    let class = parsed.headers().get("Content-Class").is_some_and(|header| {
        header
            .raw_value()
            .trim_ascii()
            .eq_ignore_ascii_case(b"rpmsg.message")
    });
    class
        || parsed
            .parts()
            .any(|part| part.attachment_name().is_some_and(rpmsg))
}

fn headers(parsed: &MessageRef<'_>, notes: &mut Vec<String>) -> Message {
    let headers = parsed.headers();
    let first_received = headers.all(HeaderName::Received).next();
    Message {
        line: "message",
        subject: parsed.subject().map(str::to_owned),
        sender: parsed
            .from()
            .and_then(|from| from.first())
            .and_then(|mailbox| address(mailbox.name(), mailbox.address())),
        to: addresses(parsed.to()),
        cc: addresses(parsed.cc()),
        bcc: addresses(parsed.bcc()),
        submit_time: time(
            headers.get(HeaderName::Date),
            |header| header.value().as_datetime(),
            notes,
        ),
        delivery_time: time(
            first_received,
            |header| header.value().as_received().and_then(|r| r.date()),
            notes,
        ),
        transport_headers: header_block(parsed.root_part().raw_headers(), notes),
        message_id: stored(headers.get(HeaderName::MessageId), notes),
        in_reply_to: stored(headers.get(HeaderName::InReplyTo), notes),
        references: stored(headers.get(HeaderName::References), notes),
        importance: importance(&headers, notes),
        sensitivity: sensitivity(headers.get(HeaderName::Sensitivity), notes),
        conversation_topic: headers
            .get(HeaderName::ThreadTopic)
            .and_then(|header| header.value().as_text())
            .map(str::trim)
            .filter(|topic| !topic.is_empty())
            .map(str::to_owned),
        conversation_index: thread_index(headers.get(HeaderName::ThreadIndex), notes),
        ..Message::default()
    }
}

fn importance(headers: &Headers<'_>, notes: &mut Vec<String>) -> Option<&'static str> {
    let header = headers.get(HeaderName::Importance);
    let named = header.and_then(|header| {
        let value = stored(Some(header), notes)?;
        let name = ["low", "normal", "high"]
            .into_iter()
            .find(|name| value.eq_ignore_ascii_case(name));
        if name.is_none() {
            notes.push(unknown(&header, &value));
        }
        name
    });
    named.or_else(|| {
        let header = headers.get(HeaderName::XPriority)?;
        let value = stored(Some(header), notes)?;
        let digits = value.bytes().take_while(u8::is_ascii_digit).count();
        let band = match value[..digits].parse::<u32>() {
            Ok(1 | 2) => Some("high"),
            Ok(3) => Some("normal"),
            Ok(4 | 5) => Some("low"),
            _ => None,
        };
        if band.is_none() {
            notes.push(unknown(&header, &value));
        }
        band
    })
}

fn sensitivity(header: Option<Header<'_>>, notes: &mut Vec<String>) -> Option<&'static str> {
    let header = header?;
    let value = stored(Some(header), notes)?;
    let name = [
        ("personal", "personal"),
        ("private", "private"),
        ("company-confidential", "confidential"),
    ]
    .into_iter()
    .find(|(stored, _)| value.eq_ignore_ascii_case(stored))
    .map(|(_, name)| name);
    if name.is_none() {
        notes.push(unknown(&header, &value));
    }
    name
}

fn unknown(header: &Header<'_>, value: &str) -> String {
    format!(
        "the {} header has unknown value {value:?}; left out",
        header.raw_name()
    )
}

fn thread_index(header: Option<Header<'_>>, notes: &mut Vec<String>) -> Option<String> {
    let header = header?;
    let value = stored(Some(header), notes)?;
    let (bytes, malformed) = Encoding::Base64.decode_checked(value.as_bytes());
    if malformed || bytes.is_empty() {
        notes.push(format!(
            "the {} header is not valid base64; left out",
            header.raw_name()
        ));
        return None;
    }
    Some(mapping::hex(&bytes))
}

/// A parsed message's bodies, its text body's text parts and its HTML body's HTML parts, each
/// joined; a body that decodes lossily gives a note.
pub fn bodies(parsed: &MessageRef<'_>, notes: &mut Vec<String>) -> Bodies {
    let text = joined(
        parsed.text_body(),
        |kind| matches!(kind, PartKind::Text),
        "text body",
        notes,
    );
    let html = joined(
        parsed.html_body(),
        |kind| matches!(kind, PartKind::Html),
        "HTML body",
        notes,
    );
    Bodies { html, text }
}

fn joined<'m>(
    list: impl Iterator<Item = MessagePart<'m>>,
    kind: impl Fn(&PartKind<'m>) -> bool,
    what: &str,
    notes: &mut Vec<String>,
) -> Option<String> {
    let mut problems = DecodeProblems::default();
    let mut texts = Vec::new();
    for part in list.filter(|part| kind(&part.kind())) {
        if let Some((text, found)) = part.text_checked() {
            problems |= found;
            texts.push(text);
        }
    }
    lossy_note(problems, what, notes);
    (!texts.is_empty()).then(|| texts.join("\n"))
}

struct Part<'m> {
    record: Attachment,
    location: Option<String>,
    content: Option<Cow<'m, [u8]>>,
    nested: Option<MessageRef<'m>>,
    notes: Vec<String>,
}

fn attachment<'m>(part: &MessagePart<'m>, too_deep_here: bool) -> Part<'m> {
    let mut notes = Vec::new();
    let mime_type = part.content_type().map(|content_type| {
        let ctype = content_type.ctype();
        match content_type.subtype() {
            Some(subtype) => format!("{ctype}/{subtype}"),
            None => ctype.to_owned(),
        }
    });
    let mut record = Attachment {
        filename: owned(part.attachment_name()),
        mime_type: owned(mime_type.as_deref()),
        content_id: owned(part.content_id()),
        ..Attachment::default()
    };
    let location = owned(part.content_location().map(str::trim));
    let embedded =
        part.is_content_type("message", "rfc822") || part.is_content_type("message", "global");
    if let PartKind::Message(nested) = part.kind() {
        record.method = Some("embedded_message");
        return Part {
            record,
            location,
            content: None,
            nested: Some(nested),
            notes,
        };
    }
    if embedded {
        record.method = Some("embedded_message");
        notes.push(if too_deep_here {
            too_deep()
        } else {
            "the embedded message can't be parsed; listed without it".to_owned()
        });
        return Part {
            record,
            location,
            content: None,
            nested: None,
            notes,
        };
    }
    let (content, problems) = part.decoded_checked();
    lossy_note(problems, "attachment's content", &mut notes);
    record.method = Some("by_value");
    record.size = Some(content.len() as u64);
    Part {
        record,
        location,
        content: Some(content),
        nested: None,
        notes,
    }
}

fn owned(text: Option<&str>) -> Option<String> {
    text.filter(|text| !text.is_empty()).map(str::to_owned)
}

fn lossy_note(problems: DecodeProblems, what: &str, notes: &mut Vec<String>) {
    if problems.is_empty() {
        return;
    }
    let found: Vec<&str> = [
        (
            DecodeProblems::MALFORMED_TRANSFER_ENCODING,
            "has a malformed transfer encoding",
        ),
        (
            DecodeProblems::UNKNOWN_TRANSFER_ENCODING,
            "has an unknown transfer encoding",
        ),
        (DecodeProblems::UNKNOWN_CHARSET, "has an unknown charset"),
        (
            DecodeProblems::MALFORMED_CHARSET,
            "is not valid in its charset",
        ),
    ]
    .into_iter()
    .filter(|(problem, _)| problems.contains(*problem))
    .map(|(_, phrase)| phrase)
    .collect();
    notes.push(format!(
        "the {what} {}; decoded lossily",
        found.join(" and ")
    ));
}

fn address(name: Option<&str>, address: Option<&str>) -> Option<Address> {
    let made = Address {
        name: owned(name),
        address: owned(address),
        address_type: Some(SMTP.to_owned()),
        smtp_address: None,
    };
    (made.name.is_some() || made.address.is_some()).then_some(made)
}

fn addresses(list: Option<AddressList<'_>>) -> Vec<Address> {
    list.into_iter()
        .flat_map(|list| list.mailboxes())
        .filter_map(|mailbox| address(mailbox.name(), mailbox.address()))
        .collect()
}

fn time(
    header: Option<Header<'_>>,
    date: impl FnOnce(&Header<'_>) -> Option<DateTime>,
    notes: &mut Vec<String>,
) -> Option<String> {
    let header = header?;
    let utc = date(&header)
        .filter(DateTime::is_valid)
        .and_then(|date| mapping::utc(date.to_timestamp()));
    if utc.is_none() {
        notes.push(format!(
            "the {} header has no date that can be read; left out",
            header.raw_name()
        ));
    }
    utc
}

fn stored(header: Option<Header<'_>>, notes: &mut Vec<String>) -> Option<String> {
    let header = header?;
    let what = format!("{} header", header.raw_name());
    let raw = lossy(header.raw_value(), &what, notes);
    let unfolded: String = raw.chars().filter(|&c| c != '\r' && c != '\n').collect();
    let trimmed = unfolded.trim();
    (!trimmed.is_empty()).then(|| trimmed.to_owned())
}

fn header_block(raw: &[u8], notes: &mut Vec<String>) -> Option<String> {
    let block = raw
        .strip_suffix(b"\r\n")
        .or_else(|| raw.strip_suffix(b"\n"))
        .filter(|block| block.ends_with(b"\n"))
        .unwrap_or(raw);
    if block.is_empty() {
        return None;
    }
    Some(lossy(block, "header block", notes))
}

fn lossy(raw: &[u8], what: &str, notes: &mut Vec<String>) -> String {
    let text = String::from_utf8_lossy(raw);
    if matches!(text, Cow::Owned(_)) {
        notes.push(format!("the {what} is not valid UTF-8; decoded lossily"));
    }
    text.into_owned()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn notes(raw: &str) -> Vec<(String, String)> {
        let parsed = parser().parse(raw.as_bytes()).unwrap();
        message(&parsed.root(), &mut Media::new(None))
            .unwrap()
            .notes
    }

    fn with_part(part: &str) -> String {
        with_part_in("b", part)
    }

    fn with_part_in(b: &str, part: &str) -> String {
        format!(
            "From: ann@example.com\r\nContent-Type: multipart/mixed; boundary={b}\r\n\r\n\
             --{b}\r\nContent-Type: text/plain\r\n\r\nSee the attachment.\r\n\
             --{b}\r\n{part}\r\n--{b}--\r\n"
        )
    }

    const UNREAD: &str = "skipped: the TNEF part can't be read (the signature is 4 bytes, past \
                          the 1 that remain)";
    const SMIME: &str = "it holds an S/MIME part (application/pkcs7-mime), encrypted or \
                         opaquely signed, whose content is not read";

    #[test]
    fn a_tnef_part_that_cant_be_read_is_a_skipped_attachment() {
        for part in [
            "Content-Type: application/ms-tnef\r\n\r\nx",
            "Content-Type: Application/VND.MS-TNEF; name=x.dat\r\n\r\nx",
            "Content-Type: application/octet-stream\r\n\
             Content-Disposition: attachment; filename=\"WINMAIL.DAT\"\r\n\r\nx",
        ] {
            let raw = with_part(part);
            let parsed = parser().parse(raw.as_bytes()).unwrap();
            let read = message(&parsed.root(), &mut Media::new(None)).unwrap();
            assert_eq!(
                read.notes,
                [(", attachment 1".to_owned(), UNREAD.to_owned())],
                "{part}"
            );
            assert_eq!(read.skipped.len(), 1, "{part}");
            assert!(read.message.attachments.is_empty(), "{part}");
            assert_eq!(
                read.message.markdown.as_deref(),
                Some("See the attachment.")
            );
        }
    }

    fn tnef_stream(attributes: &[(u8, u32, &[u8])]) -> Vec<u8> {
        let mut bytes = vec![0x78, 0x9F, 0x3E, 0x22, 0, 0];
        for (level, id, data) in attributes {
            bytes.push(*level);
            bytes.extend_from_slice(&id.to_le_bytes());
            bytes.extend_from_slice(&u32::try_from(data.len()).unwrap().to_le_bytes());
            bytes.extend_from_slice(data);
            let sum = data
                .iter()
                .fold(0u16, |sum, b| sum.wrapping_add(u16::from(*b)));
            bytes.extend_from_slice(&sum.to_le_bytes());
        }
        bytes
    }

    fn base64(bytes: &[u8]) -> String {
        const DIGITS: &[u8] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
        let mut out = String::new();
        for chunk in bytes.chunks(3) {
            let word = chunk
                .iter()
                .enumerate()
                .fold(0u32, |word, (i, b)| word | u32::from(*b) << (16 - 8 * i));
            for i in 0..4 {
                out.push(if i <= chunk.len() {
                    char::from(DIGITS[(word >> (18 - 6 * i) & 63) as usize])
                } else {
                    '='
                });
            }
        }
        out
    }

    fn tnef_part(stream: &[u8]) -> String {
        format!(
            "Content-Type: application/ms-tnef; name=winmail.dat\r\n\
             Content-Transfer-Encoding: base64\r\n\r\n{}",
            base64(stream)
        )
    }

    #[test]
    fn a_tnef_parts_attachments_are_the_messages_and_its_text_body_wins_over_plain_rtf() {
        let rtf = b"{\\rtf1\\ansi From RTF}";
        let mut compressed = Vec::new();
        let size = u32::try_from(rtf.len()).unwrap();
        for word in [size + 12, size, 0x414C_454D, 0] {
            compressed.extend_from_slice(&word.to_le_bytes());
        }
        compressed.extend_from_slice(rtf);
        let mut props = 1u32.to_le_bytes().to_vec();
        props.extend_from_slice(&0x1009_0102u32.to_le_bytes());
        props.extend_from_slice(&1u32.to_le_bytes());
        props.extend_from_slice(&u32::try_from(compressed.len()).unwrap().to_le_bytes());
        compressed.resize(compressed.len().next_multiple_of(4), 0);
        props.extend_from_slice(&compressed);
        let stream = tnef_stream(&[
            (1, 0x0006_9003, &props),
            (2, 0x0006_9002, &[0; 14]),
            (2, 0x0001_8010, b"notes.txt\0"),
            (2, 0x0006_800F, b"Notes."),
        ]);
        let raw = with_part(&tnef_part(&stream));
        let parsed = parser().parse(raw.as_bytes()).unwrap();
        let read = message(&parsed.root(), &mut Media::new(None)).unwrap();
        assert_eq!(read.notes, []);
        assert_eq!(
            read.message.markdown.as_deref(),
            Some("See the attachment.")
        );
        let [attachment] = &read.message.attachments[..] else {
            panic!("one attachment");
        };
        assert_eq!(attachment.filename.as_deref(), Some("notes.txt"));
        assert_eq!(attachment.size, Some(6));

        let bare = format!("From: ann@example.com\r\n{}", tnef_part(&stream));
        let parsed = parser().parse(bare.as_bytes()).unwrap();
        let read = message(&parsed.root(), &mut Media::new(None)).unwrap();
        assert_eq!(read.notes, []);
        assert_eq!(read.message.markdown.as_deref(), Some("From RTF"));
        assert_eq!(read.message.attachments.len(), 1);
    }

    fn string8(property: u32, text: &[u8]) -> Vec<u8> {
        let mut bytes = property.to_le_bytes().to_vec();
        bytes.extend_from_slice(&1u32.to_le_bytes());
        bytes.extend_from_slice(&u32::try_from(text.len()).unwrap().to_le_bytes());
        bytes.extend_from_slice(text);
        bytes.resize(bytes.len().next_multiple_of(4), 0);
        bytes
    }

    #[test]
    fn a_tnef_attachments_embedded_message_is_nested_under_it() {
        let inner_props = [
            2u32.to_le_bytes().to_vec(),
            string8(0x0037_001E, b"Inner\0"),
            string8(0x1000_001E, b"Inner text.\0"),
        ]
        .concat();
        let inner = tnef_stream(&[(1, 0x0006_9003, &inner_props)]);
        let iid = [
            0x07, 0x03, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00,
            0x00, 0x46,
        ];
        let props = [
            2u32.to_le_bytes().to_vec(),
            0x3705_0003u32.to_le_bytes().to_vec(),
            5u32.to_le_bytes().to_vec(),
            string8(0x3701_000D, &[&iid[..], &inner].concat()),
        ]
        .concat();
        let stream = tnef_stream(&[(2, 0x0006_9002, &[0; 14]), (2, 0x0006_9005, &props)]);
        let raw = with_part(&tnef_part(&stream));
        let parsed = parser().parse(raw.as_bytes()).unwrap();
        let read = message(&parsed.root(), &mut Media::new(None)).unwrap();
        assert_eq!(read.notes, []);
        let [attachment] = &read.message.attachments[..] else {
            panic!("one attachment");
        };
        assert_eq!(attachment.method, Some("embedded_message"));
        assert_eq!(attachment.file, None);
        let nested = attachment.message.as_deref().unwrap();
        assert_eq!(nested.line, "");
        assert_eq!(nested.subject.as_deref(), Some("Inner"));
        assert_eq!(nested.markdown.as_deref(), Some("Inner text."));
    }

    #[test]
    fn a_nested_messages_tnef_part_is_its_own() {
        let nested = with_part("Content-Type: application/ms-tnef\r\n\r\nx");
        let outer = with_part_in(
            "outer",
            &format!("Content-Type: message/rfc822\r\n\r\n{nested}"),
        );
        assert_eq!(
            notes(&outer),
            [(
                ", attachment 1, message, attachment 1".to_owned(),
                UNREAD.to_owned()
            )]
        );
    }

    #[test]
    fn an_opaque_smime_part_gives_its_message_a_note() {
        let encrypted = "From: ann@example.com\r\n\
                         Content-Type: application/pkcs7-mime; smime-type=enveloped-data; \
                         name=smime.p7m\r\nContent-Transfer-Encoding: base64\r\n\r\nMIAGCSqG\r\n";
        assert_eq!(notes(encrypted), [(String::new(), SMIME.to_owned())]);
        let part = "Content-Type: application/x-pkcs7-mime; smime-type=signed-data\r\n\r\nx";
        assert_eq!(notes(&with_part(part)), [(String::new(), SMIME.to_owned())]);
        let signed = "From: ann@example.com\r\n\
                      Content-Type: multipart/signed; protocol=\"application/pkcs7-signature\"; \
                      boundary=s\r\n\r\n--s\r\nContent-Type: text/plain\r\n\r\nSigned.\r\n\
                      --s\r\nContent-Type: application/pkcs7-signature; name=smime.p7s\r\n\r\n\
                      x\r\n--s--\r\n";
        assert_eq!(notes(signed), []);
    }
}
