//! Ink: page drawings and recognised words' alternatives, read from onenote.rs's samples. Three
//! committed sections hold ink: handwriting_recognition.one, whose first page has 62 ink
//! containers of one stroke each and OneNote's recognition of two lines, and "New Section 1.one"
//! and "non-legacy/New Section 1 2.one", whose test page has one. None holds ink in a paragraph
//! or an outline; src/ink.rs's unit tests cover those drawings. The expectations are written by
//! hand from what onenote_parser's API gives (each stroke's absolute_path() and width(), and
//! Page::ink_recognition), not from the CLI's output.

mod common;

use std::path::Path;

use serde_json::{Value, json};

use common::{fixture, run, scratch, sha256};

const HANDWRITING: &str = "onenote-rs/handwriting_recognition.one";
const ONE_STROKE: [&str; 2] = [
    "onenote-rs/New Section 1.one",
    "onenote-rs/non-legacy/New Section 1 2.one",
];

fn pages(file: &str) -> Vec<Value> {
    let out = run(&[fixture(file)]);
    assert_eq!(out.code, Some(0), "{file}: {}", out.stderr);
    out.lines()
        .into_iter()
        .filter(|line| line["line"] == "page")
        .collect()
}

fn pages_into(file: &str, dir: &Path) -> Vec<Value> {
    let out = run(&[
        fixture(file).as_os_str(),
        "--media".as_ref(),
        dir.as_os_str(),
    ]);
    assert_eq!(out.code, Some(0), "{file}: {}", out.stderr);
    out.lines()
        .into_iter()
        .filter(|line| line["line"] == "page")
        .collect()
}

fn drawings(page: &Value) -> Vec<&str> {
    page["media"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|medium| medium["kind"] == "ink")
        .map(|medium| medium["file"].as_str().unwrap())
        .collect()
}

fn svg(dir: &Path, file: &str) -> String {
    std::fs::read_to_string(dir.join(file)).unwrap()
}

#[test]
fn a_pages_ink_is_one_drawing() {
    for (file, strokes) in [(HANDWRITING, 62), (ONE_STROKE[0], 1), (ONE_STROKE[1], 1)] {
        let dir = scratch("ink-one");
        let pages = pages_into(file, &dir);
        let files = drawings(&pages[0]);
        assert_eq!(files.len(), 1, "{file}");
        let media = pages[0]["media"].as_array().unwrap();
        assert_eq!(media.last().unwrap()["file"], files[0], "{file}");
        let text = svg(&dir, files[0]);
        let document = roxmltree::Document::parse(&text).unwrap();
        let paths = document
            .root_element()
            .children()
            .filter(|n| n.is_element());
        assert_eq!(paths.count(), strokes, "{file}");
        for page in &pages[1..] {
            assert!(drawings(page).is_empty(), "{file}");
        }
    }
    let dir = scratch("ink-box");
    let pages = pages_into(HANDWRITING, &dir);
    let text = svg(&dir, drawings(&pages[0])[0]);
    let document = roxmltree::Document::parse(&text).unwrap();
    assert_eq!(
        document.root_element().attribute("viewBox"),
        Some("1313 38593 16663 15849")
    );
}

#[test]
fn each_recognised_word_has_its_alternatives() {
    let handwriting = pages(HANDWRITING);
    let word =
        |word: usize, alternatives: [&str; 4]| json!({"word": word, "alternatives": alternatives});
    assert_eq!(
        handwriting[0]["ink_alternatives"],
        json!([
            word(0, ["Hallo", "Hell", "HellO", "Hella"]),
            word(1, ["world", "Worlds", "Word", "World."]),
            word(2, ["Hallo", "Hell", "HellO", "Helle"]),
            word(3, ["world", "Worlds", "Word", "Would"]),
        ])
    );
    assert_eq!(handwriting[1].get("ink_alternatives"), None);
    for file in ONE_STROKE {
        assert!(
            pages(file)
                .iter()
                .all(|page| page.get("ink_alternatives").is_none())
        );
    }
}

#[test]
fn each_drawing_is_an_svg_named_by_its_sha256() {
    for file in [HANDWRITING, ONE_STROKE[0], ONE_STROKE[1]] {
        let dir = scratch("ink-svg");
        let pages = pages_into(file, &dir);
        let name = drawings(&pages[0])[0];
        let stem = name.strip_suffix(".svg").expect("an ink file ends .svg");
        assert_eq!(sha256(&dir.join(name)), stem, "{file}");
        let text = svg(&dir, name);
        let document = roxmltree::Document::parse(&text)
            .unwrap_or_else(|err| panic!("{file}: not well-formed XML: {err}"));
        let root = document.root_element();
        assert_eq!(root.tag_name().name(), "svg", "{file}");
        assert_eq!(
            root.tag_name().namespace(),
            Some("http://www.w3.org/2000/svg")
        );
        for path in root.children().filter(|node| node.is_element()) {
            assert_eq!(path.tag_name().name(), "path", "{file}");
            assert!(path.attribute("d").unwrap().starts_with("M "), "{file}");
            assert_eq!(path.attribute("stroke"), Some("#000000"), "{file}");
            assert!(path.attribute("stroke-width").is_some(), "{file}");
        }
    }
}

#[test]
fn each_stroke_keeps_its_width() {
    let dir = scratch("ink-width");
    let pages = pages_into(HANDWRITING, &dir);
    let text = svg(&dir, drawings(&pages[0])[0]);
    let document = roxmltree::Document::parse(&text).unwrap();
    let widths: Vec<&str> = document
        .descendants()
        .filter_map(|node| node.attribute("stroke-width"))
        .collect();
    assert_eq!(widths.len(), 62);
    let count = |width: &str| widths.iter().filter(|w| **w == width).count();
    assert_eq!((count("35"), count("18.75"), count("100")), (13, 35, 14));
}

#[test]
fn drawings_are_the_same_on_every_read() {
    let first = scratch("ink-first");
    let second = scratch("ink-second");
    let a = pages_into(HANDWRITING, &first);
    let b = pages_into(HANDWRITING, &second);
    assert_eq!(a, b);
    let name = drawings(&a[0])[0];
    assert_eq!(
        std::fs::read(first.join(name)).unwrap(),
        std::fs::read(second.join(name)).unwrap()
    );
}
