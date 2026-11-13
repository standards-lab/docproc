//! What the tests share: running the built binary, comparing a run with its goldens, and the
//! paths of fixtures, goldens and test-built inputs.

#![allow(
    dead_code,
    reason = "each test crate compiles this module and uses only part of it"
)]

pub mod msg;

use std::path::{Path, PathBuf};
use std::process::Command;

const BIN: &str = env!("CARGO_BIN_EXE_docproc-mail");

/// One run of the binary: its exit code, stdout and stderr.
pub struct Run {
    pub code: Option<i32>,
    pub stdout: String,
    pub stderr: String,
}

impl Run {
    /// stdout's lines, each parsed as JSON.
    pub fn lines(&self) -> Vec<serde_json::Value> {
        self.stdout
            .lines()
            .map(|line| serde_json::from_str(line).expect("each stdout line is JSON"))
            .collect()
    }
}

pub fn run<S: AsRef<std::ffi::OsStr>>(args: &[S]) -> Run {
    finish(Command::new(BIN).args(args))
}

/// One run of the binary with `dir` as its working directory.
pub fn run_in<S: AsRef<std::ffi::OsStr>>(dir: &Path, args: &[S]) -> Run {
    finish(Command::new(BIN).current_dir(dir).args(args))
}

fn finish(command: &mut Command) -> Run {
    let out = command.output().unwrap();
    Run {
        code: out.status.code(),
        stdout: String::from_utf8(out.stdout).expect("stdout is UTF-8"),
        stderr: String::from_utf8(out.stderr).expect("stderr is UTF-8"),
    }
}

/// Asserts that no value in `value`, at any depth, is null: a property with no value is left
/// out of a line, never written as null.
pub fn assert_no_nulls(value: &serde_json::Value, what: &str) {
    match value {
        serde_json::Value::Null => panic!("{what}: a null was written"),
        serde_json::Value::Array(items) => {
            items.iter().for_each(|item| assert_no_nulls(item, what))
        }
        serde_json::Value::Object(fields) => {
            fields.values().for_each(|item| assert_no_nulls(item, what));
        }
        _ => {}
    }
}

/// A committed fixture, by its path under tests/fixtures.
pub fn fixture(path: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures")
        .join(path)
}

/// Runs the binary over the committed input `input`, by its path under tests/fixtures, asserts
/// that it exits 0, and compares its stdout and stderr, byte for byte, with its goldens: the
/// input's path under tests/golden, its extension `.jsonl` and `.stderr`. With UPDATE_GOLDEN set
/// (the `golden` task), it writes the goldens instead; their diff is reviewed before committing.
pub fn golden(input: &str) {
    let out = run(&[fixture(input)]);
    assert_eq!(out.code, Some(0), "{input}: {}", out.stderr);
    let base = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/golden")
        .join(input);
    let (stdout, stderr) = (base.with_extension("jsonl"), base.with_extension("stderr"));
    if std::env::var_os("UPDATE_GOLDEN").is_some() {
        std::fs::create_dir_all(base.parent().unwrap()).unwrap();
        std::fs::write(&stdout, &out.stdout).unwrap();
        std::fs::write(&stderr, &out.stderr).unwrap();
        return;
    }
    let read = |path: &Path| {
        std::fs::read_to_string(path)
            .unwrap_or_else(|err| panic!("{input}: {}: {err}", path.display()))
    };
    assert!(
        out.stdout == read(&stdout),
        "{input}: stdout differs from {}",
        stdout.display()
    );
    assert_eq!(out.stderr, read(&stderr), "{input}: stderr");
}

