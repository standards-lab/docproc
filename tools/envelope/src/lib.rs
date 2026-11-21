//! docproc-envelope: the common envelope of docproc's Rust system tools (docs/tool-contract.md,
//! "Common envelope"), written once for every one of them.
//!
//! - [`Fault`] and [`run`]: why a run stopped short of exit 0, its exit code from 1 to 4, and the
//!   one place a run's outcome becomes its error line and exit code; [`input_refusal`], the
//!   refusal of an input that is missing, a directory, or unreadable, naming its path.
//! - [`stderr`]: the `<tool>: error: <reason>` and `<tool>: warning: <where>: <what>` lines, with
//!   the envelope's escape.
//! - [`Lines`]: the JSON Lines writer of stdout.
//! - [`guarded`]: a reader's error, or its panic, as a reason.
//! - [`Media`]: the media store, streaming each medium to its hash name.
//! - [`markdown`]: the literal-text escape, the destination and title writers, and the test for a
//!   `&` that could begin a character reference.
//! - [`content`]: the content rule, the sentences of a source text that a target text lacks.
//! - [`category`]: Unicode general-category predicates, on the content rule's Unicode version.
//!
//! Each tool keeps its own flags, readers, and model. The envelope's cases are
//! tools/envelope/cases.json's, which this crate's unit tests read in place, as docproc-ooxml's
//! tests and docproc's internal/cli do.

pub mod category;
pub mod content;
mod fault;
mod guard;
mod lines;
pub mod markdown;
mod media;
pub mod stderr;

#[cfg(test)]
mod cases;

pub use fault::{Fault, input_refusal, panic_text, run, version};
pub use guard::{Failure, guarded};
pub use lines::Lines;
pub use media::{Media, extension_of};
