//! The encryption signal: whether a section that failed to parse is a password-protected one.
//!
//! onenote_parser reads no encrypted section, and fails on one with whatever its parse met first
//! (`Encryption.one`'s "Unexpected end of file"). [MS-ONESTORE] 2.5.19's encryption key, which a
//! password-protected section holds, opens and closes with fixed 8-byte constants, so a section
//! the parser couldn't read and that holds both is encrypted. The file is scanned only after its
//! parse failed: a false match relabels a read that already failed and never costs content. The
//! scan streams the file, so a large section isn't held whole.

use std::io::{self, ErrorKind, Read};

/// The reason an encrypted section is refused with, as the input, or skipped with, inside a
/// notebook. It begins `encrypted`, which every docproc tool's reason does for an encrypted input.
pub const REASON: &str = "encrypted section (a password-protected section, holding an \
                          [MS-ONESTORE] 2.5.19 encryption key); remove its password in OneNote first";

const HEADER: [u8; 8] = 0xFB6B_A385_DAD1_A067_u64.to_le_bytes();
const FOOTER: [u8; 8] = 0x2649_294F_8E19_8B3C_u64.to_le_bytes();

const CHUNK: usize = 64 << 10;

/// Whether `source` holds both of 2.5.19's constants, anywhere, in either order. A constant that
/// straddles two reads is found: the last 7 bytes of each read are kept before the next.
pub fn holds_key(mut source: impl Read) -> io::Result<bool> {
    let keep = HEADER.len() - 1;
    let mut buffer = vec![0; keep + CHUNK];
    let mut kept = 0;
    let (mut header, mut footer) = (false, false);
    loop {
        let read = match source.read(&mut buffer[kept..]) {
            Ok(0) => return Ok(false),
            Ok(read) => read,
            Err(err) if err.kind() == ErrorKind::Interrupted => continue,
            Err(err) => return Err(err),
        };
        let filled = &buffer[..kept + read];
        header = header || filled.windows(HEADER.len()).any(|window| window == HEADER);
        footer = footer || filled.windows(FOOTER.len()).any(|window| window == FOOTER);
        if header && footer {
            return Ok(true);
        }
        let end = filled.len();
        let carry = end.min(keep);
        buffer.copy_within(end - carry..end, 0);
        kept = carry;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    struct Trickle<'a> {
        bytes: &'a [u8],
        step: usize,
    }

    impl Read for Trickle<'_> {
        fn read(&mut self, into: &mut [u8]) -> io::Result<usize> {
            let n = self.bytes.len().min(self.step).min(into.len());
            into[..n].copy_from_slice(&self.bytes[..n]);
            self.bytes = &self.bytes[n..];
            Ok(n)
        }
    }

    fn scan(bytes: &[u8], step: usize) -> bool {
        holds_key(Trickle { bytes, step }).unwrap()
    }

    #[test]
    fn a_password_protected_section_holds_the_key() {
        let fixture = |path: &str| {
            let dir = concat!(env!("CARGO_MANIFEST_DIR"), "/tests/fixtures/interop/SUT/");
            std::fs::File::open(format!("{dir}{path}")).unwrap()
        };
        assert!(holds_key(fixture("Encryption.one")).unwrap());
        for path in ["OneWithFileData.one", "OneWithoutFileData.one"] {
            assert!(!holds_key(fixture(path)).unwrap(), "{path}");
        }
    }

    #[test]
    fn both_constants_are_a_key_in_either_order_and_across_reads() {
        let mut bytes = vec![0xAA; 100];
        bytes.extend_from_slice(&FOOTER);
        bytes.extend_from_slice(&[0x55; 30]);
        bytes.extend_from_slice(&HEADER);
        bytes.extend_from_slice(&[0x11; 5]);
        for step in [1, 3, 7, 8, 9, 64, 1 << 20] {
            assert!(scan(&bytes, step), "step {step}");
        }
        let joined = [HEADER, FOOTER].concat();
        assert!(scan(&joined, 5));
    }

    #[test]
    fn one_constant_alone_or_a_partial_one_is_no_key() {
        assert!(!scan(&[&[0; 20][..], &HEADER].concat(), 4));
        assert!(!scan(&[&FOOTER[..], &[0; 20]].concat(), 4));
        assert!(!scan(&[&HEADER[..7], &FOOTER].concat(), 1));
        assert!(!scan(b"", 1));
    }
}
