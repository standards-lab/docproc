//! The one mapping from MAPI properties to the message model, shared by the .pst and .msg
//! readers. It reads only what a reader has put in the bags, and returns its notes (lossy
//! decodings, values it couldn't use) for the caller to warn about with the record's place.

use docproc_envelope::extension_of;

use crate::codepage::{self, Strings};
use crate::markdown::Bodies;
use crate::model::{Address, Attachment, Message};
use crate::props::{Bag, Value, integer, tag};
use crate::rtf;

/// Recipient types of PidTagRecipientType (MS-OXOMSG 2.2.3.1), less the MAPI_P1 resend flag.
const MAPI_TO: i64 = 1;
const MAPI_CC: i64 = 2;
const MAPI_BCC: i64 = 3;
const MAPI_P1: i64 = 0x1000_0000;

/// Maps a message's properties and its recipient table's rows to a message line, and gives the
/// bodies its Markdown is made from once its attachments are read ([`crate::markdown::make`]).
/// Strings in PT_STRING8 are decoded in `code_page`.
pub fn message(props: &Bag, recipients: &[Bag], code_page: u16) -> (Message, Bodies, Vec<String>) {
    let mut strings = Strings::new(code_page);
    let mut notes = Vec::new();
    let mut message = Message {
        line: "message",
        class: strings.text(props, tag::MESSAGE_CLASS),
        subject: strings.text(props, tag::SUBJECT).map(strip_subject_prefix),
        sender: address(
            &mut strings,
            props,
            [
                tag::SENDER_NAME,
                tag::SENDER_EMAIL_ADDRESS,
                tag::SENDER_ADDRESS_TYPE,
                tag::SENDER_SMTP_ADDRESS,
            ],
        ),
        submit_time: time(props, tag::CLIENT_SUBMIT_TIME, &mut notes),
        delivery_time: time(props, tag::MESSAGE_DELIVERY_TIME, &mut notes),
        creation_time: time(props, tag::CREATION_TIME, &mut notes),
        modification_time: time(props, tag::LAST_MODIFICATION_TIME, &mut notes),
        importance: named(
            props,
            tag::IMPORTANCE,
            &["low", "normal", "high"],
            &mut notes,
        ),
        sensitivity: named(
            props,
            tag::SENSITIVITY,
            &["normal", "personal", "private", "confidential"],
            &mut notes,
        ),
        transport_headers: strings.text(props, tag::TRANSPORT_MESSAGE_HEADERS),
        message_id: strings.text(props, tag::INTERNET_MESSAGE_ID),
        in_reply_to: strings.text(props, tag::IN_REPLY_TO_ID),
        references: strings.text(props, tag::INTERNET_REFERENCES),
        conversation_topic: strings.text(props, tag::CONVERSATION_TOPIC),
        conversation_index: binary(props, tag::CONVERSATION_INDEX).map(hex),
        ..Message::default()
    };
    let bodies = bodies(&mut strings, props, code_page, &mut notes);
    for (row, recipient) in recipients.iter().enumerate() {
        let Some(address) = address(
            &mut strings,
            recipient,
            [
                tag::DISPLAY_NAME,
                tag::EMAIL_ADDRESS,
                tag::ADDRESS_TYPE,
                tag::SMTP_ADDRESS,
            ],
        ) else {
            continue;
        };
        match integer(recipient, tag::RECIPIENT_TYPE).map(|kind| kind & !MAPI_P1) {
            Some(MAPI_TO) => message.to.push(address),
            Some(MAPI_CC) => message.cc.push(address),
            Some(MAPI_BCC) => message.bcc.push(address),
            kind => notes.push(format!(
                "recipient {row} has recipient type {kind:?}, not to, cc or bcc; left out"
            )),
        }
    }
    let mut all = strings.into_notes();
    all.append(&mut notes);
    (message, bodies, all)
}

/// The bodies a message's Markdown is made from, the first it holds of: its stored HTML, the
/// HTML recovered from its RTF body, its plain text, then its RTF body's own text, which is read
/// only when it is the one. The stored HTML, the text and the RTF are each still read, and
/// decoded; a body that can't be read is passed over with a note, and the Markdown is made from
/// the next.
fn bodies(strings: &mut Strings, props: &Bag, code_page: u16, notes: &mut Vec<String>) -> Bodies {
    let text = strings.text(props, tag::BODY);
    let html = html(strings, props, code_page);
    let rtf = binary(props, tag::RTF_COMPRESSED).and_then(|compressed| {
        rtf::decompress(compressed)
            .map_err(|reason| {
                notes.push(format!(
                    "property 0x{:04X} {reason}; left out",
                    tag::RTF_COMPRESSED
                ));
            })
            .ok()
    });
    let from_rtf = rtf.as_deref().and_then(rtf::html).map(|(html, mut lossy)| {
        notes.append(&mut lossy);
        html
    });
    let html = html.or(from_rtf);
    let text = match (&html, text) {
        (None, None) => rtf.as_deref().and_then(rtf::text).map(|(text, mut lossy)| {
            notes.append(&mut lossy);
            text
        }),
        (_, text) => text,
    };
    Bodies { html, text }
}

