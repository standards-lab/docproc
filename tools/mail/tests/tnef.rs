//! The TNEF group: Apache POI's HMEF test streams under tests/fixtures/tnef/poi, each wrapped as
//! the TNEF part of an .eml under tests/fixtures/tnef/eml by the generator, through the binary
//! against what MimeKit's TnefPart reads of each stream (truth.json), and test-built messages for
//! what those don't carry: a truncated stream.

mod common;

use common::{assert_markdown, built, fixture, run, sha256, words_of_markdown};
use serde_json::{Value, json};

const MESSAGES: &[&str] = &[
    "01-quick-winmail.eml",
    "02-winmail-sample1.eml",
    "03-bug52400-winmail-simple.eml",
    "04-bug52400-winmail-with-attachments.eml",
    "05-oom.eml",
];

const MALFORMED: &str = "05-oom.eml";

const SOURCES: &[&str] = &[
    "poi/bug52400-winmail-simple.dat",
    "poi/bug52400-winmail-with-attachments.dat",
    "poi/oom.tnef",
    "poi/quick-winmail.dat",
    "poi/winmail-sample1.dat",
];

fn truth() -> Value {
    serde_json::from_slice(&std::fs::read(fixture("tnef/truth.json")).unwrap()).unwrap()
}

#[test]
fn every_stream_has_a_matching_sources_entry() {
    let sources: Value =
        serde_json::from_slice(&std::fs::read(fixture("tnef/sources.json")).unwrap()).unwrap();
    let entries = sources.as_array().unwrap();
    let listed: Vec<&str> = entries
        .iter()
        .map(|e| e["file"].as_str().unwrap())
        .collect();
    assert_eq!(listed, SOURCES);
    for entry in entries {
        for field in ["repository", "commit", "path", "license", "sha256"] {
            assert!(entry[field].is_string(), "{}: {field}", entry["file"]);
        }
        let path = fixture(&format!("tnef/{}", entry["file"].as_str().unwrap()));
        assert_eq!(
            Some(sha256(&path).as_str()),
            entry["sha256"].as_str(),
            "{}",
            path.display()
        );
    }
    for notice in ["poi/LICENSE", "poi/NOTICE"] {
        assert!(fixture(&format!("tnef/{notice}")).is_file(), "{notice}");
    }
}

fn message_of(out: &common::Run, source: &str) -> Value {
    assert_eq!(out.code, Some(0), "{source}: {}", out.stderr);
    let lines = out.lines();
    assert_eq!(
        lines[0],
        json!({"line": "store", "schema": "2", "format": "eml"}),
        "{source}"
    );
    lines[1].clone()
}

fn assert_attachments_match(got: &Value, truth: &Value, source: &str) {
    let records = got.get("attachments").and_then(Value::as_array);
    let records = records.map(Vec::as_slice).unwrap_or_default();
    let expected = truth["attachments"].as_array().unwrap();
    assert_eq!(records.len(), expected.len(), "{source}: attachments");
    for (record, expected) in records.iter().zip(expected) {
        let filename = expected["filename"].as_str().unwrap();
        let (_, extension) = filename.rsplit_once('.').unwrap();
        let sha = expected["sha256"].as_str().unwrap();
        assert_eq!(record["filename"], expected["filename"], "{source}");
        assert_eq!(record["size"], expected["size"], "{source}: {filename}");
        assert_eq!(
            record["file"],
            json!(format!("{sha}.{extension}")),
            "{source}: {filename}"
        );
        assert_eq!(record["method"], "by_value", "{source}: {filename}");
        let mime_type = record
            .get("mime_type")
            .unwrap_or(&json!("application/octet-stream"))
            .clone();
        assert_eq!(mime_type, expected["mime_type"], "{source}: {filename}");
    }
}

fn without_style(html: &str) -> String {
    let mut out = String::new();
    let mut rest = html;
    while let Some(start) = rest.to_ascii_lowercase().find("<style") {
        out.push_str(&rest[..start]);
        let end = rest.to_ascii_lowercase().find("</style>").unwrap();
        rest = &rest[end + "</style>".len()..];
    }
    out.push_str(rest);
    out
}

fn assert_in_order(words: &[String], text: &str, source: &str) {
    let mut rest = text;
    for word in words {
        let at = rest
            .find(word.as_str())
            .unwrap_or_else(|| panic!("{source}: {word:?} is not in the RTF, in order"));
        rest = &rest[at + word.len()..];
    }
}

