//! The command line: arguments, usage errors, the input's sniffing, the inputs refused with
//! exit 3, and the read thread's fault.

mod common;

use std::ffi::OsStr;
use std::fs::File;
use std::os::unix::ffi::OsStrExt;
use std::os::unix::process::CommandExt;
use std::path::Path;
use std::process::Command;

use common::{NAME, built, error, fixture, patched, run, run_to, scratch};

const USAGE: &str = "usage: docproc-onenote [--media <dir>] [--file-only] <input>
       docproc-onenote --version
       docproc-onenote --help
";

/// The fixed reason a table of contents is refused with under `--file-only`.
const TOC_REFUSED: &str = "a table of contents; its sections are read on their own";

/// A small section of each format: desktop (Interop's) and OneDrive (onenote.rs's).
const DESKTOP: &str = "interop/SUT/OneWithoutFileData.one";
const ONEDRIVE: &str = "onenote-rs/New Section 1.one";

/// The bare crate version on the first line, which a release compares with its tag's, and the
/// schema on the second.
#[test]
fn version_prints_the_bare_crate_version_then_the_schema() {
    let out = run(&["--version"]);
    assert_eq!(out.code, Some(0));
    assert_eq!(
        out.stdout,
        format!("{}\nschema 2\n", env!("CARGO_PKG_VERSION"))
    );
    assert_eq!(out.stderr, "");
}

/// `--help` prints the usage to stdout, nothing to stderr, and exits 0.
#[test]
fn help_prints_the_usage_to_stdout() {
    let out = run(&["--help"]);
    assert_eq!(out.code, Some(0));
    assert_eq!(out.stdout, USAGE);
    assert_eq!(out.stderr, "");
}

/// Each usage error exits 2 with one error line, then the usage, on stderr, and nothing on stdout.
#[test]
fn usage_errors_exit_2_with_usage() {
    let section = fixture(DESKTOP);
    let section = section.to_str().unwrap();
    let not_a_dir = fixture("interop/LICENSE");
    let not_a_dir = not_a_dir.to_str().unwrap();
    let cases: &[(&[&str], &str)] = &[
        (&[], "no input"),
        (&[section, section], "more than one input"),
        (&["--frobnicate", section], "unknown option --frobnicate"),
        (
            &["--version", section],
            "--version takes no other arguments",
        ),
        (
            &[section, "--version"],
            "--version takes no other arguments",
        ),
        (&[section, "--help"], "--help takes no other arguments"),
        (&["--help", "--version"], "--help takes no other arguments"),
        (
            &["--help", "--file-only"],
            "--help takes no other arguments",
        ),
        (&["--file-only"], "no input"),
        (
            &["--file-only", section, "--file-only"],
            "--file-only is given more than once",
        ),
        (&[section, "--media"], "--media needs a directory"),
        (&["--media", "", section], "--media needs a directory"),
        (
            &["--media", "a", "--media", "b", section],
            "--media is given more than once",
        ),
        (
            &["--media", not_a_dir, section],
            &format!("--media {not_a_dir} is not a directory"),
        ),
    ];
    for (args, reason) in cases {
        let out = run(args);
        assert_eq!(out.code, Some(2), "{args:?}");
        assert_eq!(out.stdout, "", "{args:?}");
        let first = out.stderr.lines().next().unwrap_or_default();
        assert_eq!(first, format!("{NAME}: error: {reason}"), "{args:?}");
        assert_eq!(
            out.stderr,
            format!("{NAME}: error: {reason}\n{USAGE}"),
            "{args:?}"
        );
    }
}

