//! The TNEF reader: the CLI's own MS-OXTNEF reader for a MIME message's TNEF part
//! (`winmail.dat`), Outlook's serialization of a message's MAPI properties and attachments. It
//! fills the same property bags the .pst and .msg readers fill, and they go through the one
//! mapping, so a TNEF part's bodies and attachments are what a .msg gives for the same properties.
//!
//! A TNEF stream is its signature, 0x223E9F78, a 2-byte legacy key, then attributes, each a level
//! byte (1, the message's, or 2, an attachment's), a 4-byte attribute id, a 4-byte length, that
//! many bytes of data, and a 2-byte checksum, the sum of the data's bytes modulo 65536 (MS-OXTNEF
//! 2.1.3.1). The attributes read are:
//! - attMsgProps (0x00069003), the message's MAPI properties, and attRecipTable (0x00069004), its
//!   recipients' rows, each a property list;
//! - attBody (0x0002800C) and attSubject (0x00018004), PidTagBody and PidTagSubject in the code
//!   page attOemCodepage (0x00069007) names, a property of attMsgProps taking precedence;
//! - attAttachRendData (0x00069002), which starts an attachment, and that attachment's
//!   attAttachData (0x0006800F), its content, attAttachTitle (0x00018010), its short filename, and
//!   attAttachment (0x00069005), its MAPI properties, which take precedence over the attributes.
//!
//! Every other attribute is passed over. A property list is a 4-byte count, then each property's
//! tag, a named property's GUID and its name or id, then its value: a fixed-size value in its own
//! size padded to 4 bytes (PT_SHORT and PT_BOOLEAN in 4), and a variable-size one, PT_STRING8,
//! PT_UNICODE, PT_BINARY, PT_OBJECT or PT_SVREID, as a 4-byte count of values, then each value's
//! 4-byte length and bytes, padded to 4 (MS-OXTNEF 2.1.3.4). A multi-valued property is a count of
//! values of its type. Named and multi-valued properties are read past and kept as nothing, since
//! no field reads one.
//!
//! An attachment's PidTagAttachDataObject (0x3701), as PT_OBJECT, is an embedded message when it
//! starts with IID_IMessage: the rest is the embedded message's own TNEF stream, read as this one
//! is, to [`DEPTH_MAX`] messages. One that can't be read, or nested deeper, is listed without its
//! message, with a note.
//!
//! The stream is untrusted, so every length and count is checked against the bytes that remain
//! before it is read, and a value's padding is skipped only as far as the data goes. A stream that
//! breaks its own framing, a bad signature, a length or count past its data, a property type of no
//! known size, or a level other than 1 or 2, can't be read, and the caller skips the TNEF part
//! whole. An attribute whose checksum doesn't match its data is still read, as its framing holds,
//! with a note: the data may have been altered. Fewer bytes after the last attribute than an
//! attribute's 9-byte header, as the CRLF some writers append, are not read, and give no note.

use crate::codepage::{message_code_page, msg_code_page};
use crate::msg::{DEPTH_MAX, PT_BINARY, PT_STRING8, PT_UNICODE, fixed, too_deep, variable};
use crate::props::{Bag, Value, tag};

const SIGNATURE: u32 = 0x223E_9F78;

const LEVEL_MESSAGE: u8 = 1;
const LEVEL_ATTACHMENT: u8 = 2;

const ATT_SUBJECT: u32 = 0x0001_8004;
const ATT_BODY: u32 = 0x0002_800C;
const ATT_ATTACH_DATA: u32 = 0x0006_800F;
const ATT_ATTACH_TITLE: u32 = 0x0001_8010;
const ATT_ATTACH_REND_DATA: u32 = 0x0006_9002;
const ATT_MSG_PROPS: u32 = 0x0006_9003;
const ATT_RECIP_TABLE: u32 = 0x0006_9004;
const ATT_ATTACHMENT: u32 = 0x0006_9005;
const ATT_OEM_CODEPAGE: u32 = 0x0006_9007;

