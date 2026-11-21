//! What the tests share: running the built binary, comparing a run with its goldens, and the
//! paths of fixtures, goldens and test-built inputs.

#![allow(
    dead_code,
    reason = "each test crate compiles this module and uses only part of it"
)]

use std::ffi::OsStr;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};

use serde_json::Value;

const BIN: &str = env!("CARGO_BIN_EXE_docproc-onenote");

pub const NAME: &str = "docproc-onenote";

pub struct Run {
    pub code: Option<i32>,
    pub stdout: String,
    pub stderr: String,
}

impl Run {
    pub fn lines(&self) -> Vec<Value> {
        self.stdout
            .lines()
            .map(|line| serde_json::from_str(line).expect("each stdout line is JSON"))
            .collect()
    }
}

pub fn run<S: AsRef<OsStr>>(args: &[S]) -> Run {
    finish(Command::new(BIN).args(args))
}

pub fn run_in<S: AsRef<OsStr>>(dir: &Path, args: &[S]) -> Run {
    finish(Command::new(BIN).args(args).current_dir(dir))
}

pub fn run_to<S: AsRef<OsStr>>(stdout: impl Into<Stdio>, args: &[S]) -> Run {
    finish(Command::new(BIN).args(args).stdout(stdout))
}

fn finish(command: &mut Command) -> Run {
    let out = command.output().unwrap();
    Run {
        code: out.status.code(),
        stdout: String::from_utf8(out.stdout).expect("stdout is UTF-8"),
        stderr: String::from_utf8(out.stderr).expect("stderr is UTF-8"),
    }
}

pub fn fixture(path: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures")
        .join(path)
}

pub fn built(name: impl AsRef<Path>, bytes: &[u8]) -> PathBuf {
    let path = Path::new(env!("CARGO_TARGET_TMPDIR")).join(name);
    std::fs::write(&path, bytes).unwrap();
    path
}

pub fn patched(path: &str, offset: usize, value: u8, name: &str) -> PathBuf {
    let mut bytes = std::fs::read(fixture(path)).unwrap();
    bytes[offset] = value;
    built(name, &bytes)
}

pub fn scratch(name: &str) -> PathBuf {
    let path = Path::new(env!("CARGO_TARGET_TMPDIR")).join(name);
    if path.exists() {
        std::fs::remove_dir_all(&path).unwrap();
    }
    std::fs::create_dir_all(&path).unwrap();
    path
}

pub fn error(reason: &str) -> String {
    format!("{NAME}: error: {reason}\n")
}

pub fn fixtures() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures")
}

pub fn goldens(input: &str) -> (PathBuf, PathBuf) {
    goldens_of(input, &[])
}

pub fn goldens_of(input: &str, options: &[&str]) -> (PathBuf, PathBuf) {
    let base = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/golden")
        .join(input);
    let with = |extension: &str| {
        let mut name = base.clone().into_os_string();
        for option in options {
            name.push(format!(".{}", option.trim_start_matches('-')));
        }
        name.push(extension);
        PathBuf::from(name)
    };
    (with(".jsonl"), with(".stderr"))
}

pub fn read_golden(path: &Path, input: &str) -> String {
    std::fs::read_to_string(path).unwrap_or_else(|err| panic!("{input}: {}: {err}", path.display()))
}

pub fn golden(input: &str, code: i32) {
    golden_with(input, &[], code);
}

pub fn golden_with(input: &str, options: &[&str], code: i32) {
    let mut args = options.to_vec();
    args.push(input);
    let out = run_in(&fixtures(), &args);
    assert_eq!(out.code, Some(code), "{input} {options:?}: {}", out.stderr);
    let (stdout, stderr) = goldens_of(input, options);
    if std::env::var_os("UPDATE_GOLDEN").is_some() {
        std::fs::create_dir_all(stdout.parent().unwrap()).unwrap();
        std::fs::write(&stdout, &out.stdout).unwrap();
        std::fs::write(&stderr, &out.stderr).unwrap();
        return;
    }
    assert!(
        out.stdout == read_golden(&stdout, input),
        "{input}: stdout differs from {}",
        stdout.display()
    );
    assert_eq!(out.stderr, read_golden(&stderr, input), "{input}: stderr");
}

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

pub fn files_under(dir: &Path) -> Vec<String> {
    let mut found = Vec::new();
    let mut pending = vec![dir.to_path_buf()];
    while let Some(next) = pending.pop() {
        for entry in std::fs::read_dir(&next).unwrap() {
            let path = entry.unwrap().path();
            if path.is_dir() {
                pending.push(path);
            } else {
                let relative = path.strip_prefix(dir).unwrap();
                found.push(relative.to_str().unwrap().to_owned());
            }
        }
    }
    found.sort_unstable();
    found
}

pub fn assert_line_rules(line: &Value, file: &str) {
    assert!(line["line"].is_string(), "{file}: {line}");
    assert_no_empty(line, file);
    for field in ["creation_time", "modification_time"] {
        if let Some(time) = line.get(field) {
            assert!(is_iso8601_second(time.as_str().unwrap()), "{file}: {time}");
        }
    }
}

fn assert_no_empty(value: &Value, file: &str) {
    match value {
        Value::Null => panic!("{file}: a null"),
        Value::String(text) => assert!(!text.is_empty(), "{file}: an empty string"),
        Value::Array(items) => {
            assert!(!items.is_empty(), "{file}: an empty list");
            items.iter().for_each(|item| assert_no_empty(item, file));
        }
        Value::Object(fields) => {
            assert!(!fields.is_empty(), "{file}: an empty object");
            fields
                .values()
                .for_each(|field| assert_no_empty(field, file));
        }
        Value::Bool(_) | Value::Number(_) => {}
    }
}

fn is_iso8601_second(time: &str) -> bool {
    let bytes = time.as_bytes();
    bytes.len() == 20
        && bytes.iter().enumerate().all(|(i, &b)| match i {
            4 | 7 => b == b'-',
            10 => b == b'T',
            13 | 16 => b == b':',
            19 => b == b'Z',
            _ => b.is_ascii_digit(),
        })
}
