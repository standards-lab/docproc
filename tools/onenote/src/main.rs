//! docproc-onenote: OneNote sections, packages, and notebooks to a JSON model.
//!
//! It reads one input, sniffed by content, and writes JSON Lines of schema "1" to stdout:
//! nothing else goes there. Warnings and errors are single lines on stderr. Exit codes:
//! 0 read, even with records skipped; 1 output that can't be written, to stdout or to the
//! media directory; 2 a usage error; 3 an input refused before any output; 4 a fault of the
//! CLI's own. The envelope, those lines and codes, the lines writer, the guard, and the media
//! directory's store, is docproc-envelope's.

mod content;
mod ink;
mod markdown;
mod math;
mod model;
mod notebook;
mod section;
mod sniff;
mod stdout;

use std::ffi::OsString;
use std::io::BufWriter;
use std::path::PathBuf;
use std::process::ExitCode;

use docproc_envelope::{Fault, Lines, Media, stderr};
use sniff::Input;

/// The binary's name, which Cargo.toml's package gives, leading every stderr line.
const NAME: &str = env!("CARGO_BIN_NAME");

const USAGE: &str = concat!(
    "usage: ",
    env!("CARGO_BIN_NAME"),
    " [--media <dir>] <input>\n       ",
    env!("CARGO_BIN_NAME"),
    " --version"
);

/// The parsed command line.
enum Command {
    Version,
    Read(Args),
}

struct Args {
    input: PathBuf,
    media: Option<PathBuf>,
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
        Command::Read(args) => args,
    };
    let path = &args.input;
    let input = sniff::sniff(path)?;
    // From here, descriptor 1 is /dev/null and the lines go to the descriptor stdout was.
    let mut out = Lines::new(BufWriter::new(stdout::take()?));
    let mut media = Media::new(args.media);
    match input {
        Input::OneStore => section::read(path, &mut media, &mut out)?,
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

/// Parses the arguments by hand: `--version` alone, or one input and an optional
/// `--media <dir>`, in any order.
fn parse(args: &[OsString]) -> Result<Command, Fault> {
    if let [flag] = args
        && flag == "--version"
    {
        return Ok(Command::Version);
    }
    let usage = |reason: &str| Err(Fault::Usage(reason.to_owned()));
    let mut input = None;
    let mut media = None;
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
            Some("--version") => return usage("--version takes no other arguments"),
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
    Ok(Command::Read(Args { input, media }))
}