const HEADER: usize = 9;

const MULTI_VALUED: u16 = 0x1000;
const NAMED: u16 = 0x8000;
const PT_OBJECT: u16 = 0x000D;
const PT_SVREID: u16 = 0x00FB;

const IID_IMESSAGE: [u8; 16] = [
    0x07, 0x03, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46,
];

/// A TNEF stream read: the message's properties, its recipients' rows and its attachments, the
/// code page attOemCodepage names, and the notes the stream gave.
#[derive(Debug, Default)]
pub struct Tnef {
    pub props: Bag,
    pub recipients: Vec<Bag>,
    pub attachments: Vec<Attached>,
    pub oem_code_page: Option<u16>,
    pub notes: Vec<String>,
}

impl Tnef {
    /// The code page the message's PT_STRING8 values are in: its PidTagMessageCodepage, else the
    /// one attOemCodepage names, else the one a .msg message's would be in with `parent` as its
    /// parent's ([`msg_code_page`]).
    pub fn code_page(&self, parent: Option<u16>) -> u16 {
        match self.oem_code_page {
            Some(oem) => message_code_page(&self.props, oem),
            None => msg_code_page(&self.props, parent),
        }
    }
}

/// One attachment of a TNEF stream: its properties, its content as PidTagAttachDataBinary among
/// them, and the message it embeds.
#[derive(Debug, Default)]
pub struct Attached {
    pub props: Bag,
    pub embedded: Embedded,
}

/// The message an attachment embeds.
#[derive(Debug, Default)]
pub enum Embedded {
    /// It embeds none.
    #[default]
    None,
    /// The embedded message, read.
    Read(Box<Tnef>),
    /// One that can't be read, or is nested too deep: the note it gives its attachment.
    Unread(String),
}

/// Reads a TNEF stream, the content of a part of a message nested `depth` messages deep, or says
/// why it can't be read.
pub fn read(bytes: &[u8], depth: usize) -> Result<Tnef, String> {
    let mut input = Input { bytes };
    let signature = input.u32("the signature")?;
    if signature != SIGNATURE {
        return Err(format!(
            "its signature is 0x{signature:08X}, not TNEF's 0x{SIGNATURE:08X}"
        ));
    }
    input.take(2, "the legacy key")?;
    let mut tnef = Tnef::default();
    let mut attributes = Bag::new();
    let mut attachment: Option<(Bag, Attached)> = None;
    while input.bytes.len() >= HEADER {
        let level = input.u8("an attribute's level")?;
        let id = input.u32("an attribute's id")?;
        let length = input.length("an attribute's length")?;
        let data = input.take(length, &format!("attribute 0x{id:08X}'s data"))?;
        let checksum = input.u16(&format!("attribute 0x{id:08X}'s checksum"))?;
        let sum = data
            .iter()
            .fold(0u16, |sum, byte| sum.wrapping_add(u16::from(*byte)));
        if sum != checksum {
            tnef.notes.push(format!(
                "the TNEF part's attribute 0x{id:08X} has checksum 0x{checksum:04X}, not its \
                 data's 0x{sum:04X}; read as stored"
            ));
        }
        match level {
            LEVEL_MESSAGE => message_attribute(&mut tnef, &mut attributes, id, data, depth)?,
            LEVEL_ATTACHMENT => {
                if id == ATT_ATTACH_REND_DATA
                    && let Some(done) = attachment.take()
                {
                    tnef.attachments.push(finish(done));
                }
                let (attributes, attached) = attachment.get_or_insert_default();
                attachment_attribute(attributes, attached, id, data, depth)?;
            }
            _ => return Err(format!("attribute 0x{id:08X} has unknown level {level}")),
        }
    }
    if let Some(done) = attachment {
        tnef.attachments.push(finish(done));
    }
    for (id, value) in attributes {
        tnef.props.entry(id).or_insert(value);
    }
    Ok(tnef)
}

fn finish((attributes, mut attached): (Bag, Attached)) -> Attached {
    for (id, value) in attributes {
        attached.props.entry(id).or_insert(value);
    }
    attached
}

