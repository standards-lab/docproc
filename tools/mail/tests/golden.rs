//! The goldens: every committed input, run through the binary, exits 0 with the stdout and
//! stderr its goldens under tests/golden hold, byte for byte, and every golden has its input.
//! The `golden` task re-records them (UPDATE_GOLDEN); the check only compares.

mod common;

use std::path::Path;

/// The extensions of the committed inputs. Everything else under tests/fixtures is a truth, a
/// source, a manifest, a license or notice, or one of mime4j's expected splits of mbox.rlug
/// (`mbox.rlug-0` to `-4`), which tests/mbox.rs compares with the CLI's read of mbox.rlug.
const INPUTS: &[&str] = &["pst", "msg", "eml", "mboxrd", "rlug"];

/// Adds to `found` the paths under `dir`, relative to `root`, of the files whose extension is one
/// of `extensions`.
fn files(root: &Path, dir: &Path, extensions: &[&str], found: &mut Vec<String>) {
    for entry in std::fs::read_dir(dir).unwrap() {
        let path = entry.unwrap().path();
        if path.is_dir() {
            files(root, &path, extensions, found);
        } else if path
            .extension()
            .and_then(|extension| extension.to_str())
            .is_some_and(|extension| extensions.contains(&extension))
        {
            let relative = path.strip_prefix(root).unwrap();
            found.push(relative.to_str().unwrap().to_owned());
        }
    }
}

/// The committed inputs, by path under tests/fixtures.
fn inputs() -> Vec<String> {
    let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures");
    let mut found = Vec::new();
    files(&root, &root, INPUTS, &mut found);
    found.sort_unstable();
    found
}

/// Each input matches its goldens; then every golden names a committed input, so a removed or
/// renamed input leaves none behind. One test, so a re-record has written every golden before
/// they are listed.
#[test]
fn every_input_matches_its_goldens_and_no_golden_is_left_over() {
    let inputs = inputs();
    assert_eq!(inputs.len(), 34, "{inputs:#?}");
    for input in &inputs {
        common::golden(input);
    }
    let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/golden");
    let mut goldens = Vec::new();
    files(&root, &root, &["jsonl", "stderr"], &mut goldens);
    goldens.sort_unstable();
    let mut expected: Vec<String> = inputs
        .iter()
        .flat_map(|input| {
            let input = Path::new(input);
            ["jsonl", "stderr"]
                .map(|extension| input.with_extension(extension).to_str().unwrap().to_owned())
        })
        .collect();
    expected.sort_unstable();
    assert_eq!(goldens, expected);
}
