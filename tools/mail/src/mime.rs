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
//! for a .pst or a .msg; neither body is part of the line. The attachments are the message's parts that aren't body text, in order: its
//! attachments, and the images, audio and video the bodies show. A message/rfc822 or
//! message/global part is an embedded message, nested under its record to [`DEPTH_MAX`] messages.
//!
//! Two kinds of part hold content no reader here opens: a TNEF part (`winmail.dat`, Outlook's own
//! serialization of a message's rich text and attachments) and an opaque S/MIME part (an
//! encrypted or opaquely signed `application/pkcs7-mime`). Each gives its message a note, so the
//! message's Markdown, empty or partial, is never read as all there is.
//!
//! mail-parser decodes US-ASCII and ISO-8859-1 as windows-1252, as WHATWG labels them, and the
//! five bytes windows-1252 leaves undefined as U+FFFD. Whatever it reports as a decoding problem
//! (a malformed or unknown transfer encoding, an unknown charset, bytes not valid in the charset)
//! gives a note: the text is decoded lossily. It reports none for a header's encoded words.

use std::borrow::Cow;
use std::convert::Infallible;

use docproc_envelope::{Fault, Media, extension_of, guarded};
use mail_parser::{
    AddressList, DateTime, DecodeProblems, Encoding, Header, HeaderForm, HeaderName, Headers,
    MessageParser, MessagePart, MessageRef, PartKind, PartRole,
};

use crate::model::{Address, Attachment, Message};
use crate::msg::DEPTH_MAX;
use crate::{mapping, markdown};

/// The address type of every address a MIME message carries.
const SMTP: &str = "SMTP";

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

/// Maps the message `parsed`, at `depth` and at the place `at`, with its attachments; embedded
/// messages recursively. Returns it with the attachments that couldn't be read.
fn read(
    parsed: &MessageRef<'_>,
    depth: usize,
    at: &str,
    files: &mut Media,
    notes: &mut Vec<(String, String)>,
) -> Result<(Message, Vec<(u32, String)>), Fault> {
    let mut own = Vec::new();
    let mut message = headers(parsed, &mut own);
    message.markdown = markdown(parsed, &mut own);
    own.extend(parsed.parts().filter_map(|part| unread(&part)));
    notes.extend(own.into_iter().map(|note| (at.to_owned(), note)));
    let mut skipped = Vec::new();
    let parts = parsed
        .parts()
        .filter(|part| part.is_attachment() || matches!(part.role(), PartRole::MediaBody));
    for (number, part) in (1u32..).zip(parts) {
        let here = format!("{at}, attachment {number}");
        let Part {
            mut record,
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
            record.file = Some(files.store(content.as_ref(), extension)?);
        }
        match nested {
            Some(_) if depth + 1 > DEPTH_MAX => notes.push((here, too_deep())),
            // An attachment inside it that can't be read has its warning; the embedded message
            // has no line to give it a skipped line after.
            Some(nested) => {
                let place = format!("{here}, message");
                let (mut inner, _) = read(&nested, depth + 1, &place, files, notes)?;
                inner.line = "";
                record.message = Some(Box::new(inner));
            }
            None => {}
        }
        message.attachments.push(record);
    }
    Ok((message, skipped))
}

/// The note for a part whose content no reader here opens: a TNEF part, by its content type or
/// its file name, `winmail.dat`, and an opaque S/MIME part. A detached signature
/// (`application/pkcs7-signature`) is no such part: the content it signs is read.
fn unread(part: &MessagePart<'_>) -> Option<String> {
    let tnef = part.is_content_type("application", "ms-tnef")
        || part.is_content_type("application", "vnd.ms-tnef")
        || part
            .attachment_name()
            .is_some_and(|name| name.eq_ignore_ascii_case("winmail.dat"));
    if tnef {
        return Some(
            "it holds a TNEF part (winmail.dat), Outlook's own form of a message's text and \
             attachments, whose content is not read"
                .to_owned(),
        );
    }
    let smime = part.is_content_type("application", "pkcs7-mime")
        || part.is_content_type("application", "x-pkcs7-mime");
    smime.then(|| {
        "it holds an S/MIME part (application/pkcs7-mime), encrypted or opaquely signed, \
         whose content is not read"
            .to_owned()
    })
}

/// The header fields of a message line.
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

/// The importance: Importance's low, normal or high, else X-Priority's band, 1 to 2 high,
/// 3 normal and 4 to 5 low, read from its leading number, as in "1 (Highest)". A value of
/// either that isn't one of these is left out, with a note, and an unknown Importance falls
/// back to X-Priority.
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

/// The sensitivity: Sensitivity's Personal, Private and Company-Confidential (RFC 2156), as
/// personal, private and confidential. Any other value is left out, with a note.
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

/// The note for a header whose value isn't one the model names.
fn unknown(header: &Header<'_>, value: &str) -> String {
    format!(
        "the {} header has unknown value {value:?}; left out",
        header.raw_name()
    )
}

/// The conversation index: Thread-Index base64-decoded, in lowercase hex, as a .pst or a .msg
/// writes PidTagConversationIndex. One that isn't valid base64 is left out, with a note.
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

/// The Markdown made from the text and HTML bodies. A body list's parts of the other kind, the
/// stand-ins RFC 8621 copies in for a missing version, and its media are no part of a body. Parts
/// are joined by a line break, so that one's last line never runs into the next one's first.
fn markdown(parsed: &MessageRef<'_>, notes: &mut Vec<String>) -> Option<String> {
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
    markdown::derive(html.as_deref(), text.as_deref(), notes)
}

