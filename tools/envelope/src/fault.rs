//! Why a run stops short of exit 0, and the one place its outcome becomes an error line and an
//! exit code: 0 read, skipped items included; 1 stdout or media not written; 2 usage; 3 refused
//! before any output; 4 a fault of the tool's own.

use std::any::Any;
use std::io::{self, Write};
use std::panic::{self, AssertUnwindSafe};
use std::process::ExitCode;

use crate::stderr;

/// Why a run stopped short of exit 0.
#[derive(Debug)]
pub enum Fault {
    /// stdout couldn't be written: exit 1.
    Output(io::Error),
    /// The media directory, or a medium in it, couldn't be written: exit 1.
    Media(String),
    /// The arguments: exit 2, with the usage after the error line.
    Usage(String),
    /// The input: exit 3, with nothing on stdout.
    Refused(String),
    /// A fault of the tool's own, a panic outside any guard among them: exit 4.
    Internal(String),
}

impl Fault {
    /// The exit code the fault ends a run with.
    pub fn code(&self) -> u8 {
        match self {
            Fault::Output(_) | Fault::Media(_) => 1,
            Fault::Usage(_) => 2,
            Fault::Refused(_) => 3,
            Fault::Internal(_) => 4,
        }
    }

    /// The reason the fault's error line gives.
    pub fn reason(&self) -> String {
        match self {
            Fault::Output(err) => format!("stdout can't be written: {err}"),
            Fault::Media(reason) | Fault::Usage(reason) | Fault::Refused(reason) => reason.clone(),
            Fault::Internal(reason) => format!("internal fault: {reason}"),
        }
    }
}

/// Runs a tool's whole run and ends it: exit 0 when it returns, else the fault's error line on
/// stderr, followed by `usage` for a usage fault, and the fault's exit code. A panic outside any
/// guard is an internal fault; the default panic hook is silenced, so a panic is reported once,
/// by the line its guard or this function writes, never by the hook.
pub fn run(tool: &str, usage: &str, run: impl FnOnce() -> Result<(), Fault>) -> ExitCode {
    panic::set_hook(Box::new(|_| {}));
    let outcome = panic::catch_unwind(AssertUnwindSafe(run))
        .unwrap_or_else(|payload| Err(Fault::Internal(panic_text(&*payload))));
    match outcome {
        Ok(()) => ExitCode::SUCCESS,
        Err(fault) => {
            stderr::error(tool, &fault.reason());
            if let Fault::Usage(_) = fault {
                stderr::write(&format!("{usage}\n"));
            }
            ExitCode::from(fault.code())
        }
    }
}

/// Writes `--version`'s output, `version` on its own line, to stdout. A stdout that can't take it
/// is an output fault, exit 1, never a panic.
pub fn version(version: &str) -> Result<(), Fault> {
    let mut out = io::stdout().lock();
    writeln!(out, "{version}")
        .and_then(|()| out.flush())
        .map_err(Fault::Output)
}

/// The message a panic was raised with.
pub fn panic_text(payload: &(dyn Any + Send)) -> String {
    payload
        .downcast_ref::<&str>()
        .map(|text| (*text).to_owned())
        .or_else(|| payload.downcast_ref::<String>().cloned())
        .unwrap_or_else(|| "a panic without a message".to_owned())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::cases;

    /// Each fault exits with the code whose meaning the table gives for it, and the table holds
    /// the codes 0 to 4 alone.
    #[test]
    fn each_fault_exits_with_its_tables_code() {
        let table: Vec<(u64, String)> = cases::group("exit_codes")
            .iter()
            .map(|case| {
                let code = case["code"].as_u64().expect("an exit code is a number");
                (code, cases::text(case, "meaning").to_owned())
            })
            .collect();
        let codes: Vec<u64> = table.iter().map(|(code, _)| *code).collect();
        assert_eq!(codes, [0, 1, 2, 3, 4]);
        let meaning = |fault: &Fault| {
            let code = u64::from(fault.code());
            table.iter().find(|(c, _)| *c == code).unwrap().1.clone()
        };
        // Each fault by the word of its code's meaning that names it.
        let faults = [
            (Fault::Output(io::Error::other("full")), "stdout"),
            (Fault::Media("x".into()), "media"),
            (Fault::Usage("x".into()), "usage"),
            (Fault::Refused("x".into()), "refused"),
            (Fault::Internal("x".into()), "fault"),
        ];
        for (fault, word) in faults {
            assert!(
                meaning(&fault).contains(word),
                "{fault:?}: {}",
                meaning(&fault)
            );
        }
    }

    #[test]
    fn a_reason_names_its_fault() {
        assert_eq!(
            Fault::Output(io::Error::other("No space left on device")).reason(),
            "stdout can't be written: No space left on device"
        );
        assert_eq!(
            Fault::Internal("index out of bounds".into()).reason(),
            "internal fault: index out of bounds"
        );
        assert_eq!(Fault::Refused("not a file".into()).reason(), "not a file");
    }

    #[test]
    fn a_panics_text_is_its_message() {
        let text = |payload: Box<dyn Any + Send>| panic_text(&*payload);
        assert_eq!(text(Box::new("static")), "static");
        assert_eq!(text(Box::new(String::from("formatted"))), "formatted");
        assert_eq!(text(Box::new(7)), "a panic without a message");
    }
}
