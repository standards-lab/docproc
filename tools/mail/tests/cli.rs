//! The command line: arguments, usage errors, and the inputs refused with exit 3.

mod common;

use common::{built, fixture, run, scratch};

const USAGE: &str = "usage: docproc-mail [--media <dir>] [--after <id>] <input>
       docproc-mail --version
       docproc-mail --help
";

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

#[test]
fn help_prints_the_usage_to_stdout() {
    let out = run(&["--help"]);
    assert_eq!(out.code, Some(0));
    assert_eq!(out.stdout, USAGE);
    assert_eq!(out.stderr, "");
}

#[test]
fn version_and_help_to_a_full_stdout_exit_1() {
    for flag in ["--version", "--help"] {
        let full = std::fs::File::options()
            .write(true)
            .open("/dev/full")
            .unwrap();
        let out = std::process::Command::new(env!("CARGO_BIN_EXE_docproc-mail"))
            .arg(flag)
            .stdout(full)
            .output()
            .unwrap();
        assert_eq!(out.status.code(), Some(1), "{flag}");
        assert_eq!(
            String::from_utf8(out.stderr).unwrap(),
            "docproc-mail: error: stdout can't be written: No space left on device (os error 28)\n",
            "{flag}"
        );
    }
}

#[test]
fn usage_errors_exit_2_with_usage() {
    let store = fixture("pst/libyal/outlook.pst");
    let store = store.to_str().unwrap();
    let not_a_dir = fixture("pst/sources.json");
    let not_a_dir = not_a_dir.to_str().unwrap();
    let cases: &[(&[&str], &str)] = &[
        (&[], "no input"),
        (&[store, store], "more than one input"),
        (&["--frobnicate", store], "unknown option --frobnicate"),
        (&["--version", store], "--version takes no other arguments"),
        (&[store, "--help"], "--help takes no other arguments"),
        (&["--help", "--version"], "--help takes no other arguments"),
        (&[store, "--media"], "--media needs a directory"),
        (&["--media", "", store], "--media needs a directory"),
        (
            &["--media", "a", "--media", "b", store],
            "--media is given more than once",
        ),
        (
            &["--media", not_a_dir, store],
            &format!("--media {not_a_dir} is not a directory"),
        ),
        (&[store, "--after"], "--after needs a message id"),
        (&["--after", "", store], "--after needs a message id"),
        (
            &["--after", "1", "--after", "2", store],
            "--after is given more than once",
        ),
    ];
    for (args, reason) in cases {
        let out = run(args);
        assert_eq!(out.code, Some(2), "{args:?}");
        assert_eq!(out.stdout, "", "{args:?}");
        assert_eq!(
            out.stderr,
            format!("docproc-mail: error: {reason}\n{USAGE}"),
            "{args:?}"
        );
    }
}

#[test]
fn a_malformed_after_id_is_a_usage_error() {
    let store = fixture("pst/libyal/outlook.pst");
    let store = store.to_str().unwrap();
    for id in [
        "0",
        "00",
        "-1",
        "+1",
        "1.5",
        "0x10",
        "abc",
        " 1",
        "4294967296",
    ] {
        let out = run(&["--after", id, store]);
        assert_eq!(out.code, Some(2), "{id:?}");
        assert_eq!(out.stdout, "", "{id:?}");
        assert_eq!(
            out.stderr,
            format!(
                "docproc-mail: error: --after {id} is not a message id, a whole number from 1 to 4,294,967,295\n{USAGE}"
            ),
            "{id:?}"
        );
    }
}

#[test]
fn after_with_a_single_message_is_a_usage_error() {
    for (input, kind) in [
        ("generated/msg/01-plain.msg", "a .msg"),
        ("generated/eml/01-plain.eml", "an .eml"),
    ] {
        let path = fixture(input);
        let path = path.to_str().unwrap();
        let out = run(&[path, "--after", "1"]);
        assert_eq!(out.code, Some(2), "{input}");
        assert_eq!(out.stdout, "", "{input}");
        assert_eq!(
            out.stderr,
            format!(
                "docproc-mail: error: --after resumes a .pst or an mbox; \
                 the input is {kind}, a single message\n{USAGE}"
            ),
            "{input}"
        );
    }
    let missing = std::path::Path::new(env!("CARGO_TARGET_TMPDIR")).join("missing.eml");
    let out = run(&[missing.as_os_str(), "--after".as_ref(), "1".as_ref()]);
    assert_eq!(out.code, Some(3), "{}", out.stderr);
}

