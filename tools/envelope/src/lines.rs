//! The JSON Lines writer of stdout: one compact serialized line per call, newline-terminated, and
//! nothing else.

use std::io::{self, Write};

use serde::Serialize;

pub struct Lines<W: Write> {
    out: W,
}

impl<W: Write> Lines<W> {
    pub fn new(out: W) -> Self {
        Self { out }
    }

    /// Writes one line.
    pub fn write(&mut self, line: &impl Serialize) -> io::Result<()> {
        serde_json::to_writer(&mut self.out, line)?;
        self.out.write_all(b"\n")
    }

    pub fn flush(&mut self) -> io::Result<()> {
        self.out.flush()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn each_write_is_one_compact_line() {
        let mut lines = Lines::new(Vec::new());
        lines
            .write(&serde_json::json!({"line": "a", "text": "two\nlines"}))
            .unwrap();
        lines.write(&serde_json::json!({"line": "b"})).unwrap();
        assert_eq!(
            String::from_utf8(lines.out).unwrap(),
            "{\"line\":\"a\",\"text\":\"two\\nlines\"}\n{\"line\":\"b\"}\n"
        );
    }
}
