//! Reading .pst stores: the committed public stores against the hand truth (tests/golden.rs holds
//! their goldens), their attachments, and the skip policy on stores corrupted in the test.
//!
//! The stores corrupted in the test are copies of libyal's outlook.pst, its data blocks encoded by
//! MS-PST 5.1's permutation, each with its block trailer's CRC (MS-PST 5.3: CRC-32's polynomial,
//! reflected, from 0 and with no final complement), which outlook-pst checks. At 18944 is a node
//! B-tree page, whose loss costs two folders and the message under Inbox; at 29696 a message's data
//! block; at 40000 the root's hierarchy table, and at 40704 Inbox's contents table. The cycle
//! rewrites Recovered_Group2's (33410) hierarchy row for its Inbox (33442) to name the root
//! (32802): the row matrix is the data block at 42624, 158 bytes, the row's dwRowID at 42730,
//! encoded EE 27 for 33442 and 84 E2 for 32802 (0 encodes to 0x41), and the CRC in its trailer at
//! 42804. The bytes flipped at 29699 and 29763 make outlook-pst multiply past u32, reading Inbox's
//! message and the first of its eleven pictures, which the release binary's overflow checks
//! (Cargo.toml) make a panic.

mod common;

use common::{assert_no_nulls, built, fixture, names, run, scratch, sha256};
use serde_json::Value;

const STORES: &[&str] = &[
    "tika/testPST.pst",
    "tika/testPST_variousBodyTypes.pst",
    "libyal/outlook.pst",
    "java-libpst/dist-list.pst",
    "java-libpst/passworded.pst",
];

#[test]
fn output_is_byte_identical_across_runs() {
    for store in STORES {
        let path = fixture(&format!("pst/{store}"));
        assert_eq!(run(&[&path]).stdout, run(&[&path]).stdout, "{store}");
    }
}

#[test]
fn lines_follow_schema_2() {
    for store in STORES {
        let lines = run(&[fixture(&format!("pst/{store}"))]).lines();
        let first = &lines[0];
        assert_eq!(first["line"], "store", "{store}");
        assert_eq!(first["schema"], "2", "{store}");
        assert_eq!(first["format"], "pst", "{store}");
        assert_eq!(first["encoding"], "unicode", "{store}");
        let mut folders: Vec<(u64, Vec<Value>)> = Vec::new();
        for line in &lines[1..] {
            assert_no_nulls(line, store);
            match line["line"].as_str().unwrap() {
                "folder" => {
                    let path = line["path"].as_array().unwrap().clone();
                    if let Some(parent) = line.get("parent") {
                        let (_, parent_path) = folders
                            .iter()
                            .rev()
                            .find(|(id, _)| Some(*id) == parent.as_u64())
                            .expect("a folder's parent is written before it");
                        assert_eq!(parent_path[..], path[..path.len() - 1], "{store}");
                        let last = &folders.last().unwrap().1;
                        assert!(last.starts_with(parent_path), "{store}: not depth-first");
                    } else {
                        assert!(folders.is_empty(), "{store}: only the root has no parent");
                    }
                    folders.push((line["id"].as_u64().unwrap(), path));
                }
                "skipped" if line["kind"] == "folder" => {
                    assert!(
                        folders
                            .iter()
                            .any(|(id, _)| Some(*id) == line["folder"].as_u64()),
                        "{store}: a skipped folder's parent is written before it"
                    );
                }
                "message" | "skipped" => {
                    let folder = line["folder"].as_u64();
                    assert_eq!(
                        folders.last().map(|(id, _)| *id),
                        folder,
                        "{store}: a message follows its own folder's line"
                    );
                }
                other => panic!("{store}: unknown line {other}"),
            }
        }
    }
}

#[test]
fn threading_fields_match_the_tika_truth() {
    let truth: Value =
        serde_json::from_slice(&std::fs::read(fixture("pst/tika-threading.json")).unwrap())
            .unwrap();
    for (store, expected) in truth["stores"].as_object().unwrap() {
        let lines = run(&[fixture(&format!("pst/{store}"))]).lines();
        let messages: Vec<&Value> = lines.iter().filter(|l| l["line"] == "message").collect();
        let expected = expected.as_array().unwrap();
        assert_eq!(messages.len(), expected.len(), "{store}");
        for (message, truth) in messages.iter().zip(expected) {
            for (field, value) in truth.as_object().unwrap() {
                let got = message.get(field).unwrap_or(&Value::Null);
                assert_eq!(got, value, "{store}: {} {field}", truth["subject"]);
            }
        }
    }
}