#[test]
fn media_is_accepted_with_the_input() {
    let store = fixture("pst/libyal/outlook.pst");
    let dir = std::path::Path::new(env!("CARGO_TARGET_TMPDIR")).join("media-unmade");
    let without = run(&[&store]);
    for args in [
        [store.as_os_str(), "--media".as_ref(), dir.as_os_str()],
        ["--media".as_ref(), dir.as_os_str(), store.as_os_str()],
    ] {
        if dir.exists() {
            std::fs::remove_dir_all(&dir).unwrap();
        }
        let out = run(&args);
        assert_eq!(out.code, Some(0), "{}", out.stderr);
        assert_eq!(out.stdout, without.stdout);
        assert!(dir.is_dir());
    }
}

#[test]
fn the_media_directory_is_created_once_the_input_is_read() {
    let tmp = std::path::Path::new(env!("CARGO_TARGET_TMPDIR"));
    let made = tmp.join("media-plain");
    let refused = tmp.join("media-refused");
    for dir in [&made, &refused] {
        let _ = std::fs::remove_dir_all(dir);
    }
    let plain = fixture("generated/eml/01-plain.eml");
    let out = run(&[plain.as_os_str(), "--media".as_ref(), made.as_os_str()]);
    assert_eq!(out.code, Some(0), "{}", out.stderr);
    assert_eq!(std::fs::read_dir(&made).unwrap().count(), 0);
    let foreign = built("media-refused.txt", b"neither mail nor a store\n");
    let out = run(&[foreign.as_os_str(), "--media".as_ref(), refused.as_os_str()]);
    assert_eq!(out.code, Some(3), "{}", out.stderr);
    assert!(!refused.exists());
}

fn pst_header(client: &[u8; 2], version: u16) -> Vec<u8> {
    let mut bytes = b"!BDN\0\0\0\0".to_vec();
    bytes.extend_from_slice(client);
    bytes.extend_from_slice(&version.to_le_bytes());
    bytes.resize(564, 0);
    bytes
}

fn assert_refused(args: &[&std::path::Path], reason: &str) {
    let out = run(args);
    assert_eq!(out.code, Some(3), "{}", out.stderr);
    assert_eq!(out.stdout, "");
    assert_eq!(out.stderr, format!("docproc-mail: error: {reason}\n"));
}

#[test]
fn an_ost_is_refused_with_how_to_export_it() {
    let ost = built("refused.ost", &pst_header(b"SO", 36));
    assert_refused(
        &[&ost],
        "an .ost, the offline cache of a server mailbox, is not read; \
         export the mailbox from Outlook as a .pst",
    );
}

#[test]
fn the_format_is_sniffed_not_named() {
    let ost = built("named.pst", &pst_header(b"SO", 23));
    let out = run(&[&ost]);
    assert_eq!(out.code, Some(3));
    assert!(out.stderr.contains("an .ost"), "{}", out.stderr);
}

#[test]
fn a_pst_version_outlook_pst_does_not_read_is_refused() {
    let pst = built("4k-pages.pst", &pst_header(b"SM", 36));
    assert_refused(
        &[&pst],
        "PST version 36 is not read; only versions 14 and 15 (ANSI) and 23 (Unicode) are",
    );
}

#[test]
fn an_unreadable_pst_header_is_refused() {
    let pst = built("bad-header.pst", &pst_header(b"SM", 23));
    let out = run(&[&pst]);
    assert_eq!(out.code, Some(3));
    assert_eq!(out.stdout, "");
    assert!(
        out.stderr
            .starts_with("docproc-mail: error: the PST header can't be read: "),
        "{}",
        out.stderr
    );
    assert_eq!(out.stderr.lines().count(), 1);
}

#[test]
fn an_unreadable_store_root_is_refused() {
    let whole = std::fs::read(fixture("pst/libyal/outlook.pst")).unwrap();
    let pst = built("truncated.pst", &whole[..4096]);
    let out = run(&[&pst]);
    assert_eq!(out.code, Some(3));
    assert_eq!(out.stdout, "");
    assert!(
        out.stderr
            .starts_with("docproc-mail: error: the store's root can't be read: "),
        "{}",
        out.stderr
    );
    assert_eq!(out.stderr.lines().count(), 1);
}

