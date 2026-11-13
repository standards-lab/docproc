//! Reading .msg files: what Tika's and Apache POI's own tests assert of the committed public
//! messages (tests/golden.rs holds their goldens), and messages built in the test for the
//! reader's rules, its code pages, nesting, afStorage, and the skip policy.

mod common;

use common::msg::{Attachment, Child, Message, Object, Prop, build};
use common::{fixture, names, run, scratch, sha256};
use serde_json::{Value, json};

/// The committed public messages, by path under tests/fixtures/msg.
const MESSAGES: &[&str] = &[
    "tika/testMSG_att_msg.msg",
    "tika/testMSG_chinese.msg",
    "tika/testMSG_forwarded.msg",
    "tika/test-outlook2003.msg",
    "poi/ASCII_UTF-8_CP1252_LCID1031_HTML.msg",
    "poi/example_sent_regular.msg",
    "poi/msgClassAppointment.msg",
    "poi/outlook_30_msg.msg",
];

/// The committed messages from one source, by its directory.
fn from(source: &str) -> Vec<&'static str> {
    let mut messages: Vec<&str> = MESSAGES
        .iter()
        .copied()
        .filter(|path| path.starts_with(&format!("{source}/")))
        .collect();
    messages.sort_unstable();
    messages
}

/// The files a truth file writes truth for, sorted.
fn truth_files(truth: &Value) -> Vec<&str> {
    let mut files: Vec<&str> = truth["messages"]
        .as_object()
        .unwrap()
        .keys()
        .map(String::as_str)
        .collect();
    files.sort_unstable();
    files
}

fn message(path: &str) -> Value {
    let out = run(&[fixture(&format!("msg/{path}"))]);
    assert_eq!(out.code, Some(0), "{path}: {}", out.stderr);
    out.lines().remove(1)
}

/// The same input always gives byte-identical output.
#[test]
fn output_is_byte_identical_across_runs() {
    for path in MESSAGES {
        let path = fixture(&format!("msg/{path}"));
        assert_eq!(
            run(&[&path]).stdout,
            run(&[&path]).stdout,
            "{}",
            path.display()
        );
    }
}

/// A .msg is a store line of format "msg" and one message line; no field is null, and a nested
/// message, which is no line, has no "line".
#[test]
fn a_msg_is_a_store_line_and_a_message_line() {
    for path in MESSAGES {
        let lines = run(&[fixture(&format!("msg/{path}"))]).lines();
        assert_eq!(lines.len(), 2, "{path}");
        assert_eq!(
            lines[0],
            json!({"line": "store", "schema": "2", "format": "msg"}),
            "{path}"
        );
        assert_eq!(lines[1]["line"], "message", "{path}");
        assert_nested(&lines[1], path);
    }
}

fn assert_nested(message: &Value, path: &str) {
    for attachment in message["attachments"].as_array().into_iter().flatten() {
        if let Some(nested) = attachment.get("message") {
            assert!(nested.get("line").is_none(), "{path}: a nested line");
            assert_nested(nested, path);
        }
    }
    assert!(!message.to_string().contains("null"), "{path}: a null");
}

/// Tika's testMSG_att_msg.msg carries a message and a PDF. The message is nested under its
/// attachment record with every field a message line has, its bodies included, as Tika's
/// OutlookParserTest reads it: "Test Attachment", "Blah blah blah".
#[test]
fn an_embedded_message_is_nested_under_its_attachment() {
    let message = message("tika/testMSG_att_msg.msg");
    assert_eq!(message["subject"], "test email");
    let attachments = message["attachments"].as_array().unwrap();
    assert_eq!(attachments.len(), 2);
    let embedded = &attachments[0];
    assert_eq!(embedded["method"], "embedded_message");
    assert_eq!(embedded["filename"], "Test Attachment");
    assert!(embedded.get("file").is_none() && embedded.get("size").is_none());
    let nested = &embedded["message"];
    assert_eq!(nested["class"], "IPM.Note");
    assert_eq!(nested["subject"], "Test Attachment");
    assert_eq!(nested["sender"]["name"], "Nick Booth");
    assert_eq!(nested["to"][0]["name"], "Nick Booth");
    assert_eq!(nested["submit_time"], "2010-06-17T23:52:19Z");
    assert_eq!(
        nested["message_id"],
        "<3975CB0784A2C249A2EDDD4D43C53D08045B971A82@Stitch.pof.com.au>"
    );
    assert_eq!(nested["markdown"], "Blah blah blah");
    assert_eq!(attachments[1]["filename"], "smbprn.00009008.KdcPjl.pdf");
    assert_eq!(attachments[1]["method"], "by_value");
    assert_eq!(attachments[1]["size"], 13539);
}