#[test]
fn each_message_matches_mimekits_reading_of_its_stream() {
    let truth = truth();
    let listed: Vec<&String> = truth["messages"].as_object().unwrap().keys().collect();
    assert_eq!(listed, MESSAGES, "every message has its truth");
    for source in MESSAGES {
        let truth = &truth["messages"][source];
        let out = run(&[fixture(&format!("tnef/eml/{source}"))]);
        let got = message_of(&out, source);
        if *source == MALFORMED {
            continue;
        }
        assert_eq!(out.stderr, "", "{source}: no TNEF note remains");
        assert_eq!(out.lines().len(), 2, "{source}");
        assert_attachments_match(&got, truth, source);
        let markdown = got["markdown"].as_str().map(words_of_markdown);
        if let Some(html) = truth.get("html") {
            let html = without_style(html.as_str().unwrap());
            assert_markdown(&got, &json!({ "html": html }), source);
        } else if let Some(text) = truth.get("text").or(truth.get("mime_text")) {
            let words: Vec<String> = text
                .as_str()
                .unwrap()
                .split_whitespace()
                .map(str::to_owned)
                .collect();
            assert_eq!(markdown, Some(words), "{source}: markdown");
            if truth.get("text").is_none() {
                let rtf = truth["rtf"].as_str().unwrap();
                assert!(!rtf.contains("\\fromhtml1"), "{source}: a plain RTF body");
            }
        } else {
            let markdown = markdown.unwrap_or_else(|| panic!("{source}: markdown"));
            assert!(!markdown.is_empty(), "{source}: markdown");
            assert_in_order(&markdown, truth["rtf"].as_str().unwrap(), source);
        }
    }
}

#[test]
fn a_malformed_tnef_part_is_a_skipped_attachment_and_the_message_is_read() {
    let truth = &truth()["messages"][MALFORMED];
    assert_eq!(
        truth["attachments"],
        json!([]),
        "MimeKit reads nothing of it"
    );
    assert_eq!(truth.get("html"), None);
    assert_eq!(truth.get("rtf"), None);
    assert_ne!(truth["compliance"], "Compliant");
    let out = run(&[fixture(&format!("tnef/eml/{MALFORMED}"))]);
    let got = message_of(&out, MALFORMED);
    assert_eq!(
        out.stderr,
        "docproc-mail: warning: message, attachment 1: skipped: the TNEF part can't be read \
         (property 0x8000's value is 4 bytes, past the 0 that remain)\n"
    );
    assert_eq!(
        got["markdown"].as_str().map(words_of_markdown),
        Some(vec![
            "The".into(),
            "TNEF".into(),
            "part".into(),
            "beside".into(),
            "this".into(),
            "one".into(),
            "is".into(),
            "malformed.".into()
        ])
    );
    assert_eq!(got.get("attachments"), None);
    assert_eq!(
        out.lines()[2],
        json!({"line": "skipped", "kind": "attachment", "id": 1,
               "reason": "the TNEF part can't be read (property 0x8000's value is 4 bytes, past \
                          the 0 that remain)"})
    );
}

fn base64(bytes: &[u8]) -> String {
    const DIGITS: &[u8] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    let mut out = String::new();
    for (n, chunk) in bytes.chunks(3).enumerate() {
        if n > 0 && n % 19 == 0 {
            out.push_str("\r\n");
        }
        let word = chunk.iter().enumerate().fold(0u32, |word, (i, byte)| {
            word | u32::from(*byte) << (16 - 8 * i)
        });
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

#[test]
fn a_truncated_tnef_part_is_a_skipped_attachment_and_the_message_is_read() {
    let stream = std::fs::read(fixture("tnef/poi/winmail-sample1.dat")).unwrap();
    let truncated = &stream[..stream.len() / 2];
    let eml = format!(
        "From: ann@example.com\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n\
         --b\r\nContent-Type: text/plain\r\n\r\nRead.\r\n\
         --b\r\nContent-Type: application/octet-stream; name=winmail.dat\r\n\
         Content-Transfer-Encoding: base64\r\n\r\n{}\r\n--b--\r\n",
        base64(truncated)
    );
    let path = built("truncated-tnef.eml", eml.as_bytes());
    let out = run(&[&path]);
    let got = message_of(&out, "truncated");
    let reason = "the TNEF part can't be read (attribute 0x0006800F's data is 85805 bytes, past \
                  the 37158 that remain)";
    assert_eq!(
        out.stderr,
        format!("docproc-mail: warning: message, attachment 1: skipped: {reason}\n")
    );
    assert_eq!(got["markdown"], "Read.");
    assert_eq!(
        out.lines()[2],
        json!({"line": "skipped", "kind": "attachment", "id": 1, "reason": reason})
    );
}