/// The stored HTML body. PidTagHtml is PT_BINARY, decoded in the code page
/// PidTagInternetCodepage names, else the one the HTML's own `<meta>` declares, else UTF-8 when
/// the bytes are valid UTF-8, else the message's, which is the store's when the message names
/// none. As PidTagBodyHtml in PT_STRING8 it is the same bytes and decoded the same way, not in
/// the message's code page: Apache POI's ASCII_UTF-8_CP1252_LCID1031_HTML.msg keeps its other
/// strings in 1252 and its HTML in UTF-8, its internet code page. In PT_UNICODE it is UTF-16.
fn html(strings: &mut Strings, props: &Bag, code_page: u16) -> Option<String> {
    let bytes = match props.get(&tag::HTML)? {
        Value::Binary(bytes) | Value::String8(bytes) => bytes,
        _ => return strings.text(props, tag::HTML),
    };
    let code_page = integer(props, tag::INTERNET_CODEPAGE)
        .and_then(|value| u16::try_from(value).ok())
        .or_else(|| codepage::meta_charset(bytes))
        .or_else(|| std::str::from_utf8(bytes).is_ok().then_some(codepage::UTF8))
        .unwrap_or(code_page);
    Some(strings.decode(tag::HTML, bytes, code_page))
}

/// An attachment mapped: its record, the extension it was stored under, its Content-Location,
/// which only matches an image's source to it and is no field, and the mapping's notes.
pub struct Mapped {
    pub record: Attachment,
    pub extension: Option<String>,
    pub location: Option<String>,
    pub notes: Vec<String>,
}

/// Maps an attachment's properties, and its content when it was read, to an attachment record.
/// The record has no `file` yet: the caller stores the content in the media store, which names it
/// by the content and the extension, and its `inline` is set once the message's Markdown is made.
/// Strings in PT_STRING8 are decoded in `code_page`, the message's.
pub fn attachment(props: &Bag, content: Option<&[u8]>, code_page: u16) -> Mapped {
    let mut strings = Strings::new(code_page);
    let mut notes = Vec::new();
    let filename = strings
        .text(props, tag::ATTACH_LONG_FILENAME)
        .or_else(|| strings.text(props, tag::ATTACH_FILENAME));
    // The stored extension is PidTagAttachExtension, stored with or without its dot, else the
    // filename's.
    let extension = strings
        .text(props, tag::ATTACH_EXTENSION)
        .map(|extension| extension.strip_prefix('.').unwrap_or(&extension).to_owned())
        .or_else(|| extension_of(filename.as_deref()?).map(str::to_owned));
    let attachment = Attachment {
        file: None,
        filename,
        mime_type: strings.text(props, tag::ATTACH_MIME_TAG),
        content_id: strings.text(props, tag::ATTACH_CONTENT_ID),
        inline: false,
        method: method(props, &mut notes),
        size: content.map(|content| content.len() as u64),
        message: None,
    };
    let location = strings.text(props, tag::ATTACH_CONTENT_LOCATION);
    let mut all = strings.into_notes();
    all.append(&mut notes);
    Mapped {
        record: attachment,
        extension,
        location,
        notes: all,
    }
}

/// PidTagAttachMethod (MS-OXCMSG 2.2.2.9) by name.
fn method(props: &Bag, notes: &mut Vec<String>) -> Option<&'static str> {
    let value = integer(props, tag::ATTACH_METHOD)?;
    let name = match value {
        0 => "none",
        1 => "by_value",
        2 => "by_reference",
        4 => "by_reference_only",
        5 => "embedded_message",
        6 => "storage",
        7 => "by_web_reference",
        _ => {
            notes.push(format!(
                "property 0x{:04X} has unknown value {value}; left out",
                tag::ATTACH_METHOD
            ));
            return None;
        }
    };
    Some(name)
}