/// Tika's testMSG_chinese.msg is ANSI: its strings are PT_STRING8 in a Chinese code page, and
/// read in it they are what Tika's test asserts.
#[test]
fn an_ansi_message_is_decoded_by_its_code_page() {
    let message = message("tika/testMSG_chinese.msg");
    assert_eq!(
        message["subject"],
        "Alfresco MSG format testing ( MSG 格式測試 )"
    );
    assert_eq!(message["sender"]["name"], "Tests Chang@FT (張毓倫)");
    let markdown = message["markdown"].as_str().unwrap();
    assert!(
        markdown.starts_with("Alfresco MSG format testing ( MSG 格式測試 ) 中文測試  \n"),
        "{markdown}"
    );
}

/// Tika's testMSG_forwarded.msg stores its body as RTF that encapsulates HTML (\fromhtml1) and as
/// plain text, with no HTML: the HTML is recovered from the RTF, and the Markdown made from it,
/// not from the text, so it has the HTML's bold header names. Its threading fields are as stored.
#[test]
fn html_is_recovered_from_a_fromhtml1_rtf() {
    let message = message("tika/testMSG_forwarded.msg");
    let markdown = message["markdown"].as_str().unwrap();
    assert!(markdown.starts_with("Forwarded Body\n"), "{markdown}");
    assert!(markdown.contains("**From:** Paul Allan Hill"), "{markdown}");
    assert!(markdown.ends_with("\nOriginal Body"), "{markdown}");
    let parent =
        "<C8508767C15DBF40A21693142739EA8D564D18FDA1@EXVMBX018-1.exch018.msoutlookonline.net>";
    assert_eq!(message["in_reply_to"], parent);
    assert_eq!(message["references"], parent);
    assert_eq!(message["subject"], "FW: Testing");
    assert_eq!(message["conversation_topic"], "Testing");
    assert!(message["conversation_index"].is_string());
}

