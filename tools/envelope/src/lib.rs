//! docproc-envelope: the common envelope of docproc's Rust system tools (context/layers.md,
//! "Common envelope"), written once for every one of them.
//!
//! - [`Fault`] and [`run`]: why a run stopped short of exit 0, its exit code from 1 to 4, and the
//!   one place a run's outcome becomes its error line and exit code.
//! - [`stderr`]: the `<tool>: error: <reason>` and `<tool>: warning: <where>: <what>` lines, with
//!   the envelope's escape.
//! - [`Lines`]: the JSON Lines writer of stdout.
//! - [`guarded`]: a reader's error, or its panic, as a reason.
//! - [`Media`]: the media store, streaming each medium to its hash name.
//! - [`markdown`]: the literal-text escape and the destination writer.
//!
//! Each tool keeps its own flags, readers, and model. The envelope's cases are
//! tools/envelope/cases.json's, which this crate's unit tests read in place, as docproc-ooxml's
//! tests and docproc's internal/cli do.

mod fault;
mod guard;
mod lines;
pub mod markdown;
mod media;
pub mod stderr;

#[cfg(test)]
mod cases;

pub use fault::{Fault, panic_text, run, version};
pub use guard::{Failure, guarded};
pub use lines::Lines;
pub use media::{Media, extension_of};