fn message_attribute(
    tnef: &mut Tnef,
    attributes: &mut Bag,
    id: u32,
    data: &[u8],
    depth: usize,
) -> Result<(), String> {
    match id {
        ATT_MSG_PROPS => {
            let (props, _) = property_list(&mut Input { bytes: data }, depth)?;
            for (id, value) in props {
                tnef.props.entry(id).or_insert(value);
            }
        }
        ATT_RECIP_TABLE => {
            let mut input = Input { bytes: data };
            let rows = input.u32("the recipient table's row count")?;
            for _ in 0..rows {
                let (row, _) = property_list(&mut input, depth)?;
                tnef.recipients.push(row);
            }
        }
        ATT_OEM_CODEPAGE => {
            let code_page = Input { bytes: data }.u32("attOemCodepage")?;
            tnef.oem_code_page = u16::try_from(code_page).ok();
        }
        ATT_BODY => {
            attributes.insert(tag::BODY, variable(PT_STRING8, data.to_vec()));
        }
        ATT_SUBJECT => {
            attributes.insert(tag::SUBJECT, variable(PT_STRING8, data.to_vec()));
        }
        _ => {}
    }
    Ok(())
}

fn attachment_attribute(
    attributes: &mut Bag,
    attached: &mut Attached,
    id: u32,
    data: &[u8],
    depth: usize,
) -> Result<(), String> {
    match id {
        ATT_ATTACHMENT => {
            let (props, object) = property_list(&mut Input { bytes: data }, depth)?;
            for (id, value) in props {
                attached.props.entry(id).or_insert(value);
            }
            if let Some(embedded) = object {
                attached.embedded = embedded;
            }
        }
        ATT_ATTACH_DATA => {
            attributes.insert(tag::ATTACH_DATA, Value::Binary(data.to_vec()));
        }
        ATT_ATTACH_TITLE => {
            attributes.insert(tag::ATTACH_FILENAME, variable(PT_STRING8, data.to_vec()));
        }
        _ => {}
    }
    Ok(())
}

fn property_list(input: &mut Input<'_>, depth: usize) -> Result<(Bag, Option<Embedded>), String> {
    let count = input.u32("a property count")?;
    let mut bag = Bag::new();
    let mut embedded = None;
    for _ in 0..count {
        let property = input.u32("a property tag")?;
        let (id, kind) = ((property >> 16) as u16, property as u16);
        if id >= NAMED {
            input.take(16, "a named property's GUID")?;
            match input.u32("a named property's kind")? {
                0 => {
                    input.take(4, "a named property's id")?;
                }
                1 => {
                    let length = input.length("a named property's name length")?;
                    input.padded(length, "a named property's name")?;
                }
                other => return Err(format!("a named property has unknown kind {other}")),
            }
        }
        let base = kind & !MULTI_VALUED;
        let size = fixed_size(base);
        let variable_size = matches!(
            base,
            PT_STRING8 | PT_UNICODE | PT_BINARY | PT_OBJECT | PT_SVREID
        );
        if size.is_none() && !variable_size {
            return Err(format!(
                "property 0x{id:04X} has type 0x{kind:04X}, of no known size"
            ));
        }
        let values = if kind & MULTI_VALUED != 0 || variable_size {
            input.u32(&format!("property 0x{id:04X}'s value count"))?
        } else {
            1
        };
        let mut first = None;
        for _ in 0..values {
            let length = match size {
                Some(size) => size,
                None => input.length(&format!("property 0x{id:04X}'s value length"))?,
            };
            let value = input.padded(length, &format!("property 0x{id:04X}'s value"))?;
            first.get_or_insert(value);
        }
        let Some(value) = first else {
            continue;
        };
        if id >= NAMED || kind & MULTI_VALUED != 0 || bag.contains_key(&id) {
            continue;
        }
        if base == PT_OBJECT && id == tag::ATTACH_DATA {
            embedded = embedded_message(value, depth);
        }
        let value = match size {
            Some(_) => fixed(base, value).unwrap_or(Value::Other),
            None if base == PT_OBJECT => Value::Other,
            None => variable(base, value.to_vec()),
        };
        bag.insert(id, value);
    }
    Ok((bag, embedded))
}