/// A folder's display name, the last of its path, and its container class, decoded in
/// `code_page`.
pub fn folder(props: &Bag, code_page: u16) -> (Option<String>, Option<String>, Vec<String>) {
    let mut strings = Strings::new(code_page);
    let name = strings.text(props, tag::DISPLAY_NAME);
    let class = strings.text(props, tag::CONTAINER_CLASS);
    (name, class, strings.into_notes())
}

/// A store's display name, decoded in `code_page`.
pub fn store_name(props: &Bag, code_page: u16) -> (Option<String>, Vec<String>) {
    let mut strings = Strings::new(code_page);
    let name = strings.text(props, tag::DISPLAY_NAME);
    (name, strings.into_notes())
}

/// The address `bag` holds under the ids of its name, its address, its address type and its SMTP
/// address. The SMTP address is read only for an Exchange (`EX`) address: on any other type the
/// address is already the one to write to.
fn address(
    strings: &mut Strings,
    bag: &Bag,
    [name, email, kind, smtp]: [u16; 4],
) -> Option<Address> {
    let address_type = strings.text(bag, kind);
    let exchange = address_type
        .as_deref()
        .is_some_and(|kind| kind.eq_ignore_ascii_case("EX"));
    let address = Address {
        name: strings.text(bag, name),
        address: strings.text(bag, email),
        address_type,
        smtp_address: exchange.then(|| strings.text(bag, smtp)).flatten(),
    };
    (!address.is_empty()).then_some(address)
}

/// A .pst stores PidTagSubject with a two-character prefix when its first character is U+0001
/// (MS-PST 2.5.3.1.1.1): the marker and the prefix's length. Neither is part of the subject.
fn strip_subject_prefix(subject: String) -> String {
    let mut chars = subject.chars();
    if chars.next() == Some('\u{1}') {
        chars.next();
        chars.as_str().to_owned()
    } else {
        subject
    }
}

fn binary(bag: &Bag, id: u16) -> Option<&[u8]> {
    match bag.get(&id)? {
        Value::Binary(bytes) => Some(bytes),
        _ => None,
    }
}

/// Bytes in lowercase hex, as a binary property is written.
pub fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}

/// An enumerated integer property as its name, when the value is one of `names`.
fn named(
    bag: &Bag,
    id: u16,
    names: &[&'static str],
    notes: &mut Vec<String>,
) -> Option<&'static str> {
    let value = integer(bag, id)?;
    let name = usize::try_from(value).ok().and_then(|i| names.get(i));
    if name.is_none() {
        notes.push(format!(
            "property 0x{id:04X} has unknown value {value}; left out"
        ));
    }
    name.copied()
}

/// A PT_SYSTIME property in ISO 8601 UTC, to the second.
fn time(bag: &Bag, id: u16, notes: &mut Vec<String>) -> Option<String> {
    let Some(Value::Time(ticks)) = bag.get(&id) else {
        return None;
    };
    let iso = iso8601(*ticks);
    if iso.is_none() {
        notes.push(format!("property 0x{id:04X} is out of range; left out"));
    }
    iso
}

/// Seconds between 1601-01-01, the FILETIME epoch, and 1970-01-01.
const FILETIME_TO_UNIX: i64 = 11_644_473_600;

/// A FILETIME as "YYYY-MM-DDTHH:MM:SSZ", or None past year 9999.
fn iso8601(ticks: i64) -> Option<String> {
    utc(ticks.div_euclid(10_000_000) - FILETIME_TO_UNIX)
}

/// Seconds since 1970-01-01 as "YYYY-MM-DDTHH:MM:SSZ", or None past year 9999: the one form of
/// every time in the model, a MIME message's dates included.
pub fn utc(seconds: i64) -> Option<String> {
    let (days, second_of_day) = (seconds.div_euclid(86_400), seconds.rem_euclid(86_400));
    let (year, month, day) = civil_from_days(days);
    (year <= 9999).then(|| {
        format!(
            "{year:04}-{month:02}-{day:02}T{:02}:{:02}:{:02}Z",
            second_of_day / 3600,
            second_of_day / 60 % 60,
            second_of_day % 60
        )
    })
}