/// --media is accepted before or after the input, and a directory that doesn't exist yet is
/// accepted.
#[test]
fn media_is_accepted_with_the_input() {
    let section = fixture(DESKTOP);
    let dir = Path::new(env!("CARGO_TARGET_TMPDIR")).join("media-unmade");
    let without = run(&[&section]);
    assert_eq!(without.code, Some(0), "{}", without.stderr);
    for args in [
        [section.as_os_str(), "--media".as_ref(), dir.as_os_str()],
        ["--media".as_ref(), dir.as_os_str(), section.as_os_str()],
    ] {
        let out = run(&args);
        assert_eq!(out.code, Some(0), "{}", out.stderr);
        assert_eq!(out.stdout, without.stdout);
    }
}

/// The arguments are read as OsString: an input whose name isn't UTF-8 reads, and the notebook
/// line names it with the invalid bytes replaced.
#[test]
fn an_input_whose_name_is_not_utf8_reads() {
    let name = OsStr::from_bytes(b"caf\xe9.one");
    let input = built(name, &std::fs::read(fixture(DESKTOP)).unwrap());
    let out = run(&[&input]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    assert_eq!(out.lines()[0]["name"], "caf\u{fffd}");
}

/// The input is sniffed from its content, never its name: a section named as a ZIP or a text
/// file reads as the section, and text named as a section is refused as unknown content.
#[test]
fn the_input_is_sniffed_by_content_not_name() {
    let bytes = std::fs::read(fixture(ONEDRIVE)).unwrap();
    let intact = run(&[fixture(ONEDRIVE)]);
    for name in ["New Section 1.zip", "New Section 1.txt"] {
        let out = run(&[built(name, &bytes)]);
        assert_eq!(out.code, Some(0), "{name}: {}", out.stderr);
        // The section's id and its page lines are the section's. The notebook line names the
        // file's stem, and so does the section line when, as here, the section stores no
        // display name and onenote_parser takes the file's name.
        let (lines, want) = (out.lines(), intact.lines());
        assert_eq!(lines[1]["id"], want[1]["id"], "{name}");
        assert_eq!(lines[2..], want[2..], "{name}");
    }
    let text = built("text.one", b"not OneNote at all\n");
    let out = run(&[&text]);
    assert_eq!(out.code, Some(3));
    assert_eq!(
        out.stderr,
        error("not a OneNote section, notebook, or package")
    );
}

/// Each refused input exits 3 with one error line, which never leads with the input's path, and
/// nothing on stdout.
#[test]
fn refused_inputs_exit_3_before_any_output() {
    let zip = built("download.one", b"PK\x03\x04\x14\x00\x00\x00\x08\x00");
    let empty_zip = built(
        "empty.zip",
        b"PK\x05\x06\0\0\0\0\0\0\0\0\0\0\0\0\0\0\0\0\0\0",
    );
    let empty_dir = scratch("empty-notebook");
    let sections_only = scratch("sections-only");
    std::fs::copy(fixture(DESKTOP), sections_only.join("a.one")).unwrap();
    let several = fixture("interop/Resources");
    let license = fixture("onenote-rs/LICENSE");
    let empty = built("empty.one", b"");
    let cases: &[(&Path, &str)] = &[
        (&zip, "a ZIP archive; extract the OneDrive download first"),
        (
            &empty_zip,
            "a ZIP archive; extract the OneDrive download first",
        ),
        (&empty_dir, "no .onetoc2 in the directory"),
        (&sections_only, "no .onetoc2 in the directory"),
        (
            &several,
            "more than one .onetoc2 in the directory: \
             AlternativePackaging.onetoc2, NoSection.onetoc2, Open Notebook.onetoc2",
        ),
        (&license, "not a OneNote section, notebook, or package"),
        (&empty, "not a OneNote section, notebook, or package"),
    ];
    for (input, reason) in cases {
        let out = run(&[input]);
        assert_eq!(out.code, Some(3), "{}", input.display());
        assert_eq!(out.stdout, "", "{}", input.display());
        assert_eq!(out.stderr, error(reason), "{}", input.display());
    }
}

/// A missing input, and a file or a notebook directory without read permission, are each refused,
/// exit 3 with nothing on stdout, the reason saying which and naming the input's path after it,
/// never leading with it. A directory is no refusal of its own: it is a notebook.
#[test]
fn a_missing_or_unreadable_input_is_refused_by_its_path() {
    let missing = Path::new(env!("CARGO_TARGET_TMPDIR")).join("no-such-section.one");
    let file = Locked::new("locked.one", |path| {
        std::fs::copy(fixture(DESKTOP), path).unwrap();
    });
    let dir = Locked::new("locked-notebook", |path| {
        std::fs::create_dir(path).unwrap();
        std::fs::copy(fixture(DESKTOP), path.join("a.one")).unwrap();
    });
    let mut cases = vec![(&*missing, "no such file")];
    if file.readable() || dir.readable() {
        eprintln!("skipped: an input without permission can be read, as root");
    } else {
        cases.extend([
            (&*file.0, "permission denied"),
            (&*dir.0, "permission denied"),
        ]);
    }
    for (input, which) in cases {
        let out = run(&[input]);
        assert_eq!(out.code, Some(3), "{}", input.display());
        assert_eq!(out.stdout, "", "{}", input.display());
        let reason = format!("{which}: {}", input.display());
        assert_eq!(out.stderr, error(&reason), "{}", input.display());
    }
}

/// An input under Cargo's temporary directory with no permission at all, given its permissions
/// back when dropped, so nothing that walks the tree is stopped by it.
struct Locked(std::path::PathBuf);

impl Locked {
    /// The input `make` writes at `name`, a previous run's removed first, then locked.
    fn new(name: &str, make: impl FnOnce(&Path)) -> Self {
        let path = Path::new(env!("CARGO_TARGET_TMPDIR")).join(name);
        if path.is_dir() {
            std::fs::remove_dir_all(&path).unwrap();
        } else if path.exists() {
            std::fs::remove_file(&path).unwrap();
        }
        make(&path);
        let locked = Self(path);
        locked.set(0o000);
        locked
    }

    /// Whether it can be read regardless, as root.
    fn readable(&self) -> bool {
        if self.0.is_dir() {
            std::fs::read_dir(&self.0).is_ok()
        } else {
            File::open(&self.0).is_ok()
        }
    }

    fn set(&self, mode: u32) {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&self.0, std::fs::Permissions::from_mode(mode)).unwrap();
    }
}

