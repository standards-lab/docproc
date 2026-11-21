//! Media: images, embedded files and ink drawings in a page line's media, and their contents in
//! the --media directory.
//! The expectations are written by hand from what onenote_parser's API gives for each record
//! (its fields, its data's size, and the SHA-256 of the bytes its reader yields), not from the
//! CLI's output.

mod common;

use std::fs::File;

use serde_json::{Value, json};

use common::{NAME, files_under, fixture, run, run_in, scratch, sha256};

const WITH_MEDIA: [&str; 5] = [
    "interop/SUT/OneWithFileData.one",
    "interop/SUT/FileSyncWOPI_OneNoteWithODB.one",
    "onenote-rs/New Section Group/New Section 2.one",
    "onenote-rs/New Section 1.one",
    "onenote-rs/handwriting_recognition.one",
];

const DOCX: &str = "8e99dc44681b8399608f7a181e44d3e41dc36fbacd35a9cfb2d1fc330ac68459";
const CHRYSANTHEMUM: &str = "954f7d96502b5c5fe2e98a5045bca7f5e9ba11e3dbf92a5c0214a6aa4c7f2208";
const DUMMY: &str = "b7702e05282d4dfffe233281443536319d4739946f54ebce194230df8805b650";
const MP3: &str = "d2318cc34b6254cdc2db84b931adad166a4b2b701b4241c27b338b959ac738b0";
const SCREENSHOT: &str = "d6d4898c203cbff35fe92e844bbf404064293314a71d1e6b24e907334e5bdff9";

fn records(lines: &[Value]) -> Vec<Value> {
    media(lines)
        .into_iter()
        .filter(|medium| medium["kind"] != "ink")
        .collect()
}

fn media(lines: &[Value]) -> Vec<Value> {
    lines
        .iter()
        .filter(|line| line["line"] == "page")
        .flat_map(|line| line["media"].as_array().cloned().unwrap_or_default())
        .collect()
}

fn read(file: &str) -> Vec<Value> {
    let out = run(&[fixture(file)]);
    assert_eq!(out.code, Some(0), "{file}: {}", out.stderr);
    assert_eq!(out.stderr, "", "{file}");
    records(&out.lines())
}

#[test]
fn an_embedded_document() {
    assert_eq!(
        read("interop/SUT/OneWithFileData.one"),
        [json!({
            "kind": "embedded_file",
            "file": format!("{DOCX}.docx"),
            "filename": "testing.docx",
            "size": 14660,
        })]
    );
}

#[test]
fn an_image_named_by_its_filenames_extension() {
    let records = read("interop/SUT/FileSyncWOPI_OneNoteWithODB.one");
    assert_eq!(records.len(), 1);
    assert_eq!(
        records[0],
        json!({
            "kind": "image",
            "file": format!("{CHRYSANTHEMUM}.jpg"),
            "size": 879394,
            "filename": "Chrysanthemum.jpg",
        })
    );
}

#[test]
fn a_page_image_and_an_audio_file() {
    assert_eq!(
        read("onenote-rs/New Section Group/New Section 2.one"),
        [
            json!({
                "kind": "image",
                "file": format!("{DUMMY}.png"),
                "filename": "dummy_1.png",
                "size": 27146,
                "extension": "png",
            }),
            json!({
                "kind": "embedded_file",
                "file": format!("{MP3}.mp3"),
                "filename": "ff-16b-2c-44100hz.mp3",
                "size": 77279,
                "type": "audio",
            }),
        ]
    );
}

#[test]
fn the_test_pages_image() {
    let image = json!({
        "kind": "image",
        "file": format!("{SCREENSHOT}.jpg"),
        "filename": "testimages_screenshot.jpg",
        "size": 90999,
    });
    let mut with_extension = image.clone();
    with_extension["extension"] = json!("jpg");
    assert_eq!(read("onenote-rs/handwriting_recognition.one"), [image]);
    assert_eq!(read("onenote-rs/New Section 1.one"), [with_extension]);
}