/// What a truth's `body` (its `text` and `html`, as a source holds them) says of a message
/// line's `markdown`, schema "2" carrying no body of its own: with an HTML body, it is htmd's
/// rendering of that HTML, each `cid:` image source naming an attachment of the line, by its
/// content_id, read as that attachment's file, then, when the text body holds sentences that
/// rendering lacks, a paragraph of the content rule's appended sentences, whose words are the
/// text body's, in its order; with a text body alone, it reads as that text, word for word, once
/// its backslash escapes are read; with neither, there is none.
pub fn assert_markdown(got: &serde_json::Value, body: &serde_json::Value, source: &str) {
    assert_eq!(got.get("body"), None, "{source}: no body");
    let markdown = got.get("markdown").map(|text| text.as_str().unwrap());
    if let Some(html) = body.get("html") {
        let mut html = html.as_str().unwrap().to_owned();
        let attachments = got.get("attachments").and_then(|list| list.as_array());
        for attachment in attachments.into_iter().flatten() {
            if let (Some(id), Some(file)) = (
                attachment.get("content_id").and_then(|id| id.as_str()),
                attachment.get("file").and_then(|file| file.as_str()),
            ) {
                html = html.replace(&format!("\"cid:{id}\""), &format!("\"{file}\""));
            }
        }
        let html = htmd::convert(&html).unwrap();
        let markdown = markdown.unwrap_or_else(|| panic!("{source}: no markdown"));
        let appended = markdown
            .strip_prefix(html.as_str())
            .unwrap_or_else(|| panic!("{source}: markdown {markdown:?} is not htmd's {html:?}"));
        if !appended.is_empty() {
            let paragraph = appended
                .strip_prefix("\n\n")
                .filter(|paragraph| !paragraph.contains('\n'))
                .unwrap_or_else(|| panic!("{source}: {appended:?} is no appended paragraph"));
            let text = body
                .get("text")
                .and_then(|text| text.as_str())
                .unwrap_or("");
            let mut words = text.split_whitespace();
            for word in words_of_markdown(paragraph) {
                assert!(
                    words.any(|text| text == word),
                    "{source}: appended {word:?} is not the text body's, in its order"
                );
            }
        }
    } else if let Some(text) = body.get("text") {
        let text: Vec<String> = text
            .as_str()
            .unwrap()
            .split_whitespace()
            .map(str::to_owned)
            .collect();
        assert_eq!(
            markdown.map(words_of_markdown),
            Some(text),
            "{source}: markdown"
        );
    } else {
        assert_eq!(markdown, None, "{source}: markdown");
    }
}

/// A Markdown text's words, read as plain text: each backslash escape (a backslash before ASCII
/// punctuation, CommonMark 2.4) read as the character it escapes, then split at whitespace, so a
/// hard line break's trailing spaces are no word.
pub fn words_of_markdown(markdown: &str) -> Vec<String> {
    let mut text = String::with_capacity(markdown.len());
    let mut chars = markdown.chars().peekable();
    while let Some(c) = chars.next() {
        match chars.peek() {
            Some(&next) if c == '\\' && next.is_ascii_punctuation() => {
                text.push(next);
                chars.next();
            }
            _ => text.push(c),
        }
    }
    text.split_whitespace().map(str::to_owned).collect()
}

/// A test-built input, written under Cargo's temporary directory for integration tests.
pub fn built(name: &str, bytes: &[u8]) -> PathBuf {
    let path = Path::new(env!("CARGO_TARGET_TMPDIR")).join(name);
    std::fs::write(&path, bytes).unwrap();
    path
}

/// An empty directory under Cargo's temporary directory for integration tests, emptied if a
/// previous run left it.
pub fn scratch(name: &str) -> PathBuf {
    let path = Path::new(env!("CARGO_TARGET_TMPDIR")).join(name);
    if path.exists() {
        std::fs::remove_dir_all(&path).unwrap();
    }
    std::fs::create_dir_all(&path).unwrap();
    path
}

/// The names of the files in `dir`, sorted.
pub fn names(dir: &Path) -> Vec<String> {
    let mut names: Vec<String> = std::fs::read_dir(dir)
        .unwrap()
        .map(|entry| entry.unwrap().file_name().into_string().unwrap())
        .collect();
    names.sort_unstable();
    names
}

/// A file's lowercase hex SHA-256, by sha256sum, so the tests take no hashing crate.
pub fn sha256(path: &Path) -> String {
    let out = Command::new("sha256sum").arg(path).output().unwrap();
    assert!(out.status.success(), "sha256sum {}", path.display());
    String::from_utf8(out.stdout)
        .unwrap()
        .split_whitespace()
        .next()
        .unwrap()
        .to_owned()
}
