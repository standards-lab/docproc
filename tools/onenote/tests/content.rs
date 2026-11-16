//! A page line's media and ink alternatives, read from the public fixtures, against the
//! hand-written expectations in tests/fixtures/content.json. The page's text, its runs, lists,
//! tables and tags, is its Markdown's, which tests/markdown.rs writes out by hand.

mod common;

use serde_json::Value;

use common::{fixture, run};

/// Every field of tests/fixtures/content.json holds in its page's read: the value at its
/// pointer, or no value for a null.
#[test]
fn every_content_expectation_holds() {
    let bytes = std::fs::read(fixture("content.json")).unwrap();
    let expectations: Value = serde_json::from_slice(&bytes).unwrap();
    for entry in expectations["pages"].as_array().unwrap() {
        for file in entry["files"].as_array().unwrap() {
            let file = file.as_str().unwrap();
            let out = run(&[fixture(file)]);
            assert_eq!(out.code, Some(0), "{file}: {}", out.stderr);
            let page = out
                .lines()
                .into_iter()
                .find(|line| line["line"] == "page" && line["id"] == entry["page"])
                .unwrap_or_else(|| panic!("{file}: no page {}", entry["page"]));
            for field in entry["fields"].as_array().unwrap() {
                let pointer = field["pointer"].as_str().unwrap();
                let want = Some(&field["value"]).filter(|value| !value.is_null());
                assert_eq!(page.pointer(pointer), want, "{file}: {pointer}");
            }
        }
    }
}