/// The text of a body list's parts of one kind, decoded and joined in order, with one note for
/// all of their decoding problems.
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

/// One attachment part read: its record, its decoded content when it is a file, which the caller
/// stores and names, and its nested message when it is a message/rfc822 part, with its notes.
struct Part<'m> {
    record: Attachment,
    content: Option<Cow<'m, [u8]>>,
    nested: Option<MessageRef<'m>>,
    notes: Vec<String>,
}

/// The note for an embedded message below the depth limit, listed without its message.
fn too_deep() -> String {
    format!("an embedded message nested deeper than {DEPTH_MAX} messages; listed without it")
}

/// Reads one attachment part; `too_deep_here` says the part is below [`DEPTH_MAX`] messages,
/// where the parser stops parsing a transfer-encoded message/rfc822 part.
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
        inline: part
            .content_disposition()
            .is_some_and(|disposition| disposition.is_inline()),
        ..Attachment::default()
    };
    let embedded =
        part.is_content_type("message", "rfc822") || part.is_content_type("message", "global");
    if let PartKind::Message(nested) = part.kind() {
        record.method = Some("embedded_message");
        return Part {
            record,
            content: None,
            nested: Some(nested),
            notes,
        };
    }
    if embedded {
        record.method = Some("embedded_message");
        // The parser's encoded nesting counts encoded levels only, so it reaches its limit only
        // where the depth is past it too: a part left unparsed there is unparsed for its depth.
        notes.push(if too_deep_here {
            too_deep()
        } else {
            "the embedded message can't be parsed; listed without it".to_owned()
        });
        return Part {
            record,
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
        content: Some(content),
        nested: None,
        notes,
    }
}

/// A text that is present and not empty, owned.
fn owned(text: Option<&str>) -> Option<String> {
    text.filter(|text| !text.is_empty()).map(str::to_owned)
}

/// A note for the decoding problems mail-parser found in `what`, when it found any.
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

/// An address of its name and address, either of which may be missing but not both, with
/// address type SMTP.
fn address(name: Option<&str>, address: Option<&str>) -> Option<Address> {
    let made = Address {
        name: owned(name),
        address: owned(address),
        address_type: Some(SMTP.to_owned()),
    };
    (made.name.is_some() || made.address.is_some()).then_some(made)
}

/// A recipient header's mailboxes in order, a group's members in its place.
fn addresses(list: Option<AddressList<'_>>) -> Vec<Address> {
    list.into_iter()
        .flat_map(|list| list.mailboxes())
        .filter_map(|mailbox| address(mailbox.name(), mailbox.address()))
        .collect()
}

/// A date header's time in UTC, to the second. One that is present but has no date that reads,
/// or one outside mail-parser's valid range, is left out with a note.
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

/// A header's value as stored: its raw bytes, unfolded and trimmed. A value that is blank is
/// left out, and one that isn't valid UTF-8 is decoded lossily, with a note.
fn stored(header: Option<Header<'_>>, notes: &mut Vec<String>) -> Option<String> {
    let header = header?;
    let what = format!("{} header", header.raw_name());
    let raw = lossy(header.raw_value(), &what, notes);
    let unfolded: String = raw.chars().filter(|&c| c != '\r' && c != '\n').collect();
    let trimmed = unfolded.trim();
    (!trimmed.is_empty()).then(|| trimmed.to_owned())
}

/// The raw header block, without the blank line that ends it, read as [`lossy`] reads it.
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

/// Raw header bytes as UTF-8, decoded lossily, with a note naming `what`, when they aren't.
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

    /// The notes of the message `raw`, mapped with no media directory.
    fn notes(raw: &str) -> Vec<(String, String)> {
        let parsed = parser().parse(raw.as_bytes()).unwrap();
        message(&parsed.root(), &mut Media::new(None))
            .unwrap()
            .notes
    }

    /// A multipart/mixed message of a text part and `part`, its headers and its body, whose
    /// boundary is `b`.
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

    const TNEF: &str = "it holds a TNEF part (winmail.dat), Outlook's own form of a message's \
                        text and attachments, whose content is not read";
    const SMIME: &str = "it holds an S/MIME part (application/pkcs7-mime), encrypted or \
                         opaquely signed, whose content is not read";

    /// A TNEF part is told by its content type, either of its names, or by its file name.
    #[test]
    fn a_tnef_part_gives_its_message_a_note() {
        for part in [
            "Content-Type: application/ms-tnef\r\n\r\nx",
            "Content-Type: Application/VND.MS-TNEF; name=x.dat\r\n\r\nx",
            "Content-Type: application/octet-stream\r\n\
             Content-Disposition: attachment; filename=\"WINMAIL.DAT\"\r\n\r\nx",
        ] {
            assert_eq!(
                notes(&with_part(part)),
                [(String::new(), TNEF.to_owned())],
                "{part}"
            );
        }
    }

    /// An opaque S/MIME part is noted, the whole message's or one part's; a detached signature
    /// is not, since the content it signs is read.
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

    /// A nested message's TNEF part is its own message's note, placed below the attachment.
    #[test]
    fn a_nested_messages_tnef_part_is_its_note() {
        let nested = with_part("Content-Type: application/ms-tnef\r\n\r\nx");
        let outer = with_part_in(
            "outer",
            &format!("Content-Type: message/rfc822\r\n\r\n{nested}"),
        );
        assert_eq!(
            notes(&outer),
            [(", attachment 1, message".to_owned(), TNEF.to_owned())]
        );
    }
}