/// Tika's test-outlook2003.msg carries eleven pictures, each written once by content and each
/// linked by the Markdown, so inline.
#[test]
fn attachments_are_written_by_content() {
    let dir = scratch("msg-attachments");
    let file = fixture("msg/tika/test-outlook2003.msg");
    let out = run(&[file.as_os_str(), "--media".as_ref(), dir.as_os_str()]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    assert_eq!(out.stdout, run(&[&file]).stdout);
    let attachments = out.lines()[1]["attachments"].as_array().unwrap().clone();
    assert_eq!(attachments.len(), 11);
    assert!(attachments.iter().all(|a| a["inline"] == true));
    let mut expected: Vec<String> = attachments
        .iter()
        .map(|a| a["file"].as_str().unwrap().to_owned())
        .collect();
    expected.sort_unstable();
    assert_eq!(names(&dir), expected);
    for name in expected {
        let (sha, _) = name.split_once('.').unwrap();
        assert_eq!(sha256(&dir.join(&name)), sha, "{name}");
    }
}

/// Every committed message has a sources entry whose SHA-256 matches it, and the license
/// notices it names are committed.
#[test]
fn every_message_has_a_matching_sources_entry() {
    let sources: Value =
        serde_json::from_slice(&std::fs::read(fixture("msg/sources.json")).unwrap()).unwrap();
    let entries = sources.as_array().unwrap();
    let mut listed: Vec<&str> = entries
        .iter()
        .map(|e| e["file"].as_str().unwrap())
        .collect();
    listed.sort_unstable();
    let mut messages = MESSAGES.to_vec();
    messages.sort_unstable();
    assert_eq!(listed, messages);
    for entry in entries {
        for field in ["repository", "commit", "path", "license", "sha256"] {
            assert!(entry[field].is_string(), "{}: {field}", entry["file"]);
        }
        let path = fixture(&format!("msg/{}", entry["file"].as_str().unwrap()));
        assert_eq!(
            Some(sha256(&path).as_str()),
            entry["sha256"].as_str(),
            "{}",
            path.display()
        );
    }
    for notice in ["tika/LICENSE", "tika/NOTICE", "poi/LICENSE", "poi/NOTICE"] {
        assert!(fixture(&format!("msg/{notice}")).is_file(), "{notice}");
    }
}

/// What Tika's OutlookParserTest asserts of its .msg files, as tika-truth.json writes it, is in
/// the CLI's message lines: every field but one. testMSG_chinese.msg's To recipient is an
/// Exchange recipient, and the CLI writes its address as stored (PidTagEmailAddress), where Tika
/// reports its SMTP address (PidTagSmtpAddress), which the CLI doesn't write. The findings run
/// scores the same file (docs/findings/mail-cli.md).
#[test]
fn fields_match_the_tika_truth() {
    let truth: Value =
        serde_json::from_slice(&std::fs::read(fixture("msg/tika-truth.json")).unwrap()).unwrap();
    assert_eq!(
        truth_files(&truth),
        from("tika"),
        "every committed Tika message has its truth"
    );
    let mut checked = 0;
    for (path, entry) in truth["messages"].as_object().unwrap() {
        let got = message(path);
        for (pointer, want) in entry["fields"].as_object().unwrap() {
            if (path.as_str(), pointer.as_str()) == ("tika/testMSG_chinese.msg", "/to/0/address") {
                assert_eq!(got["to"][0]["address_type"], "EX");
                assert_eq!(
                    got.pointer(pointer),
                    Some(&json!("/O=FT GROUP/OU=FT/CN=RECIPIENTS/CTestsiaChang"))
                );
                continue;
            }
            assert_eq!(got.pointer(pointer), Some(want), "{path}: {pointer}");
            checked += 1;
        }
    }
    assert_eq!(checked, 21);
}

/// What Apache POI's HSMF tests assert of its .msg files, as poi-truth.json writes it, is in the
/// CLI's message lines: equal fields, and the lengths of the recipient lists. Its assertions on
/// the stored bodies, written as pointers under /body (equal values and Java String.hashCodes),
/// name a field schema "2" cuts: the message's Markdown is made from the bodies, and no body is
/// written, so those are passed over, and counted.
#[test]
fn fields_match_the_poi_truth() {
    let truth: Value =
        serde_json::from_slice(&std::fs::read(fixture("msg/poi-truth.json")).unwrap()).unwrap();
    assert_eq!(
        truth_files(&truth),
        from("poi"),
        "every committed POI message has its truth"
    );
    let (mut checked, mut bodies) = (0, 0);
    for (path, entry) in truth["messages"].as_object().unwrap() {
        let got = message(path);
        assert_eq!(got.get("body"), None, "{path}: body");
        let fields = entry["fields"].as_object().unwrap();
        let hashes = entry["java_hashes"].as_object().into_iter().flatten();
        for (pointer, _) in fields.iter().chain(hashes) {
            if pointer.starts_with("/body/") {
                bodies += 1;
            }
        }
        for (pointer, want) in fields {
            if pointer.starts_with("/body/") {
                continue;
            }
            assert_eq!(got.pointer(pointer), Some(want), "{path}: {pointer}");
            checked += 1;
        }
        for (pointer, want) in entry["java_hashes"].as_object().into_iter().flatten() {
            if pointer.starts_with("/body/") {
                continue;
            }
            let text = got.pointer(pointer).and_then(Value::as_str);
            assert_eq!(
                text.map(java_hash),
                want.as_i64().map(|hash| i32::try_from(hash).unwrap()),
                "{path}: {pointer}"
            );
            checked += 1;
        }
        for (pointer, want) in entry["lengths"].as_object().into_iter().flatten() {
            let len = got.pointer(pointer).and_then(Value::as_array).map(Vec::len);
            assert_eq!(
                len.map(|len| len as u64),
                want.as_u64(),
                "{path}: {pointer}"
            );
            checked += 1;
        }
    }
    assert_eq!((checked, bodies), (27, 4));
}

/// Java's String.hashCode: s[0]*31^(n-1) + ... + s[n-1] over UTF-16 code units, wrapping.
fn java_hash(text: &str) -> i32 {
    text.encode_utf16().fold(0i32, |hash, unit| {
        hash.wrapping_mul(31).wrapping_add(i32::from(unit))
    })
}

// Messages built in the test.

const SUBJECT: u16 = 0x0037;
const MESSAGE_CODEPAGE: u16 = 0x3FFD;
const INTERNET_CODEPAGE: u16 = 0x3FDE;
const DISPLAY_NAME: u16 = 0x3001;
const RECIPIENT_TYPE: u16 = 0x0C15;
const ATTACH_DATA: u16 = 0x3701;
const ATTACH_METHOD: u16 = 0x3705;
const BODY: u16 = 0x1000;
const RTF_COMPRESSED: u16 = 0x1009;
const HTML: u16 = 0x1013;
const SENDER_NAME: u16 = 0x0C1A;
const SENDER_ADDRESS_TYPE: u16 = 0x0C1E;
const SENDER_EMAIL_ADDRESS: u16 = 0x0C1F;
const SENDER_SMTP_ADDRESS: u16 = 0x5D01;
const ADDRESS_TYPE: u16 = 0x3002;
const EMAIL_ADDRESS: u16 = 0x3003;
const SMTP_ADDRESS: u16 = 0x39FE;
const ATTACH_CONTENT_ID: u16 = 0x3712;
const ATTACH_CONTENT_LOCATION: u16 = 0x3713;
const ATTACH_FLAGS: u16 = 0x3714;
const ATTACH_LONG_FILENAME: u16 = 0x3707;

fn subject(text: &str) -> Vec<(u16, Prop)> {
    vec![(SUBJECT, Prop::Unicode(text.into()))]
}

fn by_value(name: &str, content: &[u8]) -> Attachment {
    Attachment {
        props: Child::Props(vec![
            (ATTACH_METHOD, Prop::Long(1)),
            (ATTACH_LONG_FILENAME, Prop::Unicode(name.into())),
            (ATTACH_DATA, Prop::Binary(content.to_vec())),
        ]),
        object: None,
    }
}

fn embedded(message: Message) -> Attachment {
    Attachment {
        props: Child::Props(vec![(ATTACH_METHOD, Prop::Long(5))]),
        object: Some(Object::Message(message)),
    }
}

/// PT_STRING8 is read in the message's code page: PidTagMessageCodepage, else
/// PidTagInternetCodepage, else, in an embedded message, its parent's, else 1252.
#[test]
fn string8_is_read_in_the_messages_code_page() {
    // "Привет" in windows-1251, and "Grüße" in windows-1252.
    let cyrillic = b"\xCF\xF0\xE8\xE2\xE5\xF2".to_vec();
    let latin = b"Gr\xFC\xDFe".to_vec();
    let inherits = Message {
        props: vec![(SUBJECT, Prop::String8(cyrillic.clone()))],
        ..Message::default()
    };
    let msg = build(
        "code-pages.msg",
        &Message {
            props: vec![
                (SUBJECT, Prop::String8(cyrillic.clone())),
                (INTERNET_CODEPAGE, Prop::Long(1251)),
            ],
            attachments: vec![
                embedded(inherits),
                embedded(Message {
                    props: vec![
                        (SUBJECT, Prop::String8(latin.clone())),
                        (MESSAGE_CODEPAGE, Prop::Long(1252)),
                        (INTERNET_CODEPAGE, Prop::Long(1251)),
                    ],
                    ..Message::default()
                }),
            ],
            ..Message::default()
        },
    );
    let out = run(&[&msg]);
    assert_eq!(out.stderr, "");
    let message = &out.lines()[1];
    assert_eq!(message["subject"], "Привет");
    assert_eq!(message["attachments"][0]["message"]["subject"], "Привет");
    assert_eq!(message["attachments"][1]["message"]["subject"], "Grüße");
    let plain = build(
        "code-page-default.msg",
        &Message {
            props: vec![(SUBJECT, Prop::String8(latin))],
            ..Message::default()
        },
    );
    assert_eq!(run(&[&plain]).lines()[1]["subject"], "Grüße");
}

/// A nested message has its own recipients and attachments, recursively, and its attachments'
/// files are written by content like any other.
#[test]
fn nested_attachments_are_written_by_content() {
    let inner = Message {
        props: subject("inner"),
        attachments: vec![by_value("deep.txt", b"deep content")],
        ..Message::default()
    };
    let middle = Message {
        props: subject("middle"),
        recipients: vec![Child::Props(vec![
            (DISPLAY_NAME, Prop::Unicode("Ada".into())),
            (RECIPIENT_TYPE, Prop::Long(2)),
        ])],
        attachments: vec![embedded(inner), by_value("mid.txt", b"middle content")],
    };
    let msg = build(
        "nested.msg",
        &Message {
            props: subject("outer"),
            attachments: vec![embedded(middle)],
            ..Message::default()
        },
    );
    let dir = scratch("msg-nested-attachments");
    let out = run(&[msg.as_os_str(), "--media".as_ref(), dir.as_os_str()]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    assert_eq!(out.stderr, "");
    let middle = &out.lines()[1]["attachments"][0]["message"];
    assert_eq!(middle["subject"], "middle");
    assert_eq!(middle["cc"], json!([{"name": "Ada"}]));
    let inner = &middle["attachments"][0]["message"];
    assert_eq!(inner["subject"], "inner");
    let deep = &inner["attachments"][0];
    let mid = &middle["attachments"][1];
    let mut files = vec![
        deep["file"].as_str().unwrap().to_owned(),
        mid["file"].as_str().unwrap().to_owned(),
    ];
    files.sort_unstable();
    assert_eq!(names(&dir), files);
    assert_eq!(
        std::fs::read(dir.join(deep["file"].as_str().unwrap())).unwrap(),
        b"deep content"
    );
}

/// Embedded messages are read 16 deep; one nested deeper is listed without its message, with a
/// warning.
#[test]
fn nesting_is_capped_at_16() {
    let mut message = Message {
        props: subject("17"),
        ..Message::default()
    };
    for depth in (0..17).rev() {
        message = Message {
            props: subject(&depth.to_string()),
            attachments: vec![embedded(message)],
            ..Message::default()
        };
    }
    let out = run(&[build("deep.msg", &message)]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    let mut place = "message".to_owned();
    let mut at = out.lines().remove(1);
    for depth in 0..16 {
        assert_eq!(at["subject"], depth.to_string());
        place.push_str(", attachment 0");
        at = at["attachments"][0]["message"].take();
        place.push_str(", message");
    }
    assert_eq!(at["subject"], "16");
    let last = &at["attachments"][0];
    assert_eq!(last["method"], "embedded_message");
    assert!(last.get("message").is_none());
    assert_eq!(
        out.stderr,
        format!(
            "docproc-mail: warning: {place}, attachment 0: an embedded message nested \
             deeper than 16 messages; listed without it\n"
        )
    );
}

/// An attachment that can't be read is a skipped line after the message line, with a warning;
/// inside an embedded message, which has no line, it is left out with a warning. A recipient that
/// can't be read is left out with a warning, and an embedded message that can't be read leaves
/// its attachment listed without it. The run exits 0.
#[test]
fn records_that_cant_be_read_are_skipped() {
    let short = vec![0; 4];
    let nested = Message {
        props: subject("nested"),
        attachments: vec![Attachment {
            props: Child::Raw(short.clone()),
            object: None,
        }],
        ..Message::default()
    };
    let msg = build(
        "unreadable.msg",
        &Message {
            props: subject("outer"),
            recipients: vec![
                Child::Raw(short.clone()),
                Child::Props(vec![
                    (DISPLAY_NAME, Prop::Unicode("Ada".into())),
                    (RECIPIENT_TYPE, Prop::Long(1)),
                ]),
            ],
            attachments: vec![
                by_value("first.txt", b"first"),
                Attachment {
                    props: Child::Raw(short.clone()),
                    object: None,
                },
                embedded(nested),
                Attachment {
                    props: Child::Props(vec![(ATTACH_METHOD, Prop::Long(5))]),
                    object: Some(Object::Raw(short)),
                },
            ],
        },
    );
    let out = run(&[&msg]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    let lines = out.lines();
    let short = "the property stream is 4 bytes, short of its 8-byte header";
    let message = &lines[1];
    assert_eq!(message["to"], json!([{"name": "Ada"}]));
    let attachments = message["attachments"].as_array().unwrap();
    assert_eq!(attachments.len(), 3);
    assert_eq!(attachments[0]["filename"], "first.txt");
    assert_eq!(attachments[1]["message"]["subject"], "nested");
    assert!(attachments[1]["message"].get("attachments").is_none());
    assert_eq!(
        attachments[2],
        json!({"inline": false, "method": "embedded_message"})
    );
    assert_eq!(
        lines[2..],
        [json!({"line": "skipped", "kind": "attachment", "id": 1, "reason": short})]
    );
    let warning =
        |place: &str, what: &str| format!("docproc-mail: warning: message{place}: {what}\n");
    assert_eq!(
        out.stderr,
        [
            warning(", recipient 0", &format!("skipped: {short}")),
            warning(", attachment 1", &format!("skipped: {short}")),
            warning(
                ", attachment 2, message, attachment 0",
                &format!("skipped: {short}")
            ),
            warning(
                ", attachment 3",
                "the embedded message can't be read (the property stream is 4 bytes, short of \
                 its 24-byte header); listed without it"
            ),
        ]
        .concat()
    );
}

/// An afStorage attachment's storage is written as stored: a compound file of its own holding
/// its streams, the same bytes on every run.
#[test]
fn an_af_storage_attachment_is_written_as_a_compound_file() {
    let msg = build(
        "storage.msg",
        &Message {
            props: subject("storage"),
            attachments: vec![Attachment {
                props: Child::Props(vec![
                    (ATTACH_METHOD, Prop::Long(6)),
                    (ATTACH_LONG_FILENAME, Prop::Unicode("object.bin".into())),
                ]),
                object: Some(Object::Storage(vec![(
                    "Contents",
                    b"stored bytes".to_vec(),
                )])),
            }],
            ..Message::default()
        },
    );
    let dir = scratch("msg-storage");
    let out = run(&[msg.as_os_str(), "--media".as_ref(), dir.as_os_str()]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    assert_eq!(out.stderr, "");
    assert_eq!(out.stdout, run(&[&msg]).stdout);
    let record = &out.lines()[1]["attachments"][0];
    assert_eq!(record["method"], "storage");
    let file = dir.join(record["file"].as_str().unwrap());
    let mut storage = cfb::open(&file).unwrap();
    let mut contents = Vec::new();
    std::io::Read::read_to_end(
        &mut storage.open_stream("/Contents").unwrap(),
        &mut contents,
    )
    .unwrap();
    assert_eq!(contents, b"stored bytes");
}

/// PidTagRtfCompressed holding `rtf`, compressed as Outlook stores it.
fn rtf(rtf: &str) -> Prop {
    Prop::Binary(compressed_rtf::compress_rtf(rtf).unwrap())
}

/// The message line of the built message `message`, with its stderr, run with no media directory.
fn read_built(name: &str, message: &Message) -> (Value, String) {
    let out = run(&[build(name, message)]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    (out.lines()[1].clone(), out.stderr)
}

/// A message whose only body is plain RTF has the RTF's own text as its Markdown, read as a text
/// body is: control words, the font table and ignorable groups stripped, `\par` and `\line` as
/// line breaks, a symbol word as its character, and `\'hh` in the `\ansicpg` code page.
#[test]
fn a_plain_rtf_only_message_has_the_rtfs_text() {
    let mut props = subject("plain rtf");
    props.push((
        RTF_COMPRESSED,
        rtf(
            "{\\rtf1\\ansi\\ansicpg1251\\deff0{\\fonttbl{\\f0\\fswiss Arial;}}\
             {\\*\\generator Riched20;}\\pard\\f0\\fs20 \\'cf\\'f0\\'e8\\'e2\\'e5\\'f2, *all*\\par \
             Second\\line line\\rquote s end\\emdash done\\par}",
        ),
    ));
    let (message, stderr) = read_built(
        "plain-rtf.msg",
        &Message {
            props,
            ..Message::default()
        },
    );
    assert_eq!(stderr, "");
    assert_eq!(
        message["markdown"],
        "Привет, \\*all\\*  \nSecond  \nline\u{2019}s end\u{2014}done"
    );
}

/// A `\fromtext` RTF is read as plain RTF is, its `\*\htmltag` groups skipped, when the message
/// has no text body; with one, the Markdown is made from the text body, which comes first.
#[test]
fn a_fromtext_rtf_gives_way_to_the_text_body() {
    let body = "{\\rtf1\\ansi\\ansicpg1252\\fromtext \\deff0{\\fonttbl{\\f0\\fswiss Arial;}}\
                {\\*\\htmltag64 <p>}Hello from \\'abRTF\\'bb\\par\
                {\\*\\htmltag72 </p>}world}";
    let mut props = subject("fromtext");
    props.push((RTF_COMPRESSED, rtf(body)));
    let (message, stderr) = read_built(
        "fromtext.msg",
        &Message {
            props,
            ..Message::default()
        },
    );
    assert_eq!(stderr, "");
    assert_eq!(message["markdown"], "Hello from \u{ab}RTF\u{bb}  \nworld");

    let mut props = subject("fromtext and text");
    props.push((RTF_COMPRESSED, rtf(body)));
    props.push((BODY, Prop::Unicode("The text body".into())));
    let (message, _) = read_built(
        "fromtext-text.msg",
        &Message {
            props,
            ..Message::default()
        },
    );
    assert_eq!(message["markdown"], "The text body");
}

/// Beside a stored HTML body and no text body, a plain RTF body's own text is the source text the
/// content rule compares the HTML's Markdown with: its sentence the HTML lacks is appended. With a
/// text body, the text body is the source text, and the RTF is not read.
#[test]
fn a_plain_rtf_beside_html_is_the_source_text() {
    let body = "{\\rtf1\\ansi\\ansicpg1252\\deff0 We found a moth.\\par It is taped in.\\par}";
    let html = || Prop::Binary(b"<p>We found a <b>moth</b>.</p>".to_vec());
    let mut props = subject("html and rtf");
    props.push((RTF_COMPRESSED, rtf(body)));
    props.push((HTML, html()));
    let (message, stderr) = read_built(
        "html-rtf.msg",
        &Message {
            props,
            ..Message::default()
        },
    );
    assert_eq!(stderr, "");
    assert_eq!(
        message["markdown"],
        "We found a **moth**.\n\nIt is taped in."
    );

    let mut props = subject("html, rtf and text");
    props.push((RTF_COMPRESSED, rtf(body)));
    props.push((HTML, html()));
    props.push((BODY, Prop::Unicode("We found a moth.".into())));
    let (message, _) = read_built(
        "html-rtf-text.msg",
        &Message {
            props,
            ..Message::default()
        },
    );
    assert_eq!(message["markdown"], "We found a **moth**.");
}

/// An Exchange address's SMTP address: the sender's from PidTagSenderSmtpAddress, a recipient's
/// from PidTagSmtpAddress. An SMTP address has none written, its address being the one to write
/// to, and an Exchange address without one has none.
#[test]
fn an_exchange_address_has_its_smtp_address() {
    let ex = "/O=EXAMPLE/OU=EXCHANGE ADMINISTRATIVE GROUP/CN=RECIPIENTS/CN=ANN";
    let mut props = subject("smtp");
    props.extend([
        (SENDER_NAME, Prop::Unicode("Ann".into())),
        (SENDER_ADDRESS_TYPE, Prop::Unicode("EX".into())),
        (SENDER_EMAIL_ADDRESS, Prop::Unicode(ex.into())),
        (SENDER_SMTP_ADDRESS, Prop::Unicode("ann@example.com".into())),
    ]);
    let recipient = |name: &str, kind: &str, address: &str, smtp: Option<&str>| {
        let mut props = vec![
            (RECIPIENT_TYPE, Prop::Long(1)),
            (DISPLAY_NAME, Prop::Unicode(name.into())),
            (ADDRESS_TYPE, Prop::Unicode(kind.into())),
            (EMAIL_ADDRESS, Prop::Unicode(address.into())),
        ];
        props.extend(smtp.map(|smtp| (SMTP_ADDRESS, Prop::Unicode(smtp.into()))));
        Child::Props(props)
    };
    let (message, stderr) = read_built(
        "smtp.msg",
        &Message {
            props,
            recipients: vec![
                recipient("Bob", "EX", "/O=EXAMPLE/CN=BOB", Some("bob@example.com")),
                recipient("Cy", "SMTP", "cy@example.com", Some("cy@example.com")),
                recipient("Di", "EX", "/O=EXAMPLE/CN=DI", None),
            ],
            ..Message::default()
        },
    );
    assert_eq!(stderr, "");
    assert_eq!(
        message["sender"],
        json!({"name": "Ann", "address": ex, "address_type": "EX",
               "smtp_address": "ann@example.com"})
    );
    assert_eq!(
        message["to"],
        json!([
            {"name": "Bob", "address": "/O=EXAMPLE/CN=BOB", "address_type": "EX",
             "smtp_address": "bob@example.com"},
            {"name": "Cy", "address": "cy@example.com", "address_type": "SMTP"},
            {"name": "Di", "address": "/O=EXAMPLE/CN=DI", "address_type": "EX"},
        ])
    );
}

/// An attachment that is an image of the HTML body, with its content-id and its location.
fn picture(name: &str, content: &[u8], extra: Vec<(u16, Prop)>) -> Attachment {
    let mut attachment = by_value(name, content);
    let Child::Props(props) = &mut attachment.props else {
        unreachable!()
    };
    props.extend(extra);
    attachment
}

/// The PNG signature, the payload of the `data:` image below.
const PNG: &[u8] = b"\x89PNG\r\n\x1a\n";

/// An image's source is rewritten to its media name: a percent-decoded `cid:` matches an
/// attachment's content-id, another source its PidTagAttachContentLocation, and a `data:` image is
/// written as media and listed in `media`, with no attachment. A source that matches nothing is
/// kept, with one warning. `inline` says the Markdown links the attachment, whatever its stored
/// flags say.
#[test]
fn images_link_their_media_names() {
    let html = "<p>Pictures</p><img src=\"cid:one%2Bimg@example.com\" alt=\"one\">\
                <img src='pic2.png' alt=two><img alt=three src=\"data:image/png;base64,iVBORw0KGgo=\">\
                <img src=\"cid:gone@example.com\" alt=four><img src=cid:gone@example.com alt=five>";
    let mut props = subject("images");
    props.push((HTML, Prop::Binary(html.as_bytes().to_vec())));
    let msg = build(
        "images.msg",
        &Message {
            props,
            attachments: vec![
                picture(
                    "one.gif",
                    b"one",
                    vec![(
                        ATTACH_CONTENT_ID,
                        Prop::Unicode("one+img@example.com".into()),
                    )],
                ),
                picture(
                    "two.jpg",
                    b"two",
                    vec![(ATTACH_CONTENT_LOCATION, Prop::Unicode("pic2.png".into()))],
                ),
                // ATT_MHTML_REF says the HTML refers to it, but the Markdown doesn't link it.
                picture(
                    "three.png",
                    b"three",
                    vec![
                        (
                            ATTACH_CONTENT_ID,
                            Prop::Unicode("unused@example.com".into()),
                        ),
                        (ATTACH_FLAGS, Prop::Long(4)),
                    ],
                ),
            ],
            ..Message::default()
        },
    );
    let dir = scratch("msg-images");
    let out = run(&[msg.as_os_str(), "--media".as_ref(), dir.as_os_str()]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    assert_eq!(
        out.stderr,
        "docproc-mail: warning: message: an image's source \"cid:gone@example.com\" names no \
         attachment; kept as written\n"
    );
    assert_eq!(out.stdout, run(&[&msg]).stdout);
    let message = &out.lines()[1];
    let attachments = message["attachments"].as_array().unwrap();
    let file = |at: usize| attachments[at]["file"].as_str().unwrap();
    let inline: Vec<&Value> = attachments.iter().map(|a| &a["inline"]).collect();
    assert_eq!(inline, [true, true, false]);
    let media = message["media"].as_array().unwrap();
    assert_eq!(media.len(), 1);
    assert_eq!(media[0]["mime_type"], "image/png");
    let png = media[0]["file"].as_str().unwrap();
    assert!(png.ends_with(".png"), "{png}");
    assert_eq!(
        message["markdown"],
        format!(
            "Pictures\n\n![one]({})![two]({})![three]({png})![four](cid:gone@example.com)\
             ![five](cid:gone@example.com)",
            file(0),
            file(1)
        )
    );
    assert_eq!(std::fs::read(dir.join(png)).unwrap(), PNG);
    let mut expected = vec![file(0), file(1), file(2), png];
    expected.sort_unstable();
    assert_eq!(names(&dir), expected);
}

/// An embedded message's images link its own attachments, read before its Markdown is made.
#[test]
fn an_embedded_messages_images_link_its_own_attachments() {
    let mut inner = subject("inner");
    inner.push((
        HTML,
        Prop::Binary(b"<img src=\"cid:moth@example.net\" alt=moth>".to_vec()),
    ));
    let (message, stderr) = read_built(
        "embedded-images.msg",
        &Message {
            props: subject("outer"),
            attachments: vec![embedded(Message {
                props: inner,
                attachments: vec![picture(
                    "moth.png",
                    b"moth",
                    vec![(ATTACH_CONTENT_ID, Prop::Unicode("moth@example.net".into()))],
                )],
                ..Message::default()
            })],
            ..Message::default()
        },
    );
    assert_eq!(stderr, "");
    let inner = &message["attachments"][0]["message"];
    let picture = &inner["attachments"][0];
    assert_eq!(picture["inline"], true);
    assert_eq!(
        inner["markdown"],
        format!("![moth]({})", picture["file"].as_str().unwrap())
    );
}