fn embedded_message(object: &[u8], depth: usize) -> Option<Embedded> {
    let stream = object.strip_prefix(&IID_IMESSAGE)?;
    if depth + 1 > DEPTH_MAX {
        return Some(Embedded::Unread(too_deep()));
    }
    Some(match read(stream, depth + 1) {
        Ok(tnef) => Embedded::Read(Box::new(tnef)),
        Err(reason) => Embedded::Unread(format!(
            "the embedded message can't be read ({reason}); listed without it"
        )),
    })
}

fn fixed_size(kind: u16) -> Option<usize> {
    match kind {
        0x0002 | 0x0003 | 0x0004 | 0x000A | 0x000B => Some(4),
        0x0005 | 0x0006 | 0x0007 | 0x0014 | 0x0040 => Some(8),
        0x0048 => Some(16),
        _ => None,
    }
}

struct Input<'a> {
    bytes: &'a [u8],
}

impl<'a> Input<'a> {
    fn take(&mut self, length: usize, what: &str) -> Result<&'a [u8], String> {
        let Some((head, rest)) = self.bytes.split_at_checked(length) else {
            return Err(format!(
                "{what} is {length} bytes, past the {} that remain",
                self.bytes.len()
            ));
        };
        self.bytes = rest;
        Ok(head)
    }

    fn padded(&mut self, length: usize, what: &str) -> Result<&'a [u8], String> {
        let value = self.take(length, what)?;
        let padding = length.wrapping_neg() % 4;
        self.bytes = self.bytes.get(padding..).unwrap_or_default();
        Ok(value)
    }

    fn u8(&mut self, what: &str) -> Result<u8, String> {
        let [byte] = *self.array::<1>(what)?;
        Ok(byte)
    }

    fn u16(&mut self, what: &str) -> Result<u16, String> {
        Ok(u16::from_le_bytes(*self.array(what)?))
    }

    fn u32(&mut self, what: &str) -> Result<u32, String> {
        Ok(u32::from_le_bytes(*self.array(what)?))
    }

    fn length(&mut self, what: &str) -> Result<usize, String> {
        let length = self.u32(what)?;
        usize::try_from(length).map_err(|_| format!("{what} {length} is past this host's reach"))
    }

    fn array<const N: usize>(&mut self, what: &str) -> Result<&'a [u8; N], String> {
        let bytes = self.take(N, what)?;
        bytes
            .try_into()
            .map_err(|_| format!("{what} is short of its {N} bytes"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn attribute(level: u8, id: u32, data: &[u8]) -> Vec<u8> {
        let sum = data
            .iter()
            .fold(0u16, |sum, byte| sum.wrapping_add(u16::from(*byte)));
        attribute_with(level, id, data, sum)
    }

    fn attribute_with(level: u8, id: u32, data: &[u8], checksum: u16) -> Vec<u8> {
        let mut bytes = vec![level];
        bytes.extend_from_slice(&id.to_le_bytes());
        bytes.extend_from_slice(&u32::try_from(data.len()).unwrap().to_le_bytes());
        bytes.extend_from_slice(data);
        bytes.extend_from_slice(&checksum.to_le_bytes());
        bytes
    }

    fn stream(attributes: &[Vec<u8>]) -> Vec<u8> {
        let mut bytes = SIGNATURE.to_le_bytes().to_vec();
        bytes.extend_from_slice(&[1, 0]);
        for attribute in attributes {
            bytes.extend_from_slice(attribute);
        }
        bytes
    }

    fn padded(bytes: &[u8]) -> Vec<u8> {
        let mut bytes = bytes.to_vec();
        bytes.resize(bytes.len().next_multiple_of(4), 0);
        bytes
    }

    fn variable_property(property: u32, value: &[u8]) -> Vec<u8> {
        let mut bytes = property.to_le_bytes().to_vec();
        bytes.extend_from_slice(&1u32.to_le_bytes());
        bytes.extend_from_slice(&u32::try_from(value.len()).unwrap().to_le_bytes());
        bytes.extend_from_slice(&padded(value));
        bytes
    }

    fn list(properties: &[Vec<u8>]) -> Vec<u8> {
        let mut bytes = u32::try_from(properties.len())
            .unwrap()
            .to_le_bytes()
            .to_vec();
        for property in properties {
            bytes.extend_from_slice(property);
        }
        bytes
    }

    fn utf16(text: &str) -> Vec<u8> {
        text.encode_utf16().flat_map(u16::to_le_bytes).collect()
    }

    #[test]
    fn message_and_attachment_attributes_fill_their_bags() {
        let long = variable_property(0x3707_001F, &utf16("a.txt"));
        let method = [0x3705_0003u32.to_le_bytes(), 1u32.to_le_bytes()].concat();
        let named = [
            0x8001_0003u32.to_le_bytes().to_vec(),
            vec![0; 16],
            0u32.to_le_bytes().to_vec(),
            7u32.to_le_bytes().to_vec(),
            5u32.to_le_bytes().to_vec(),
        ]
        .concat();
        let bytes = stream(&[
            attribute(1, ATT_OEM_CODEPAGE, &[0xE4, 0x04, 0, 0, 0, 0, 0, 0]),
            attribute(1, ATT_SUBJECT, b"Old\0"),
            attribute(1, ATT_BODY, b"Body\0"),
            attribute(
                1,
                ATT_MSG_PROPS,
                &list(&[variable_property(0x0037_001E, b"New\0"), named]),
            ),
            attribute(2, ATT_ATTACH_REND_DATA, &[0; 14]),
            attribute(2, ATT_ATTACH_TITLE, b"A~1.TXT\0"),
            attribute(2, ATT_ATTACH_DATA, b"one"),
            attribute(2, ATT_ATTACHMENT, &list(&[long, method])),
            attribute(2, ATT_ATTACH_REND_DATA, &[0; 14]),
            attribute(2, ATT_ATTACH_DATA, b"two"),
        ]);
        let tnef = read(&[bytes, b"\r\n".to_vec()].concat(), 0).unwrap();
        assert_eq!(tnef.notes, Vec::<String>::new());
        assert_eq!(tnef.oem_code_page, Some(1252));
        assert_eq!(tnef.code_page(None), 1252);
        assert_eq!(
            tnef.props,
            Bag::from([
                (tag::SUBJECT, Value::String8(b"New".to_vec())),
                (tag::BODY, Value::String8(b"Body".to_vec())),
            ])
        );
        let [first, second] = &tnef.attachments[..] else {
            panic!("{:?}", tnef.attachments);
        };
        assert_eq!(
            first.props,
            Bag::from([
                (tag::ATTACH_DATA, Value::Binary(b"one".to_vec())),
                (tag::ATTACH_FILENAME, Value::String8(b"A~1.TXT".to_vec())),
                (tag::ATTACH_METHOD, Value::Integer(1)),
                (tag::ATTACH_LONG_FILENAME, Value::Unicode(utf16("a.txt"))),
            ])
        );
        assert_eq!(
            second.props,
            Bag::from([(tag::ATTACH_DATA, Value::Binary(b"two".to_vec()))])
        );
    }

    #[test]
    fn a_checksum_mismatch_is_read_with_a_note() {
        let bytes = stream(&[attribute_with(1, ATT_BODY, b"Body", 1)]);
        let tnef = read(&bytes, 0).unwrap();
        assert_eq!(tnef.props[&tag::BODY], Value::String8(b"Body".to_vec()));
        assert_eq!(
            tnef.notes,
            [
                "the TNEF part's attribute 0x0002800C has checksum 0x0001, not its data's \
              0x018E; read as stored"
            ]
        );
    }

    #[test]
    fn a_stream_that_breaks_its_framing_is_unreadable() {
        let whole = stream(&[attribute(1, ATT_BODY, b"Body")]);
        assert_eq!(
            read(b"\x78\x9f\x3e\x23\x01\x00", 0).unwrap_err(),
            "its signature is 0x233E9F78, not TNEF's 0x223E9F78"
        );
        assert_eq!(
            read(&whole[..whole.len() - 3], 0).unwrap_err(),
            "attribute 0x0002800C's data is 4 bytes, past the 3 that remain"
        );
        assert_eq!(
            read(&stream(&[attribute(3, ATT_BODY, b"x")]), 0).unwrap_err(),
            "attribute 0x0002800C has unknown level 3"
        );
        let huge = [u32::MAX.to_le_bytes(), 0x0037_001Fu32.to_le_bytes()].concat();
        assert_eq!(
            read(&stream(&[attribute(1, ATT_MSG_PROPS, &huge)]), 0).unwrap_err(),
            "property 0x0037's value count is 4 bytes, past the 0 that remain"
        );
        let unknown = list(&[0x0037_0099u32.to_le_bytes().to_vec()]);
        assert_eq!(
            read(&stream(&[attribute(1, ATT_MSG_PROPS, &unknown)]), 0).unwrap_err(),
            "property 0x0037 has type 0x0099, of no known size"
        );
        let long = [
            1u32.to_le_bytes(),
            0x1000_0102u32.to_le_bytes(),
            1u32.to_le_bytes(),
            u32::MAX.to_le_bytes(),
        ]
        .concat();
        assert_eq!(
            read(&stream(&[attribute(1, ATT_MSG_PROPS, &long)]), 0).unwrap_err(),
            "property 0x1000's value is 4294967295 bytes, past the 0 that remain"
        );
    }

    fn object(tnef: &[u8]) -> Vec<u8> {
        variable_property(0x3701_000D, &[&IID_IMESSAGE[..], tnef].concat())
    }

    fn embedding(inner: &[u8]) -> Vec<u8> {
        let method = [0x3705_0003u32.to_le_bytes(), 5u32.to_le_bytes()].concat();
        stream(&[
            attribute(2, ATT_ATTACH_REND_DATA, &[0; 14]),
            attribute(2, ATT_ATTACHMENT, &list(&[method, object(inner)])),
        ])
    }

    #[test]
    fn an_attachment_data_object_is_an_embedded_message() {
        let inner = stream(&[attribute(
            1,
            ATT_MSG_PROPS,
            &list(&[variable_property(0x0037_001E, b"Inner\0")]),
        )]);
        let tnef = read(&embedding(&inner), 0).unwrap();
        let Embedded::Read(embedded) = &tnef.attachments[0].embedded else {
            panic!("{:?}", tnef.attachments[0].embedded);
        };
        assert_eq!(
            embedded.props,
            Bag::from([(tag::SUBJECT, Value::String8(b"Inner".to_vec()))])
        );
        assert_eq!(tnef.attachments[0].props[&tag::ATTACH_DATA], Value::Other);

        let tnef = read(&embedding(b"not tnef"), 0).unwrap();
        let Embedded::Unread(note) = &tnef.attachments[0].embedded else {
            panic!("{:?}", tnef.attachments[0].embedded);
        };
        assert!(note.starts_with("the embedded message can't be read (its signature is "));

        let mut nested = inner;
        for _ in 0..=DEPTH_MAX {
            nested = embedding(&nested);
        }
        let mut tnef = read(&nested, 0).unwrap();
        for _ in 0..DEPTH_MAX {
            let Embedded::Read(inner) = tnef.attachments.remove(0).embedded else {
                panic!("read to {DEPTH_MAX} levels");
            };
            tnef = *inner;
        }
        let Embedded::Unread(note) = &tnef.attachments[0].embedded else {
            panic!("{:?}", tnef.attachments[0].embedded);
        };
        assert_eq!(note, &too_deep());
    }
}
