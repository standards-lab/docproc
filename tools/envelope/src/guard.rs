//! The guard every reader call runs inside: a reader's error, or its panic, becomes the reason of
//! one record's failure, and the walk goes on.

use std::fmt::Display;
use std::panic::{AssertUnwindSafe, catch_unwind};

use crate::panic_text;

/// Why one read failed, and whether by a panic. After a panic, a reader's state (a file's mutex,
/// an allocator's lock) may be poisoned, so the caller reopens its input.
#[derive(Debug)]
pub struct Failure {
    pub reason: String,
    pub panicked: bool,
}

/// Runs one read, turning its error or its panic into a reason.
pub fn guarded<T, E: Display>(read: impl FnOnce() -> Result<T, E>) -> Result<T, Failure> {
    match catch_unwind(AssertUnwindSafe(read)) {
        Ok(Ok(value)) => Ok(value),
        Ok(Err(err)) => Err(Failure {
            reason: err.to_string(),
            panicked: false,
        }),
        Err(payload) => Err(Failure {
            reason: format!("the reader panicked: {}", panic_text(&*payload)),
            panicked: true,
        }),
    }
}

#[cfg(test)]
mod tests {
    use std::io;

    use super::*;

    #[test]
    fn a_reader_panic_becomes_a_reason() {
        let failure = guarded::<(), io::Error>(|| panic!("index out of bounds"))
            .err()
            .unwrap();
        assert_eq!(failure.reason, "the reader panicked: index out of bounds");
        assert!(failure.panicked);
    }

    #[test]
    fn a_reader_error_becomes_a_reason() {
        let failure =
            guarded::<(), _>(|| Err(io::Error::other("Missing Sub-Node Tree on message")))
                .err()
                .unwrap();
        assert_eq!(failure.reason, "Missing Sub-Node Tree on message");
        assert!(!failure.panicked);
    }

    #[test]
    fn a_read_that_succeeds_gives_its_value() {
        assert_eq!(guarded(|| Ok::<_, io::Error>(7)).unwrap(), 7);
    }
}
