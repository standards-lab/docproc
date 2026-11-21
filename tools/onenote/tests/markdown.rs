//! A page line's markdown, read from the public fixtures. The test page of "New Section 1.one"
//! is written out in full, by hand from what onenote_parser's API gives for it and the rendering
//! rules, not from the CLI's output: it is the hand truth of the page's text, its headings, runs,
//! links, note tags, lists, tables, equation and image. The other pages are checked for their
//! Markdown's shape.

mod common;

use serde_json::Value;

use common::{fixture, run};

const LOREM: &str = "Lorem ipsum dolor sit amet, consetetur sadipscing elitr, sed diam nonumy \
                     eirmod tempor invidunt ut labore et dolore magna aliquyam erat, sed diam \
                     voluptua.";

fn pages(file: &str) -> Vec<Value> {
    let out = run(&[fixture(file)]);
    assert_eq!(out.code, Some(0), "{file}: {}", out.stderr);
    out.lines()
        .into_iter()
        .filter(|line| line["line"] == "page")
        .collect()
}

fn markdown(page: &Value) -> &str {
    page["markdown"].as_str().unwrap_or_default()
}

fn test_page(ink: &str) -> String {
    let lorem3 = [LOREM; 3];
    [
        "# Test Page",
        "Tuesday, 27. October 2020 11:47",
        "ABCDEF",
        "## ABCDEF",
        "- [ ] [To Do] ABCDEF\n- [x] [To Do] ABCDEFG",
        "[Important] ABCDEFGH",
        "**Lorem** ipsum *dolor* sit amet, consetetur ~~sadipscing~~ elitr, sed diam nonumy \
         eirmod tempor invidunt ut labore et dolore [magna](https://example.com) aliquyam \
         erat, sed diam voluptua.",
        LOREM,
        LOREM,
        "[http://example.com/](http://example.com/)",
        &format!("- {}\n  - {}\n    - {}", lorem3[0], lorem3[1], lorem3[2]),
        &format!(
            "1. {LOREM}\n   1. {LOREM}\n      1. {LOREM}\n2. {LOREM}\n3. {LOREM}\n\n<!-- -->\n\n1. {LOREM}"
        ),
        LOREM,
        LOREM,
        "| A | B | C |\n| --- | --- | --- |\n| 1 | 2 | 3 |",
        "| A | B |\n| --- | --- |",
        "$$a=b$$",
        "![example images from TESTIMAGES archive]\
         (d6d4898c203cbff35fe92e844bbf404064293314a71d1e6b24e907334e5bdff9.jpg)",
        ink,
    ]
    .join("\n\n")
}

fn ink_file(page: &Value) -> &str {
    page["media"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|medium| medium["kind"] == "ink")
        .and_then(|medium| medium["file"].as_str())
        .expect("the page has ink")
}

#[test]
fn the_test_page_renders_in_full() {
    let pages = pages("onenote-rs/New Section 1.one");
    let ink = format!("![ink]({})", ink_file(&pages[0]));
    assert_eq!(markdown(&pages[0]), test_page(&ink));
}

const SECOND_PAGE: &str = "# Test Page\n\nMonday, 2. November 2020 14:45";

#[test]
fn recognised_ink_is_the_ink_lines_alt_text() {
    let pages = pages("onenote-rs/handwriting_recognition.one");
    let ink = format!("![ink: Hello World Hello World]({})", ink_file(&pages[0]));
    assert_eq!(markdown(&pages[0]), test_page(&ink));
    assert_eq!(markdown(&pages[1]), SECOND_PAGE);
}

#[test]
fn the_other_pages_render_their_titles_text_and_media() {
    let alternative = pages("interop/Resources/AlternativePackaging.one");
    assert_eq!(
        markdown(&alternative[0]),
        "# Test Suite Sample\n\n2021年7月15日 17:07\n\nTest Suite Sample"
    );

    let without = pages("interop/SUT/OneWithoutFileData.one");
    assert_eq!(
        markdown(&without[0]),
        "# The OneNote file without file data\n\n2019年1月14日 14:59\n\n\
         This is a test file which does not contain the file data."
    );

    let with = &pages("interop/SUT/OneWithFileData.one")[0];
    let docx = with["media"][0]["file"].as_str().unwrap();
    assert_eq!(
        markdown(with),
        format!("2019年3月14日 17:06\n\n[testing.docx]({docx})")
    );

    let wopi = &pages("interop/SUT/FileSyncWOPI_OneNoteWithODB.one")[0];
    let md = markdown(wopi);
    assert!(
        md.starts_with("2013年12月10日 11:10\n\n![尹、气 歹杯 "),
        "{md}"
    );
    let image = wopi["media"][0]["file"].as_str().unwrap();
    assert!(
        md.ends_with(&format!(" Z! 卜传州l LLI lri]({image})")),
        "{md}"
    );
    assert!(!md.contains('#'), "an untitled page has no heading: {md}");

    let group = pages("onenote-rs/New Section Group/New Section 1.one");
    assert_eq!(
        markdown(&group[0]),
        "# Test Page 2\n\nTuesday, 27. October 2020 11:47\n\nTest 1\n\nTest 2"
    );

    let media = pages("onenote-rs/New Section Group/New Section 2.one");
    let md = markdown(&media[0]);
    assert!(
        md.starts_with("# Test Page 3\n\nTuesday, 27. October 2020 11:47\n\n![]("),
        "{md}"
    );
    assert!(md.ends_with(".png)"), "{md}");
    let md = markdown(&media[1]);
    assert!(
        md.starts_with(
            "# Test Page 4\n\nTuesday, 27. October 2020 11:53\n\n[ff-16b-2c-44100hz.mp3]("
        ),
        "{md}"
    );
    assert!(md.ends_with(".mp3)"), "{md}");

    let deleted = pages("onenote-rs/OneNote_RecycleBin/OneNote_DeletedPages.one");
    assert_eq!(
        markdown(&deleted[0]),
        "# Te\n\nTuesday, 27. October 2020 11:47"
    );

    for (file, dates) in [
        ("interop/SUT/InvalidData.one", &["2019年1月22日 11:24"][..]),
        (
            "onenote-rs/non-legacy/New Section 2.one",
            &[
                "Montag, 2. November 2020 16:06",
                "Sunday, 28. December 2025 13:58",
            ][..],
        ),
        (
            "onenote-rs/non-legacy/New Section 3.one",
            &["Sunday, 28. December 2025 13:58"][..],
        ),
    ] {
        let pages = pages(file);
        let read: Vec<&str> = pages.iter().map(markdown).collect();
        assert_eq!(read, dates, "{file}");
    }

    let copy = pages("onenote-rs/non-legacy/New Section 1 2.one");
    let ink = format!("![ink]({})", ink_file(&copy[0]));
    assert_eq!(markdown(&copy[0]), test_page(&ink));
    assert_eq!(markdown(&copy[1]), SECOND_PAGE);
}