impl Drop for Locked {
    fn drop(&mut self) {
        self.set(0o700);
    }
}

/// The reason a section holding [MS-ONESTORE] 2.5.19's encryption key is refused with.
const ENCRYPTED: &str = "encrypted section (a password-protected section, holding an \
                         [MS-ONESTORE] 2.5.19 encryption key); remove its password in OneNote first";

/// 2.5.19's key constants, as a file stores them: little-endian.
const KEY: [[u8; 8]; 2] = [
    0xFB6B_A385_DAD1_A067_u64.to_le_bytes(),
    0x2649_294F_8E19_8B3C_u64.to_le_bytes(),
];

/// A password-protected section, which the parser fails on, is refused as encrypted, its reason
/// beginning `encrypted`, as the input's path never leads it.
#[test]
fn a_password_protected_section_is_refused_as_encrypted() {
    let input = fixture("interop/SUT/Encryption.one");
    let out = run(&[&input]);
    assert_eq!(out.code, Some(3));
    assert_eq!(out.stdout, "");
    assert_eq!(out.stderr, error(ENCRYPTED));
    assert!(ENCRYPTED.starts_with("encrypted"));
}

/// The scan runs only after a failed parse, and needs both constants: a section that reads is
/// read whatever it holds; one that fails holding a single constant keeps the parser's reason;
/// one that fails holding both is encrypted, whatever made it fail.
#[test]
fn only_a_failed_section_holding_both_constants_is_encrypted() {
    let mut bytes = std::fs::read(fixture(DESKTOP)).unwrap();
    bytes.extend(KEY.concat());
    let out = run(&[built("keyed.one", &bytes)]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    // Its section and page lines are the intact section's; the names are the files'.
    let (lines, want) = (out.lines(), run(&[fixture(DESKTOP)]).lines());
    assert_eq!(lines[1]["id"], want[1]["id"]);
    assert_eq!(lines[2..], want[2..]);

    let mut bytes = std::fs::read(fixture(DESKTOP)).unwrap();
    bytes[1040] = 0xFF;
    let mut half = bytes.clone();
    half.extend(KEY[0]);
    let out = run(&[built("half-keyed.one", &half)]);
    assert_eq!(out.code, Some(3));
    assert_eq!(out.stderr, error(NODE_SIZE));
    bytes.extend(KEY.concat());
    let out = run(&[built("failed-keyed.one", &bytes)]);
    assert_eq!(out.code, Some(3));
    assert_eq!(out.stderr, error(ENCRYPTED));
}

/// Under `--file-only`, a table of contents, by its content whatever its name, and a notebook
/// directory are refused, exit 3, with the fixed reason and nothing on stdout; a section and a
/// package read as they do without it.
#[test]
fn file_only_refuses_a_table_of_contents_and_reads_the_rest() {
    let toc = std::fs::read(fixture("onenote-rs/non-legacy/Open Notebook.onetoc2")).unwrap();
    let toc_as_section = built("FileOnly.one", &toc);
    for input in [
        fixture("onenote-rs/non-legacy/Open Notebook.onetoc2"),
        toc_as_section,
        fixture("onenote-rs/non-legacy"),
    ] {
        let out = run(&["--file-only".as_ref(), input.as_os_str()]);
        assert_eq!(out.code, Some(3), "{}", input.display());
        assert_eq!(out.stdout, "", "{}", input.display());
        assert_eq!(out.stderr, error(TOC_REFUSED), "{}", input.display());
    }
    let section = fixture(DESKTOP);
    let out = run(&[section.as_os_str(), "--file-only".as_ref()]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    assert_eq!(out.stdout, run(&[&section]).stdout);
    // A missing input is refused as missing, not as a table of contents.
    let missing = Path::new(env!("CARGO_TARGET_TMPDIR")).join("no-such-toc.onetoc2");
    let out = run(&["--file-only".as_ref(), missing.as_os_str()]);
    assert_eq!(out.code, Some(3));
    assert_eq!(
        out.stderr,
        error(&format!("no such file: {}", missing.display()))
    );
}

/// One run of the binary over [`DESKTOP`] under an address-space limit of `limit` bytes, with
/// RUST_MIN_STACK, std's default stack for a spawned thread, set to 64 KiB.
fn run_limited(limit: libc::rlim_t) -> std::process::Output {
    let mut command = Command::new(env!("CARGO_BIN_EXE_docproc-onenote"));
    command.arg(fixture(DESKTOP)).env("RUST_MIN_STACK", "65536");
    // SAFETY: setrlimit is async-signal-safe, and the closure touches nothing else between the
    // fork and the exec.
    unsafe {
        command.pre_exec(move || {
            let limit = libc::rlimit {
                rlim_cur: limit,
                rlim_max: limit,
            };
            if libc::setrlimit(libc::RLIMIT_AS, &limit) == 0 {
                Ok(())
            } else {
                Err(std::io::Error::last_os_error())
            }
        });
    }
    command.output().unwrap()
}

/// The read thread's stack is sized from the address-space limit, a quarter of it floored at
/// 8 MiB, so a modest limit reads: under 64 MiB, a 16 MiB stack and room for the read, where the
/// 256 MiB stack of no limit would not fit. Under 6 MiB, the stack's 8 MiB floor can't be
/// reserved, and the thread can't start, a fault outside any record, exit 4 with nothing on
/// stdout. 6 MiB leaves the static binary room to start and parse its arguments: measured, it
/// does from 3 MiB, and the thread starts from 11 MiB. RUST_MIN_STACK, set small for both runs,
/// doesn't change the stack.
#[test]
fn the_read_stack_fits_the_address_space_limit_or_is_exit_4() {
    let out = run_limited(64 << 20);
    let stderr = String::from_utf8(out.stderr).unwrap();
    assert_eq!(out.status.code(), Some(0), "{stderr}");
    assert_eq!(out.stdout, run(&[fixture(DESKTOP)]).stdout.into_bytes());
    let out = run_limited(6 << 20);
    assert_eq!(out.status.code(), Some(4));
    assert_eq!(String::from_utf8(out.stdout).unwrap(), "");
    assert_eq!(
        String::from_utf8(out.stderr).unwrap(),
        error("internal fault: the read thread can't start")
    );
}

/// A parser panic is caught by the shared guard and refused with exit 3, never 4. The byte set here makes
/// onenote_parser add past its integer's range; the tests run the release binary, whose overflow
/// checks (Cargo.toml's release profile) make that a panic.
#[test]
fn a_parser_panic_is_refused_with_exit_3() {
    let input = patched(DESKTOP, 9348, 0xFF, "panic.one");
    let out = run(&[&input]);
    assert_eq!(out.code, Some(3));
    assert_eq!(out.stdout, "");
    assert_eq!(
        out.stderr,
        error("the reader panicked: attempt to add with overflow")
    );
}

/// onenote_parser prints a dump of the file node to stdout as it fails on a node of the wrong
/// size; none of it reaches stdout, which a refusal leaves empty.
#[test]
fn the_parsers_own_printing_never_reaches_stdout() {
    let input = patched(DESKTOP, 1040, 0xFF, "node-size.one");
    let out = run(&[&input]);
    assert_eq!(out.code, Some(3));
    assert_eq!(out.stdout, "");
    assert_eq!(out.stderr, error(NODE_SIZE));
}

/// The parser's reason for [`DESKTOP`] with byte 1040 set to 0xFF, a file node of the wrong size.
const NODE_SIZE: &str = "Malformed OneNote file data: The size specified for this structure is \
                         incorrect. Was 7, expected 27. Id: 0xff";

/// A .onetoc2, by its content whatever its name, and a notebook directory are read as a
/// notebook, and a cabinet as a package: one the parser can't read is refused with its reason.
#[test]
fn notebooks_and_packages_are_recognised() {
    let toc = std::fs::read(fixture("onenote-rs/non-legacy/Open Notebook.onetoc2")).unwrap();
    let toc_as_section = built("Notebook.one", &toc);
    let notebook = fixture("onenote-rs/non-legacy");
    for input in [&toc_as_section, &notebook] {
        let out = run(&[input]);
        assert_eq!(out.code, Some(0), "{}: {}", input.display(), out.stderr);
        let first = &out.lines()[0];
        assert_eq!(first["input"], "notebook", "{}", input.display());
        assert_eq!(first["id"], "f1da443f-a65f-4513-b200-78d8a9910b8d");
    }
    let cabinet = built("Notebook.onepkg", b"MSCF\0\0\0\0");
    let out = run(&[&cabinet]);
    assert_eq!(out.code, Some(3));
    assert_eq!(out.stdout, "");
    assert_eq!(
        out.stderr,
        error("Malformed OneNote package: failed to read cabinet: failed to fill whole buffer")
    );
}

/// A stdout that can't be written exits 1 with one error line, for a read, --version and --help.
#[test]
fn an_unwritable_stdout_exits_1() {
    for args in [
        fixture(DESKTOP).into_os_string(),
        "--version".into(),
        "--help".into(),
    ] {
        let full = File::options().write(true).open("/dev/full").unwrap();
        let out = run_to(full, &[&args]);
        assert_eq!(out.code, Some(1), "{args:?}");
        assert_eq!(
            out.stderr,
            format!(
                "{NAME}: error: stdout can't be written: No space left on device (os error 28)\n"
            ),
            "{args:?}"
        );
    }
}