#[test]
fn an_unreadable_ipm_subtree_is_refused() {
    let mut bytes = std::fs::read(fixture("pst/libyal/outlook.pst")).unwrap();
    bytes[30784] ^= 0xFF;
    let pst = built("bad-ipm-subtree.pst", &bytes);
    assert_refused(
        &[&pst],
        "the IPM subtree can't be read: Invalid BLOCKTRAILER dwCRC: 0x1D954937",
    );
}

#[test]
fn an_unreadable_compound_file_is_refused() {
    let mut bytes = vec![0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1];
    bytes.resize(512, 0);
    let msg = built("compound.msg", &bytes);
    let out = run(&[&msg]);
    assert_eq!(out.code, Some(3));
    assert_eq!(out.stdout, "");
    assert!(
        out.stderr
            .starts_with("docproc-mail: error: the compound file can't be read: "),
        "{}",
        out.stderr
    );
    assert_eq!(out.stderr.lines().count(), 1);
}

#[test]
fn a_compound_file_that_is_not_a_message_is_refused() {
    let path = std::path::Path::new(env!("CARGO_TARGET_TMPDIR")).join("not-a-message.msg");
    let mut file = cfb::create(&path).unwrap();
    std::io::Write::write_all(&mut file.create_stream("/WordDocument").unwrap(), b"text").unwrap();
    file.flush().unwrap();
    assert_refused(
        &[&path],
        "a compound file, but not an Outlook message: \
         it has no __properties_version1.0 stream",
    );
}

#[test]
fn none_of_the_four_formats_is_refused() {
    for (name, bytes) in [
        ("empty.eml", &b""[..]),
        ("text.pst", b"hello, this is no mail\n"),
        ("short.pst", b"!BDN\0\0\0\0SM"),
        ("client.pst", &pst_header(b"XX", 23)),
        (
            "no-from-or-date.eml",
            b"Subject: hi\r\nTo: bob@example.com\r\n\r\nbody\r\n",
        ),
        (
            "blank-first.eml",
            b"\r\nFrom: ann@example.com\r\n\r\nbody\r\n",
        ),
        (
            "not-a-field.eml",
            b"Subject: hi\r\nhello\r\nFrom: ann@example.com\r\n\r\n",
        ),
        (
            "from-in-body.eml",
            b"Subject: hi\r\n\r\nFrom: ann@example.com\r\n",
        ),
    ] {
        let input = built(name, bytes);
        assert_refused(&[&input], "neither a .pst, a .msg, an .eml nor an mbox");
    }
}

#[test]
fn an_input_that_is_missing_a_directory_or_unreadable_is_refused_by_its_path() {
    let missing = std::path::Path::new(env!("CARGO_TARGET_TMPDIR")).join("missing.pst");
    assert_refused(&[&missing], &format!("no such file: {}", missing.display()));
    let dir = scratch("folder.pst");
    assert_refused(&[&dir], &format!("is a directory: {}", dir.display()));
    let locked = Locked::new("locked.eml");
    if std::fs::File::open(&locked.0).is_ok() {
        eprintln!("skipped: a file without permission can be opened, as root");
        return;
    }
    assert_refused(
        &[&locked.0],
        &format!("permission denied: {}", locked.0.display()),
    );
}

struct Locked(std::path::PathBuf);

impl Locked {
    fn new(name: &str) -> Self {
        let _ = std::fs::remove_file(std::path::Path::new(env!("CARGO_TARGET_TMPDIR")).join(name));
        let locked = Self(built(name, b"Subject: locked\r\n\r\nbody\r\n"));
        locked.set(0o000);
        locked
    }

    fn set(&self, mode: u32) {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&self.0, std::fs::Permissions::from_mode(mode)).unwrap();
    }
}

impl Drop for Locked {
    fn drop(&mut self) {
        self.set(0o600);
    }
}

#[test]
fn a_path_that_is_not_utf8_is_read_as_a_path() {
    use std::os::unix::ffi::OsStrExt;
    let name = std::ffi::OsStr::from_bytes(b"missing-\xFF.pst");
    let missing = std::path::Path::new(env!("CARGO_TARGET_TMPDIR")).join(name);
    assert_refused(&[&missing], &format!("no such file: {}", missing.display()));
}
