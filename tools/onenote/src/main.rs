//! docproc-onenote: OneNote sections, packages, and notebooks to JSON Lines and Markdown.
//!
//! It reads one input, sniffed by content, and writes JSON Lines of schema "2" to stdout:
//! nothing else goes there. Warnings and errors are single lines on stderr. Exit codes:
//! 0 read, even with records skipped; 1 output that can't be written, to stdout or to the
//! media directory; 2 a usage error; 3 an input refused before any output; 4 a fault of the
//! CLI's own. The envelope, those lines and codes, the lines writer, the guard, and the media
//! directory's store, is docproc-envelope's.
//!
//! The read runs on a thread of its own, with a fixed stack of [`READ_STACK`]: the reader, the
//! renderer, the math port and the parser all recurse to the file's nesting depth, and a stack
//! overflow is a signal no handler catches.

mod content;
mod encryption;
mod ink;
mod markdown;
mod math;
mod model;
mod notebook;
mod page;
mod section;
mod sniff;
mod stdout;

use std::ffi::OsString;
use std::io::{self, BufWriter, Write};
use std::path::PathBuf;
use std::process::ExitCode;
use std::thread;

use docproc_envelope::{Fault, Lines, Media, panic_text, stderr};
use sniff::Input;

/// The binary's name, which Cargo.toml's package gives, leading every stderr line.
const NAME: &str = env!("CARGO_BIN_NAME");

/// The usage: on stdout for `--help`, and on stderr after a usage error's line.
const USAGE: &str = concat!(
    "usage: ",
    env!("CARGO_BIN_NAME"),
    " [--media <dir>] [--file-only] <input>\n       ",
    env!("CARGO_BIN_NAME"),
    " --version\n       ",
    env!("CARGO_BIN_NAME"),
    " --help"
);

/// The read thread's stack, fixed, so no flag or environment changes it: 256 MiB holds outline,
/// group, table and equation nesting far past any real page's. It is an address-space
/// reservation, its pages touched only as deep as a read goes, so it costs no resident memory
/// beyond what the read uses; a process limit on its address space must leave room for it.
const READ_STACK: usize = 256 << 20;

/// The reason a refused table of contents gives under `--file-only`, fixed text that docproc
/// maps to the part being read elsewhere: through its sibling sections, each its own input.
pub const TOC_REFUSED: &str = "a table of contents; its sections are read on their own";

/// The parsed command line.
enum Command {
    Version,
    Help,
    Read(Args),
}

struct Args {
    input: PathBuf,
    media: Option<PathBuf>,
    /// `--file-only`: a table of contents, given as a file or as its notebook's directory, is
    /// refused before any section is read, so only a section or a package is read.
    file_only: bool,
}

fn main() -> ExitCode {
    // args_os: a path needn't be UTF-8, and std::env::args panics on one that isn't.
    let args: Vec<OsString> = std::env::args_os().skip(1).collect();
    docproc_envelope::run(NAME, USAGE, || run(&args))
}

fn run(args: &[OsString]) -> Result<(), Fault> {
    let args = match parse(args)? {
        // The bare version, which the release compares with its tag's, then the schema.
        Command::Version => {
            return docproc_envelope::version(env!("CARGO_PKG_VERSION"), model::SCHEMA);
        }
        // The usage, asked for, so on stdout and exit 0; a stdout that can't take it is an
        // output fault, exit 1, as for --version.
        Command::Help => {
            let mut out = io::stdout().lock();
            return writeln!(out, "{USAGE}")
                .and_then(|()| out.flush())
                .map_err(Fault::Output);
        }
        Command::Read(args) => args,
    };
    on_read_thread(|| read(args))
}

/// Runs `read` on a thread whose stack is [`READ_STACK`], and gives its outcome. A thread that
/// can't start, its stack's reservation refused, is a fault outside any record, exit 4; a panic
/// on it is one too, as it would be on the main thread, the envelope's hook keeping it silent.
fn on_read_thread(read: impl FnOnce() -> Result<(), Fault> + Send) -> Result<(), Fault> {
    thread::scope(|scope| {
        let reader = thread::Builder::new()
            .name("read".to_owned())
            .stack_size(READ_STACK)
            .spawn_scoped(scope, read)
            .map_err(|_| Fault::Internal("the read thread can't start".to_owned()))?;
        reader
            .join()
            .unwrap_or_else(|payload| Err(Fault::Internal(panic_text(&*payload))))
    })
}

/// Reads the input the arguments name, on the read thread.
fn read(args: Args) -> Result<(), Fault> {
    let path = &args.input;
    let input = sniff::sniff(path)?;
    // A notebook directory is read through its table of contents, so --file-only refuses it as
    // it refuses the table of contents itself.
    if args.file_only && matches!(input, Input::Directory(_)) {
        return Err(Fault::Refused(TOC_REFUSED.to_owned()));
    }
    // From here, descriptor 1 is /dev/null and the lines go to the descriptor stdout was.
    let mut out = Lines::new(BufWriter::new(stdout::take()?));
    let mut media = Media::new(args.media);
    match input {
        Input::OneStore => section::read(path, args.file_only, &mut media, &mut out)?,
        Input::Directory(toc) => notebook::read(&toc, &mut media, &mut out)?,
        Input::Package => notebook::read_package(path, &mut media, &mut out)?,
    }
    out.flush().map_err(Fault::Output)
}

/// Writes one warning line, "docproc-onenote: warning: <where>: <what>", escaped: `place` names
/// the record, from its section down, and `what` says what happened to it.
pub fn warn(place: &str, what: &str) {
    stderr::warning(NAME, place, what);
}

/// Parses the arguments by hand: `--version` or `--help` alone, or one input and an optional
/// `--media <dir>` and `--file-only`, in any order.
fn parse(args: &[OsString]) -> Result<Command, Fault> {
    if let [flag] = args {
        if flag == "--version" {
            return Ok(Command::Version);
        }
        if flag == "--help" {
            return Ok(Command::Help);
        }
    }
    let usage = |reason: &str| Err(Fault::Usage(reason.to_owned()));
    let mut input = None;
    let mut media = None;
    let mut file_only = false;
    let mut rest = args.iter();
    while let Some(arg) = rest.next() {
        match arg.to_str() {
            Some("--media") => {
                let Some(dir) = rest.next().filter(|dir| !dir.is_empty()) else {
                    return usage("--media needs a directory");
                };
                if media.replace(PathBuf::from(dir)).is_some() {
                    return usage("--media is given more than once");
                }
            }
            Some("--file-only") => {
                if file_only {
                    return usage("--file-only is given more than once");
                }
                file_only = true;
            }
            Some(flag @ ("--version" | "--help")) => {
                return usage(&format!("{flag} takes no other arguments"));
            }
            Some(option) if option.starts_with('-') && option != "-" => {
                return usage(&format!("unknown option {option}"));
            }
            _ => {
                if input.replace(PathBuf::from(arg)).is_some() {
                    return usage("more than one input");
                }
            }
        }
    }
    let Some(input) = input else {
        return usage("no input");
    };
    if let Some(dir) = &media
        && dir.exists()
        && !dir.is_dir()
    {
        return usage(&format!("--media {} is not a directory", dir.display()));
    }
    Ok(Command::Read(Args {
        input,
        media,
        file_only,
    }))
}
