//! Notebooks: each committed .onetoc2 against its hand-written structural expectations in
//! tests/fixtures/notebooks.json, each notebook directory read through its .onetoc2, a notebook
//! with a section group, and each package, built here through cab's MSZIP writer, read as its
//! directory's golden is.

mod common;

use std::io::{Cursor, Write};
use std::path::{Path, PathBuf};
use std::sync::OnceLock;

use serde_json::{Value, json};

use common::{
    NAME, Run, assert_line_rules, files_under, fixture, goldens, read_golden, run, scratch,
};

fn expectations() -> Vec<Value> {
    let bytes = std::fs::read(fixture("notebooks.json")).unwrap();
    serde_json::from_slice::<Value>(&bytes)
        .unwrap()
        .as_array()
        .unwrap()
        .clone()
}

const DIRECTORIES: [&str; 6] = [
    "interop/SUT",
    "onenote-rs",
    "onenote-rs/New Section Group",
    "onenote-rs/non-legacy",
    "onenote-rs/object-revision",
    "onenote-rs/OneNote_RecycleBin",
];

fn composed_expectation() -> Value {
    let group = "2ed96381-1fac-4ca5-bda9-0e3a35478644";
    let mut expected = expectations()
        .into_iter()
        .find(|e| e["file"] == "onenote-rs/non-legacy/Open Notebook.onetoc2")
        .unwrap();
    expected["notebook"]["name"] = json!("composed");
    let entries = expected["entries"].as_array_mut().unwrap();
    entries.extend([
        json!({"line": "section_group", "id": group, "path": ["New Section Group"]}),
        json!({"line": "section", "id": "07e1868e-e7d0-c24e-85c6-7cbe04486764", "parent": group,
               "path": ["New Section Group", "New Section 1"], "color": "#9bbbd2",
               "pages": [{"id": "3fdc5978-8647-8e4c-85de-356300ee58ab", "title": "Test Page 2"}]}),
        json!({"line": "section", "id": "18481a4a-6857-0748-ac96-6327c4ada7c0", "parent": group,
               "path": ["New Section Group", "New Section 2"], "color": "#ab9cb7",
               "pages": [{"id": "e9e2b25d-4462-ea4f-a86c-f167efa63e0d", "title": "Test Page 3"},
                         {"id": "7198bad9-4462-a34e-b5a2-44b2b78e4e60", "title": "Test Page 4"}]}),
    ]);
    expected["warnings"] = json!([
        "/OneNote_RecycleBin: the recycle bin is skipped; its sections and pages are not read"
    ]);
    expected
}

#[test]
fn every_committed_notebook_has_an_expectation() {
    let mut listed: Vec<String> = expectations()
        .iter()
        .map(|e| e["file"].as_str().unwrap().to_owned())
        .collect();
    listed.sort_unstable();
    let mut tocs = Vec::new();
    for set in ["interop", "onenote-rs"] {
        for file in files_under(&fixture(set)) {
            if file.ends_with(".onetoc2") {
                tocs.push(format!("{set}/{file}"));
            }
        }
    }
    tocs.sort_unstable();
    assert_eq!(listed, tocs);
}

#[test]
fn every_committed_notebook_reads_as_expected() {
    for expected in expectations() {
        let file = expected["file"].as_str().unwrap();
        check(&run(&[fixture(file)]), &expected, "notebook", file);
    }
}

#[test]
fn a_notebook_directory_reads_as_its_toc() {
    for dir in DIRECTORIES {
        let by_dir = run(&[fixture(dir)]);
        let by_toc = run(&[fixture(dir).join("Open Notebook.onetoc2")]);
        assert_eq!(by_dir.code, Some(0), "{dir}: {}", by_dir.stderr);
        assert_eq!(by_dir.stdout, by_toc.stdout, "{dir}");
        assert_eq!(by_dir.stderr, by_toc.stderr, "{dir}");
    }
}

