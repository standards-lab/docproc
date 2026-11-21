//! The goldens: every committed OneStore input, a section or a table of contents, run through the
//! binary from tests/fixtures with its relative path, exits as it should with the stdout and
//! stderr its goldens under tests/golden hold, byte for byte, and every golden has its input. A
//! table of contents is also read with `--file-only`, which refuses it. The `golden` task
//! re-records them (UPDATE_GOLDEN); the check only compares.

mod common;

use std::path::Path;

use common::{files_under, fixtures, golden, golden_with, goldens, goldens_of};

const INPUTS: [&str; 2] = [".one", ".onetoc2"];

const REFUSED: [(&str, i32); 1] = [("interop/SUT/Encryption.one", 3)];

const FILE_ONLY: &str = "interop/SUT/Open Notebook.onetoc2";

fn inputs() -> Vec<String> {
    let mut found = files_under(&fixtures());
    found.retain(|file| INPUTS.iter().any(|extension| file.ends_with(extension)));
    found
}

#[test]
fn every_input_matches_its_goldens_and_no_golden_is_left_over() {
    let inputs = inputs();
    assert_eq!(inputs.len(), 23, "{inputs:#?}");
    for input in &inputs {
        let code = REFUSED
            .iter()
            .find(|(refused, _)| refused == input)
            .map_or(0, |&(_, code)| code);
        golden(input, code);
    }
    golden_with(FILE_ONLY, &["--file-only"], 3);
    let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/golden");
    let found = files_under(&root);
    let mut expected: Vec<String> = inputs
        .iter()
        .flat_map(|input| {
            let (stdout, stderr) = goldens(input);
            [stdout, stderr].map(|golden| {
                let relative = golden.strip_prefix(&root).unwrap();
                relative.to_str().unwrap().to_owned()
            })
        })
        .collect();
    let (stdout, stderr) = goldens_of(FILE_ONLY, &["--file-only"]);
    for golden in [stdout, stderr] {
        let relative = golden.strip_prefix(&root).unwrap();
        expected.push(relative.to_str().unwrap().to_owned());
    }
    expected.sort_unstable();
    assert_eq!(found, expected);
}