#[test]
fn the_first_tika_message_matches_tikas_assertions() {
    let lines = run(&[fixture("pst/tika/testPST.pst")]).lines();
    let first = lines.iter().find(|l| l["line"] == "message").unwrap();
    assert_eq!(first["class"], "IPM.Note");
    assert_eq!(first["subject"], "Re: Feature Generators");
    assert_eq!(first["sender"]["name"], "Jörn Kottmann");
    assert_eq!(first["sender"]["address"], "kottmann@gmail.com");
    assert_eq!(first["to"][0]["name"], "users@opennlp.apache.org");
    assert_eq!(first.get("cc"), None);
    assert_eq!(first.get("bcc"), None);
    assert_eq!(first["submit_time"], "2014-02-26T07:50:04Z");
    assert_eq!(first["delivery_time"], "2014-02-26T07:51:02Z");
    assert_eq!(first["creation_time"], "2014-02-26T07:51:02Z");
    assert_eq!(first["modification_time"], "2014-02-26T12:37:43Z");
    let folder = &lines[1];
    assert_eq!(
        folder["path"],
        serde_json::json!(["Début du fichier de données Outlook"])
    );
    assert_eq!(folder.get("display_name"), None);
}

#[test]
fn the_store_line_records_the_password() {
    for (store, password) in [
        ("java-libpst/passworded.pst", true),
        ("java-libpst/dist-list.pst", false),
    ] {
        let lines = run(&[fixture(&format!("pst/{store}"))]).lines();
        assert_eq!(lines[0]["password"], password, "{store}");
    }
}

#[test]
fn a_message_outlook_pst_cannot_read_is_skipped() {
    for store in ["java-libpst/dist-list.pst", "java-libpst/passworded.pst"] {
        let out = run(&[fixture(&format!("pst/{store}"))]);
        assert_eq!(out.code, Some(0));
        let skipped: Vec<Value> = out
            .lines()
            .into_iter()
            .filter(|l| l["line"] == "skipped")
            .collect();
        assert_eq!(
            skipped,
            [serde_json::json!({
                "line": "skipped",
                "kind": "message",
                "folder": 33090,
                "id": 2097188,
                "reason": "Missing Sub-Node Tree on message",
            })],
            "{store}"
        );
        let skips: Vec<&str> = out
            .stderr
            .lines()
            .filter(|line| line.contains("skipped"))
            .collect();
        assert_eq!(
            skips,
            ["docproc-mail: warning: folder 33090, message \
              2097188: skipped: Missing Sub-Node Tree on message"]
        );
    }
}

fn attachments(lines: &[Value]) -> Vec<(u64, Vec<Value>)> {
    lines
        .iter()
        .filter(|l| l["line"] == "message")
        .filter_map(|l| {
            let list = l.get("attachments")?.as_array()?.clone();
            Some((l["id"].as_u64().unwrap(), list))
        })
        .collect()
}

#[test]
fn embedded_messages_are_listed_with_a_warning() {
    let embedded = serde_json::json!({ "inline": false, "method": "embedded_message" });
    for (store, place, message, ids) in [
        (
            "java-libpst/dist-list.pst",
            "folder 33058",
            2097348,
            &[32933, 32997][..],
        ),
        ("tika/testPST.pst", "folder 32802", 2097380, &[32805][..]),
    ] {
        let dir = scratch(&format!("embedded-{}", store.replace('/', "-")));
        let path = fixture(&format!("pst/{store}"));
        let out = run(&[path.as_os_str(), "--media".as_ref(), dir.as_os_str()]);
        assert_eq!(out.code, Some(0), "{store}: {}", out.stderr);
        let expected = vec![embedded.clone(); ids.len()];
        assert_eq!(attachments(&out.lines()), [(message, expected)], "{store}");
        for id in ids {
            let warning = format!(
                "docproc-mail: warning: {place}, message {message}, attachment {id}: \
                 an embedded message (attach method 5), listed without its content: \
                 outlook-pst 1.2.0 can't open one"
            );
            assert!(
                out.stderr.lines().any(|l| l == warning),
                "{store}: {warning}"
            );
        }
        assert_eq!(
            names(&dir),
            Vec::<String>::new(),
            "{store}: no content written"
        );
    }
}

