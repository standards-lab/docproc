//! docproc-mail: Outlook .pst stores, .msg messages, .eml messages and mbox files to a
//! JSON model.
//!
//! It reads one input, sniffed by content, and writes JSON Lines of schema "1" to stdout:
//! nothing else goes there. Warnings and errors are single lines on stderr. Exit codes:
//! 0 read, even with records skipped; 1 output that can't be written, to stdout or to the
//! attachments directory; 2 a usage error;
//! 3 an input refused; 4 a fault of the CLI's own. The envelope, those lines and codes, the
//! guard, and the attachments directory's store, is docproc-envelope's.

mod codepage;
mod eml;
mod mapping;
mod markdown;
mod mbox;
mod mime;
mod model;
mod msg;
mod props;
mod pst;
mod rtf;
mod sniff;

use std::ffi::OsString;
use std::fs::File;
use std::io::{self, BufReader, BufWriter, Read};
use std::path::PathBuf;
use std::process::ExitCode;

use docproc_envelope::{Fault, Lines, Media, stderr};
use sniff::Format;

/// The binary's name, which Cargo.toml's package gives, leading every stderr line.
const NAME: &str = env!("CARGO_BIN_NAME");

const USAGE: &str = concat!(
    "usage: ",
    env!("CARGO_BIN_NAME"),
    " [--attachments <dir>] [--after <id>] <input>\n       ",
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
    attachments: Option<PathBuf>,
    /// `--after <id>`: the message a .pst's or an mbox's read resumes after.
    after: Option<u32>,
}

fn main() -> ExitCode {
    // args_os: a path needn't be UTF-8, and std::env::args panics on one that isn't.
    let args: Vec<OsString> = std::env::args_os().skip(1).collect();
    docproc_envelope::run(NAME, USAGE, || run(&args))
}

fn run(args: &[OsString]) -> Result<(), Fault> {
    let args = match parse(args)? {
        // The bare version, which the release compares with its tag's.
        Command::Version => return docproc_envelope::version(env!("CARGO_PKG_VERSION")),
        Command::Read(args) => args,
    };
    let path = &args.input;
    let input = path.display();
    let mut prefix = Vec::with_capacity(sniff::PREFIX);
    File::open(path)
        .and_then(|file| file.take(sniff::PREFIX as u64).read_to_end(&mut prefix))
        .map_err(|err| Fault::Refused(format!("{input}: {err}")))?;
    let mut out = Lines::new(BufWriter::new(io::stdout().lock()));
    let mut files = Media::new(args.attachments);
    match sniff::sniff(&prefix) {
        Format::Pst(encoding) => pst::read(path, encoding, args.after, &mut out, &mut files)?,
        Format::Ost => {
            return Err(Fault::Refused(format!(
                "{input}: an .ost, the offline cache of a server mailbox, is not read; \
                 export the mailbox from Outlook as a .pst"
            )));
        }
        Format::PstVersion(version) => {
            return Err(Fault::Refused(format!(
                "{input}: PST version {version} is not read; \
                 only versions 14 and 15 (ANSI) and 23 (Unicode) are"
            )));
        }
        Format::CompoundFile => {
            single(args.after, &input, "a .msg")?;
            msg::read(path, &mut out, &mut files)?;
        }
        Format::Mbox => mbox::read(path, args.after, &mut out, &mut files)?,
        Format::Other => {
            let shaped = File::open(path)
                .and_then(|file| sniff::header_shape(BufReader::new(file)))
                .map_err(|err| Fault::Refused(format!("{input}: {err}")))?;
            if !shaped {
                return Err(Fault::Refused(format!(
                    "{input}: neither a .pst, a .msg, an .eml nor an mbox"
                )));
            }
            single(args.after, &input, "an .eml")?;
            eml::read(path, &mut out, &mut files)?;
        }
    }
    out.flush().map_err(Fault::Output)
}

/// Refuses `--after` for an input that is a single message, `kind`, as a usage error: there is no
/// message to resume after. The input has been sniffed first, so a missing or foreign input is
/// still refused as such.
fn single(after: Option<u32>, input: &impl std::fmt::Display, kind: &str) -> Result<(), Fault> {
    match after {
        Some(_) => Err(Fault::Usage(format!(
            "--after resumes a .pst or an mbox; {input} is {kind}, a single message"
        ))),
        None => Ok(()),
    }
}

/// Parses the arguments by hand: `--version` alone, or one input and an optional
/// `--attachments <dir>` and `--after <id>`, in any order.
fn parse(args: &[OsString]) -> Result<Command, Fault> {
    if let [flag] = args
        && flag == "--version"
    {
        return Ok(Command::Version);
    }
    let usage = |reason: &str| Err(Fault::Usage(reason.to_owned()));
    let mut input = None;
    let mut attachments = None;
    let mut after = None;
    let mut rest = args.iter();
    while let Some(arg) = rest.next() {
        match arg.to_str() {
            Some("--attachments") => {
                let Some(dir) = rest.next().filter(|dir| !dir.is_empty()) else {
                    return usage("--attachments needs a directory");
                };
                if attachments.replace(PathBuf::from(dir)).is_some() {
                    return usage("--attachments is given more than once");
                }
            }
            Some("--after") => {
                let Some(id) = rest.next().filter(|id| !id.is_empty()) else {
                    return usage("--after needs a message id");
                };
                let Some(id) = message_id(id) else {
                    return usage(&format!(
                        "--after {} is not a message id, a whole number from 1 to 4,294,967,295",
                        id.to_string_lossy()
                    ));
                };
                if after.replace(id).is_some() {
                    return usage("--after is given more than once");
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
    if let Some(dir) = &attachments
        && dir.exists()
        && !dir.is_dir()
    {
        return usage(&format!(
            "--attachments {} is not a directory",
            dir.display()
        ));
    }
    Ok(Command::Read(Args {
        input,
        attachments,
        after,
    }))
}

/// `--after`'s id: decimal digits alone, no sign, naming a u32 from 1. A .pst's node ids and an
/// mbox's ordinals both start at 1, so 0 names no message and is a usage error, like an id that
/// isn't a number, rather than "resume from the start".
fn message_id(arg: &OsString) -> Option<u32> {
    let digits = arg.to_str()?;
    if !digits.bytes().all(|byte| byte.is_ascii_digit()) {
        return None;
    }
    digits.parse().ok().filter(|id| *id != 0)
}

/// Writes one warning line: "docproc-mail: warning: <where>: <what>", escaped.
pub fn warn(place: &str, what: &str) {
    stderr::warning(NAME, place, what);
}

/// Writes one warning line for each of a record's notes.
pub fn warn_all(place: &str, notes: Vec<String>) {
    for note in notes {
        warn(place, &note);
    }
}

/// Writes one warning line for each note of a record and of the records below it, each note's
/// place following `place`.
pub fn warn_below(place: &str, notes: Vec<(String, String)>) {
    for (below, note) in notes {
        warn(&format!("{place}{below}"), &note);
    }
}