#[test]
fn media_files_are_named_by_their_sha256() {
    for file in WITH_MEDIA {
        let dir = scratch("media-named").join("made/here");
        let without = run(&[fixture(file)]);
        let out = run(&[
            fixture(file).as_os_str(),
            "--media".as_ref(),
            dir.as_os_str(),
        ]);
        assert_eq!(out.code, Some(0), "{file}: {}", out.stderr);
        assert_eq!(out.stdout, without.stdout, "{file}");
        let records = records(&out.lines());
        let mut names: Vec<String> = media(&out.lines())
            .iter()
            .map(|r| r["file"].as_str().unwrap().to_owned())
            .collect();
        names.sort_unstable();
        names.dedup();
        assert_eq!(files_under(&dir), names, "{file}");
        for record in records {
            let name = record["file"].as_str().unwrap();
            let path = dir.join(name);
            let stem = name.split_once('.').map_or(name, |(stem, _)| stem);
            assert_eq!(sha256(&path), stem, "{file}: {name}");
            let size = std::fs::metadata(&path).unwrap().len();
            assert_eq!(json!(size), record["size"], "{file}: {name}");
        }
    }
}

fn drawings(lines: &[Value]) -> Vec<String> {
    media(lines)
        .iter()
        .filter(|medium| medium["kind"] == "ink")
        .map(|medium| medium["file"].as_str().unwrap().to_owned())
        .collect()
}

#[test]
fn a_repeated_content_is_one_file() {
    let dir = scratch("media-repeated");
    let mut expected = vec![format!("{SCREENSHOT}.jpg")];
    for file in [
        "onenote-rs/New Section 1.one",
        "onenote-rs/handwriting_recognition.one",
    ] {
        let out = run(&[
            fixture(file).as_os_str(),
            "--media".as_ref(),
            dir.as_os_str(),
        ]);
        assert_eq!(out.code, Some(0), "{file}: {}", out.stderr);
        expected.extend(drawings(&out.lines()));
    }
    expected.sort_unstable();
    assert_eq!(expected.len(), 3);
    assert_eq!(files_under(&dir), expected);
}

#[test]
fn an_unwritable_media_directory_exits_1() {
    let base = scratch("media-unwritable");
    let blocker = base.join("file");
    File::create(&blocker).unwrap();
    let dir = blocker.join("media");
    let section = fixture("onenote-rs/New Section Group/New Section 2.one");
    let out = run(&[section.as_os_str(), "--media".as_ref(), dir.as_os_str()]);
    assert_eq!(out.code, Some(1));
    assert_eq!(
        out.stderr,
        format!(
            "{NAME}: error: the media directory {} can't be created: Not a directory (os error 20)\n",
            dir.display()
        )
    );
    assert_eq!(out.stdout, "");
}

#[test]
fn the_media_directory_is_created_once_the_input_is_read() {
    let base = scratch("media-created");
    let dir = base.join("made");
    let section = fixture("interop/SUT/OneWithoutFileData.one");
    let out = run(&[section.as_os_str(), "--media".as_ref(), dir.as_os_str()]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    assert!(dir.is_dir());
    assert!(files_under(&dir).is_empty());
    let refused = base.join("refused");
    let input = fixture("interop/SUT/Encryption.one");
    let out = run(&[input.as_os_str(), "--media".as_ref(), refused.as_os_str()]);
    assert_eq!(out.code, Some(3));
    assert!(!refused.exists());
}

#[test]
fn without_media_nothing_is_written() {
    let dir = scratch("media-none");
    for file in WITH_MEDIA {
        let out = run_in(&dir, &[fixture(file)]);
        assert_eq!(out.code, Some(0), "{file}: {}", out.stderr);
        assert!(!out.stdout.is_empty(), "{file}");
    }
    assert_eq!(std::fs::read_dir(&dir).unwrap().count(), 0);
}