#[test]
fn a_table_of_contents_without_its_sections_warns_at_the_root() {
    let alone = scratch("toc-alone");
    let toc = alone.join("Open Notebook.onetoc2");
    std::fs::copy(fixture("onenote-rs/non-legacy/Open Notebook.onetoc2"), &toc).unwrap();
    let out = run(&[&toc]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    assert_eq!(out.lines().len(), 1);
    assert_eq!(out.lines()[0]["line"], "notebook");
    assert_eq!(
        out.stderr,
        format!("{NAME}: warning: /: no section or section group is read\n")
    );
}

#[test]
fn a_section_group_holds_its_sections() {
    let out = run(&[composed()]);
    check(&out, &composed_expectation(), "notebook", "composed");
    assert!(!out.stdout.contains("OneNote_DeletedPages"));
}

#[test]
fn a_package_reads_as_its_directorys_golden() {
    let packages = scratch("packages");
    for dir in DIRECTORIES {
        let input = format!("{dir}/Open Notebook.onetoc2");
        let (stdout, stderr) = goldens(&input);
        let by_package = run(&[package(&fixture(dir), &packages)]);
        assert_eq!(by_package.code, Some(0), "{dir}: {}", by_package.stderr);
        assert_eq!(by_package.stderr, read_golden(&stderr, &input), "{dir}");
        let golden = read_golden(&stdout, &input);
        let (first, rest) = by_package.stdout.split_once('\n').unwrap();
        let (golden_first, golden_rest) = golden.split_once('\n').unwrap();
        assert_eq!(rest, golden_rest, "{dir}");
        let mut first: Value = serde_json::from_str(first).unwrap();
        assert_eq!(first["input"], "package", "{dir}");
        first["input"] = json!("notebook");
        assert_eq!(
            first,
            serde_json::from_str::<Value>(golden_first).unwrap(),
            "{dir}"
        );
    }
    let composed = composed();
    let by_dir = run(&[&composed]);
    let by_package = run(&[package(&composed, &packages)]);
    assert_eq!(by_package.code, Some(0), "{}", by_package.stderr);
    assert_eq!(by_package.stderr, by_dir.stderr);
    let (mut lines, want) = (by_package.lines(), by_dir.lines());
    assert_eq!(lines[0]["input"], "package");
    lines[0]["input"] = json!("notebook");
    assert_eq!(lines, want);
    let recycled = run(&[packages.join("onenote-rs.onepkg")]);
    assert!(
        recycled
            .stderr
            .contains("/OneNote_RecycleBin: the recycle bin is skipped")
    );
}

#[test]
fn every_notebook_read_keeps_the_line_rules_and_repeats_byte_for_byte() {
    let mut inputs: Vec<PathBuf> = expectations()
        .iter()
        .map(|e| fixture(e["file"].as_str().unwrap()))
        .collect();
    inputs.push(composed());
    for input in inputs {
        let name = input.display().to_string();
        let first = run(&[&input]);
        let second = run(&[&input]);
        assert_eq!(first.stdout, second.stdout, "{name}");
        for line in first.lines() {
            assert_line_rules(&line, &name);
        }
    }
}

fn check(out: &Run, expected: &Value, input: &str, file: &str) {
    assert_eq!(
        out.code,
        expected["code"].as_i64().map(|c| c as i32),
        "{file}"
    );
    let warnings: String = expected["warnings"]
        .as_array()
        .unwrap()
        .iter()
        .map(|w| format!("{NAME}: warning: {}\n", w.as_str().unwrap()))
        .collect();
    assert_eq!(out.stderr, warnings, "{file}");

    let mut notebook = json!({"line": "notebook", "schema": "2", "input": input});
    for (field, value) in expected["notebook"].as_object().unwrap() {
        notebook[field] = value.clone();
    }
    let mut want = vec![notebook];
    for entry in expected["entries"].as_array().unwrap() {
        let mut line = entry.clone();
        let pages = line.as_object_mut().unwrap().remove("pages");
        want.push(line);
        for page in pages
            .as_ref()
            .and_then(Value::as_array)
            .into_iter()
            .flatten()
        {
            let mut line = json!({"line": "page", "id": page["id"], "section": entry["id"]});
            if let Some(title) = page.get("title") {
                line["title"] = title.clone();
            }
            want.push(line);
        }
    }
    let lines: Vec<Value> = out.lines().into_iter().map(projected).collect();
    assert_eq!(lines, want, "{file}");
}

fn projected(line: Value) -> Value {
    if line["line"] != "page" {
        return line;
    }
    let mut page = json!({"line": "page", "id": line["id"], "section": line["section"]});
    if let Some(title) = line.get("title") {
        page["title"] = title.clone();
    }
    page
}

fn composed() -> PathBuf {
    static COMPOSED: OnceLock<PathBuf> = OnceLock::new();
    COMPOSED
        .get_or_init(|| {
            let dir = scratch("composed");
            for (from, into) in [
                ("onenote-rs/non-legacy", ""),
                ("onenote-rs/New Section Group", "New Section Group"),
                ("onenote-rs/OneNote_RecycleBin", "OneNote_RecycleBin"),
            ] {
                std::fs::create_dir_all(dir.join(into)).unwrap();
                for file in files_under(&fixture(from)) {
                    std::fs::copy(fixture(from).join(&file), dir.join(into).join(&file)).unwrap();
                }
            }
            dir
        })
        .clone()
}

fn package(dir: &Path, into: &Path) -> PathBuf {
    let files: Vec<String> = files_under(dir)
        .into_iter()
        .filter(|file| file.ends_with(".one") || file.ends_with(".onetoc2"))
        .collect();
    let mut builder = cab::CabinetBuilder::new();
    let folder = builder.add_folder(cab::CompressionType::MsZip);
    for file in &files {
        folder.add_file(file.replace('/', "\\"));
    }
    let mut writer = builder.build(Cursor::new(Vec::new())).unwrap();
    while let Some(mut entry) = writer.next_file().unwrap() {
        let file = entry.file_name().replace('\\', "/");
        entry
            .write_all(&std::fs::read(dir.join(file)).unwrap())
            .unwrap();
    }
    let bytes = writer.finish().unwrap().into_inner();
    let name = dir.file_name().unwrap().to_str().unwrap();
    let path = into.join(format!("{name}.onepkg"));
    std::fs::write(&path, bytes).unwrap();
    path
}