#[test]
fn each_distinct_content_is_written_once() {
    let dir = scratch("dedup");
    let store = fixture("pst/libyal/outlook.pst");
    let out = run(&[store.as_os_str(), "--media".as_ref(), dir.as_os_str()]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    let records: Vec<Value> = attachments(&out.lines())
        .into_iter()
        .flat_map(|(_, list)| list)
        .collect();
    assert_eq!(records.len(), 22);
    let mut files: Vec<String> = records
        .iter()
        .map(|r| {
            assert_eq!(r["method"], "by_value");
            r["file"].as_str().unwrap().to_owned()
        })
        .collect();
    files.sort_unstable();
    files.dedup();
    assert_eq!(files.len(), 11);
    assert_eq!(names(&dir), files);
    for record in &records {
        let name = record["file"].as_str().unwrap();
        let path = dir.join(name);
        assert_eq!(format!("{}.jpg", sha256(&path)), name);
        assert_eq!(
            Some(std::fs::metadata(&path).unwrap().len()),
            record["size"].as_u64()
        );
    }
}

#[test]
fn records_after_a_reader_panic_are_still_read() {
    let mut bytes = std::fs::read(fixture("pst/libyal/outlook.pst")).unwrap();
    bytes[29699] ^= 0xFF;
    let out = run(&[built("panic.pst", &bytes)]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    assert_eq!(
        out.stderr,
        "docproc-mail: warning: folder 33090, message 2097188: \
         skipped: the reader panicked: attempt to multiply with overflow\n"
    );
    let intact = run(&[fixture("pst/libyal/outlook.pst")]).lines();
    let lines = out.lines();
    assert_eq!(lines.len(), intact.len());
    for (got, want) in lines.iter().zip(&intact) {
        if want["line"] == "message" && want["id"] == 2097188 {
            assert_eq!(got["line"], "skipped");
        } else {
            assert_eq!(got, want);
        }
    }
}

fn corrupted(name: &str, offset: usize) -> std::path::PathBuf {
    let mut bytes = std::fs::read(fixture("pst/libyal/outlook.pst")).unwrap();
    bytes[offset..offset + 32].fill(0xFF);
    built(name, &bytes)
}

#[test]
fn corrupt_records_are_skipped_and_the_run_exits_0() {
    let out = run(&[corrupted("corrupt-btree.pst", 18944)]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    let lines = out.lines();
    let skipped: Vec<(String, u64)> = lines
        .iter()
        .filter(|l| l["line"] == "skipped")
        .map(|l| {
            (
                l["kind"].as_str().unwrap().to_owned(),
                l["id"].as_u64().unwrap(),
            )
        })
        .collect();
    assert_eq!(
        skipped,
        [
            ("folder".to_owned(), 32930),
            ("folder".to_owned(), 32962),
            ("message".to_owned(), 2097188),
            ("folder".to_owned(), 33122),
        ]
    );
    let warnings: Vec<&str> = out.stderr.lines().collect();
    assert_eq!(warnings.len(), skipped.len());
    assert!(
        warnings[0].starts_with("docproc-mail: warning: folder 32802, folder 32930: skipped: ")
    );
    assert!(
        lines
            .iter()
            .any(|l| l["line"] == "message" && l["id"] == 2097220)
    );

    let out = run(&[corrupted("corrupt-message.pst", 29696)]);
    assert_eq!(out.code, Some(0));
    assert_eq!(
        out.stderr,
        "docproc-mail: warning: folder 33090, message 2097188: \
         skipped: Invalid internal block btype: 0xFF\n"
    );
    let messages = out
        .lines()
        .into_iter()
        .filter(|l| l["line"] == "message")
        .count();
    assert_eq!(messages, 1);
}

fn cyclic() -> std::path::PathBuf {
    let mut bytes = std::fs::read(fixture("pst/libyal/outlook.pst")).unwrap();
    assert_eq!(bytes[42730..42734], [0xEE, 0x27, 0x41, 0x41], "Inbox's row");
    bytes[42730..42734].copy_from_slice(&[0x84, 0xE2, 0x41, 0x41]);
    let crc = crc(&bytes[42624..42624 + 158]);
    bytes[42804..42808].copy_from_slice(&crc.to_le_bytes());
    built("cyclic.pst", &bytes)
}

fn crc(data: &[u8]) -> u32 {
    let mut crc = 0u32;
    for &byte in data {
        crc ^= u32::from(byte);
        for _ in 0..8 {
            crc = if crc & 1 != 0 {
                (crc >> 1) ^ 0xEDB8_8320
            } else {
                crc >> 1
            };
        }
    }
    crc
}

#[test]
fn a_folder_cycle_is_read_once() {
    let store = cyclic();
    let whole = run(&[fixture("pst/libyal/outlook.pst")]).lines();
    let out = run(&[&store]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    assert_eq!(
        out.stderr,
        "docproc-mail: warning: folder 33410, folder 32802: skipped: already read\n"
    );
    let skipped = serde_json::json!({
        "line": "skipped",
        "kind": "folder",
        "folder": 33410,
        "id": 32802,
        "reason": "already read",
    });
    let expected: Vec<Value> = whole
        .into_iter()
        .map(|line| {
            if line["line"] == "folder" && line["id"] == 33442 {
                skipped.clone()
            } else {
                line
            }
        })
        .collect();
    assert_eq!(out.lines(), expected);

    let resumed = run(&["--after".as_ref(), "2097188".as_ref(), store.as_os_str()]);
    assert_eq!(resumed.code, Some(0), "{}", resumed.stderr);
    assert!(resumed.lines().contains(&skipped));
    let missing = run(&["--after".as_ref(), "4".as_ref(), store.as_os_str()]);
    assert_eq!(missing.code, Some(3), "{}", missing.stderr);
    assert_eq!(
        missing.stderr,
        "docproc-mail: error: --after 4: no message in the store has that node id\n"
    );
}

fn flipped(name: &str, offset: usize) -> std::path::PathBuf {
    let mut bytes = std::fs::read(fixture("pst/libyal/outlook.pst")).unwrap();
    bytes[offset] = 0xAA;
    built(name, &bytes)
}

#[test]
fn a_folder_table_that_cannot_be_read_is_skipped() {
    let reason = |table: &str, count: &str| {
        format!(
            "the {table} table can't be read, though {count} (outlook-pst 1.2.0 gives no reason)"
        )
    };

    let out = run(&[flipped("lost-hierarchy.pst", 40000)]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    let lost = reason("hierarchy", "PidTagSubfolders says it has subfolders");
    assert_eq!(
        out.stderr,
        format!(
            "docproc-mail: warning: folder 32802, hierarchy table: \
             skipped: {lost}\n"
        )
    );
    let lines = out.lines();
    assert_eq!(lines.len(), 3);
    assert_eq!(
        lines[2],
        serde_json::json!({
            "line": "skipped",
            "kind": "hierarchy",
            "folder": 32802,
            "reason": lost,
        })
    );

    let out = run(&[flipped("lost-contents.pst", 40704)]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    let lost = reason("contents", "PidTagContentCount gives it items");
    assert_eq!(
        out.stderr,
        format!(
            "docproc-mail: warning: folder 33090, contents table: \
             skipped: {lost}\n"
        )
    );
    let mut intact = run(&[fixture("pst/libyal/outlook.pst")]).lines();
    let at = intact.iter().position(|l| l["id"] == 2097188).unwrap();
    intact[at] = serde_json::json!({
        "line": "skipped",
        "kind": "contents",
        "folder": 33090,
        "reason": lost,
    });
    assert_eq!(out.lines(), intact);
}

#[test]
fn attachments_after_a_reader_panic_are_still_read() {
    let mut bytes = std::fs::read(fixture("pst/libyal/outlook.pst")).unwrap();
    bytes[29763] ^= 0xFF;
    let out = run(&[built("attachment-panic.pst", &bytes)]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    let reason = "the reader panicked: attempt to multiply with overflow";
    assert_eq!(
        out.stderr,
        format!(
            "docproc-mail: warning: folder 33090, message 2097188, \
             attachment 32805: skipped: {reason}\n\
             docproc-mail: warning: folder 33090, message 2097188: \
             an image's source \"1.jpg\" names no attachment; kept as written\n"
        )
    );
    let mut intact = run(&[fixture("pst/libyal/outlook.pst")]).lines();
    let at = intact.iter().position(|l| l["id"] == 2097188).unwrap();
    let first = intact[at]["attachments"].as_array_mut().unwrap().remove(0);
    let markdown = intact[at]["markdown"].as_str().unwrap();
    let markdown = markdown.replace(first["file"].as_str().unwrap(), "1.jpg");
    intact[at]["markdown"] = markdown.into();
    intact.insert(
        at + 1,
        serde_json::json!({
            "line": "skipped",
            "kind": "attachment",
            "folder": 33090,
            "message": 2097188,
            "id": 32805,
            "reason": reason,
        }),
    );
    assert_eq!(intact[at]["attachments"].as_array().unwrap().len(), 10);
    assert_eq!(out.lines(), intact);
}

#[test]
fn every_store_has_a_matching_sources_entry() {
    let sources: Value =
        serde_json::from_slice(&std::fs::read(fixture("pst/sources.json")).unwrap()).unwrap();
    let entries = sources.as_array().unwrap();
    let mut listed: Vec<&str> = entries
        .iter()
        .map(|e| e["file"].as_str().unwrap())
        .collect();
    listed.sort_unstable();
    let mut stores = STORES.to_vec();
    stores.sort_unstable();
    assert_eq!(listed, stores);
    for entry in entries {
        for field in ["repository", "commit", "path", "license", "sha256"] {
            assert!(entry[field].is_string(), "{}: {field}", entry["file"]);
        }
        let path = fixture(&format!("pst/{}", entry["file"].as_str().unwrap()));
        assert_eq!(
            Some(sha256(&path).as_str()),
            entry["sha256"].as_str(),
            "{}",
            path.display()
        );
    }
}

#[test]
fn markdown_comes_from_the_html_else_the_text() {
    let lines = run(&[fixture("pst/tika/testPST_variousBodyTypes.pst")]).lines();
    let messages: Vec<&Value> = lines.iter().filter(|l| l["line"] == "message").collect();
    assert!(messages.iter().all(|message| message.get("body").is_none()));
    let markdown = |message: &Value| message["markdown"].as_str().unwrap().to_owned();
    let (html, forwarded_html, rtf, plain) = (
        markdown(messages[0]),
        markdown(messages[1]),
        markdown(messages[2]),
        markdown(messages[3]),
    );
    assert_eq!(html, "This is the **original** email (html)");
    assert!(
        forwarded_html
            .ends_with("This is the **original** email (html)\n\n\u{A0}\n\n**Forwarded** (html)")
    );
    assert!(rtf.ends_with(
        "Subject: original email\n\nThis is the original email (html)\n\nForwarded RTF"
    ));
    assert!(
        plain.starts_with("From: Allison, Timothy B.  \nSent: Wednesday, August 30, 2017 3:26 PM  \nTo: Allison, Timothy B. \\<tallison@mitre.org>  \n")
    );
}

#[test]
fn markdown_from_plain_text_escapes_markup() {
    let lines = run(&[fixture("pst/tika/testPST.pst")]).lines();
    let markdown = |id: u64| {
        let message = lines.iter().find(|l| l["id"] == id).unwrap();
        message["markdown"].as_str().unwrap().to_owned()
    };
    let resolved = markdown(2097284);
    assert!(
        resolved.starts_with(
            "\\[ https://issues.apache.org/jira/browse/TIKA-1249?page=com.atlassian.jira.\
             plugin.system.issuetabpanels:all-tabpanel \\]\n\nNick Burch resolved TIKA-1249.  \n\
             \\------------------------------\n"
        ),
        "{resolved}"
    );
    assert!(resolved.contains("\n\\> Vcard files detection  \n\\> ---------------------  \n"));
    assert!(markdown(2097316).contains("comment-tabpanel\\&focusedCommentId=13912806"));
}