/// The proleptic Gregorian date of a day count from 1970-01-01 (Howard Hinnant's algorithm).
fn civil_from_days(days: i64) -> (i64, i64, i64) {
    let z = days + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z.rem_euclid(146_097);
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let day = doy - (153 * mp + 2) / 5 + 1;
    let month = if mp < 10 { mp + 3 } else { mp - 9 };
    let year = yoe + era * 400 + i64::from(month <= 2);
    (year, month, day)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn filetimes_format_as_iso8601_utc() {
        assert_eq!(
            iso8601(130_378_746_040_000_000).as_deref(),
            Some("2014-02-26T07:50:04Z")
        );
        assert_eq!(iso8601(0).as_deref(), Some("1601-01-01T00:00:00Z"));
        assert_eq!(
            iso8601(116_444_736_000_000_000 + 951_782_400 * 10_000_000).as_deref(),
            Some("2000-02-29T00:00:00Z")
        );
        assert_eq!(iso8601(i64::MAX), None);
    }

    #[test]
    fn the_extension_is_the_stored_one_else_the_filenames() {
        let unicode =
            |text: &str| Value::Unicode(text.encode_utf16().flat_map(u16::to_le_bytes).collect());
        let mut props = Bag::from([(tag::ATTACH_LONG_FILENAME, unicode("report.csv"))]);
        let mapped = attachment(&props, Some(b""), 1252);
        assert_eq!(mapped.extension.as_deref(), Some("csv"));
        props.insert(tag::ATTACH_EXTENSION, unicode(".txt"));
        let mapped = attachment(&props, Some(b""), 1252);
        assert_eq!(mapped.extension.as_deref(), Some("txt"));
        props.insert(tag::ATTACH_EXTENSION, unicode("JPG"));
        let mapped = attachment(&props, None, 1252);
        assert_eq!(mapped.extension.as_deref(), Some("JPG"));
        assert_eq!((mapped.record.file, mapped.record.size), (None, None));
    }

    #[test]
    fn an_unknown_attach_method_is_left_out_with_a_note() {
        let props = Bag::from([(tag::ATTACH_METHOD, Value::Integer(3))]);
        let mapped = attachment(&props, None, 1252);
        assert_eq!(mapped.record.method, None);
        assert_eq!(
            mapped.notes,
            ["property 0x3705 has unknown value 3; left out"]
        );
    }

    #[test]
    fn the_subject_prefix_marker_is_dropped() {
        assert_eq!(strip_subject_prefix("\u{1}\u{5}Re: Hi".into()), "Re: Hi");
        assert_eq!(strip_subject_prefix("Hi".into()), "Hi");
    }

    fn unicode(text: &str) -> Value {
        Value::Unicode(text.encode_utf16().flat_map(u16::to_le_bytes).collect())
    }

    /// PidTagRtfCompressed holding `rtf` uncompressed, as MS-OXRTFCP's "MELA" form stores it.
    fn mela(rtf: &str) -> Value {
        let mut bytes = Vec::new();
        let size = u32::try_from(rtf.len()).unwrap();
        for word in [size + 12, size, 0x414C_454D, 0] {
            bytes.extend_from_slice(&word.to_le_bytes());
        }
        bytes.extend_from_slice(rtf.as_bytes());
        Value::Binary(bytes)
    }

    /// The bodies, by the order the Markdown is made from them: stored HTML, the HTML from a
    /// `\fromhtml1` RTF, the text body, then the RTF's own text, read only when it is the one.
    #[test]
    fn the_markdown_is_made_from_the_first_body_in_order() {
        let bodies = |props: Vec<(u16, Value)>| {
            let (_, bodies, notes) = message(&Bag::from_iter(props), &[], 1252);
            assert_eq!(notes, Vec::<String>::new());
            (bodies.html, bodies.text)
        };
        let html_rtf = mela("{\\rtf1\\ansi\\fromhtml1 {\\*\\htmltag <p>}from rtf}");
        let plain_rtf = mela("{\\rtf1\\ansi plain\\par rtf}");
        let stored = (tag::HTML, Value::Binary(b"<p>stored</p>".to_vec()));
        let text = (tag::BODY, unicode("text"));
        assert_eq!(
            bodies(vec![
                stored.clone(),
                (tag::RTF_COMPRESSED, html_rtf.clone())
            ]),
            (Some("<p>stored</p>".into()), None)
        );
        assert_eq!(
            bodies(vec![(tag::RTF_COMPRESSED, html_rtf), text.clone()]),
            (Some("<p>from rtf".into()), Some("text".into()))
        );
        assert_eq!(
            bodies(vec![(tag::RTF_COMPRESSED, plain_rtf.clone()), text]),
            (None, Some("text".into()))
        );
        assert_eq!(
            bodies(vec![(tag::RTF_COMPRESSED, plain_rtf)]),
            (None, Some("plain\r\nrtf".into()))
        );
        assert_eq!(bodies(vec![]), (None, None));
    }
}
