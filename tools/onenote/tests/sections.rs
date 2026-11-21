//! Single sections: each public .one fixture against its hand-written expectations in
//! tests/fixtures/sections.json (its section's id, path and colour, and each page's id, level,
//! title, times and author, or its refusal), and the line rules every read keeps.

mod common;

use std::path::Path;

use serde_json::{Value, json};

use common::{assert_line_rules, error, files_under, fixture, run};

const PAGE_FIELDS: [&str; 6] = [
    "id",
    "level",
    "title",
    "creation_time",
    "modification_time",
    "author",
];

fn expectations() -> Vec<Value> {
    let bytes = std::fs::read(fixture("sections.json")).unwrap();
    serde_json::from_slice::<Value>(&bytes)
        .unwrap()
        .as_array()
        .unwrap()
        .clone()
}

#[test]
fn every_public_section_has_an_expectation() {
    let mut listed: Vec<String> = expectations()
        .iter()
        .map(|e| e["file"].as_str().unwrap().to_owned())
        .collect();
    listed.sort_unstable();
    let mut sections = Vec::new();
    for set in ["interop", "onenote-rs"] {
        for file in files_under(&fixture(set)) {
            if file.ends_with(".one") {
                sections.push(format!("{set}/{file}"));
            }
        }
    }
    sections.sort_unstable();
    assert_eq!(listed, sections);
}

#[test]
fn every_public_section_reads_as_expected() {
    for expected in expectations() {
        let file = expected["file"].as_str().unwrap();
        let input = fixture(file);
        let out = run(&[&input]);
        assert_eq!(
            out.code,
            expected["code"].as_i64().map(|c| c as i32),
            "{file}"
        );
        if let Some(reason) = expected["reason"].as_str() {
            assert_eq!(out.stdout, "", "{file}");
            assert_eq!(out.stderr, error(reason), "{file}");
            continue;
        }
        assert_eq!(out.stderr, "", "{file}");
        let lines = out.lines();
        let pages = expected["pages"].as_array().unwrap();
        let kinds: Vec<&str> = lines.iter().map(|l| l["line"].as_str().unwrap()).collect();
        let mut want = vec!["notebook", "section"];
        want.extend(pages.iter().map(|_| "page"));
        assert_eq!(kinds, want, "{file}");

        let stem = Path::new(file).file_stem().unwrap().to_str().unwrap();
        assert_eq!(
            lines[0],
            json!({"line": "notebook", "schema": "2", "input": "section", "name": stem}),
            "{file}"
        );
        let section = &expected["section"];
        assert_eq!(
            lines[1],
            json!({
                "line": "section",
                "id": section["id"],
                "path": section["path"],
                "color": section["color"],
            }),
            "{file}"
        );
        for (line, page) in lines[2..].iter().zip(pages) {
            assert_eq!(line["section"], section["id"], "{file}");
            for field in PAGE_FIELDS {
                assert_eq!(line.get(field), page.get(field), "{file}: {field}");
            }
        }
    }
}

#[test]
fn every_read_keeps_the_line_rules_and_repeats_byte_for_byte() {
    for expected in expectations() {
        let file = expected["file"].as_str().unwrap();
        let first = run(&[fixture(file)]);
        let second = run(&[fixture(file)]);
        assert_eq!(first.stdout, second.stdout, "{file}");
        for line in first.lines() {
            assert_line_rules(&line, file);
        }
    }
}
