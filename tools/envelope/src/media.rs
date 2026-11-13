//! The media store of `--media <dir>`. Each distinct payload is one file there, named by the
//! lowercase hex SHA-256 of its bytes and, when the stored extension is 1 to 8 ASCII letters and
//! digits, `.` and that extension, its case kept; a stored name is never otherwise part of a path.
//! A payload is streamed: hashed while it is copied through a fixed buffer into a temporary file
//! in the directory, which is then renamed to its name, so a name in the directory always holds
//! the whole payload. Without `--media` a payload is hashed only, the names stand, and nothing is
//! written to disk.
//!
//! The directory is created, if it is missing, once the input is read, with or without a payload
//! to write: the tool calls [`Media::create`] then. A name is written once a run.

use std::collections::HashSet;
use std::fmt::Write as _;
use std::fs::{self, File};
use std::io::{self, Read, Write};
use std::path::{Path, PathBuf};

use sha2::{Digest, Sha256};

use crate::Fault;

/// The buffer a payload is copied through.
const BUFFER: usize = 64 * 1024;

/// The longest stored extension a name keeps.
const EXTENSION_MAX: usize = 8;

pub struct Media {
    /// The `--media` directory; none without `--media`.
    dir: Option<PathBuf>,
    /// The names this run has written. A name is its content's hash, so a name written once is
    /// never written again.
    written: HashSet<String>,
    /// How many temporary files this run has made, which tells their names apart.
    temporaries: u64,
}

impl Media {
    /// The directory to write into, or None to write nothing.
    pub fn new(dir: Option<PathBuf>) -> Self {
        Self {
            dir,
            written: HashSet::new(),
            temporaries: 0,
        }
    }

    /// Creates the directory if it is missing; the tool calls it once the input is read, so a run
    /// that exits 0 always leaves the directory. Without `--media` it does nothing.
    pub fn create(&self) -> Result<(), Fault> {
        let Some(dir) = &self.dir else {
            return Ok(());
        };
        fs::create_dir_all(dir).map_err(|err| {
            Fault::Media(format!(
                "the media directory {} can't be created: {err}",
                dir.display()
            ))
        })
    }

    /// Stores one payload, read from `content`, with the extension it was stored under, and
    /// returns its name. With `--media` it is written unless this run already wrote that name. A
    /// payload that can't be read or written is a media fault, exit 1.
    pub fn store(&mut self, content: impl Read, extension: Option<&str>) -> Result<String, Fault> {
        let Some(dir) = self.dir.clone() else {
            let hash = copy(content, &mut io::sink())
                .map_err(|err| Fault::Media(format!("a medium can't be read: {err}")))?;
            return Ok(name(hash, extension));
        };
        self.temporaries += 1;
        let temporary = dir.join(format!(".{}-{}.tmp", std::process::id(), self.temporaries));
        let written = File::create_new(&temporary).and_then(|mut file| {
            let hash = copy(content, &mut file)?;
            file.flush()?;
            Ok(hash)
        });
        let hash = written.map_err(|err| {
            remove(&temporary);
            Fault::Media(format!(
                "the media file {} can't be written: {err}",
                temporary.display()
            ))
        })?;
        let name = name(hash, extension);
        if self.written.contains(&name) {
            remove(&temporary);
            return Ok(name);
        }
        let path = dir.join(&name);
        fs::rename(&temporary, &path).map_err(|err| {
            remove(&temporary);
            Fault::Media(format!(
                "the media file {} can't be written: {err}",
                path.display()
            ))
        })?;
        self.written.insert(name.clone());
        Ok(name)
    }
}

/// A stored name's extension: what follows the last dot of its last path segment, when anything
/// does. [`Media::store`] keeps it only when it is 1 to 8 ASCII letters and digits.
pub fn extension_of(stored_name: &str) -> Option<&str> {
    let file = stored_name.rsplit('/').next().unwrap_or(stored_name);
    file.rsplit_once('.')
        .map(|(_, extension)| extension)
        .filter(|extension| !extension.is_empty())
}

/// Copies `content` into `out` through the fixed buffer, hashing it, and returns its SHA-256.
fn copy(mut content: impl Read, out: &mut impl Write) -> io::Result<[u8; 32]> {
    let mut hasher = Sha256::new();
    let mut buffer = vec![0; BUFFER];
    loop {
        let read = match content.read(&mut buffer) {
            Ok(0) => break,
            Ok(read) => read,
            Err(err) if err.kind() == io::ErrorKind::Interrupted => continue,
            Err(err) => return Err(err),
        };
        hasher.update(&buffer[..read]);
        out.write_all(&buffer[..read])?;
    }
    Ok(hasher.finalize().into())
}

/// A payload's name: its hash in lowercase hex, then `.` and the extension when it is usable.
fn name(hash: [u8; 32], extension: Option<&str>) -> String {
    let mut name = String::with_capacity(64 + 1 + EXTENSION_MAX);
    for byte in hash {
        write!(name, "{byte:02x}").expect("writing to a String can't fail");
    }
    if let Some(extension) = extension.filter(|extension| usable(extension)) {
        name.push('.');
        name.push_str(extension);
    }
    name
}

/// Whether an extension may end a name: 1 to 8 ASCII letters and digits, which keeps a stored
/// value from adding a separator, a dot, or anything else to the path.
fn usable(extension: &str) -> bool {
    (1..=EXTENSION_MAX).contains(&extension.len())
        && extension.bytes().all(|byte| byte.is_ascii_alphanumeric())
}

/// Removes a temporary file after a failure; the failure is what is reported.
fn remove(path: &Path) {
    let _ = fs::remove_file(path);
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::cases;

    /// Each payload and stored name make the table's media name, hashed through the store as a
    /// run without `--media` hashes it.
    #[test]
    fn a_payload_and_its_stored_name_make_the_tables_name() {
        for case in cases::group("media_names") {
            let bytes = hex(cases::text(&case, "bytes_hex"));
            let stored = cases::text(&case, "stored_name");
            let name = Media::new(None)
                .store(bytes.as_slice(), extension_of(stored))
                .unwrap();
            assert_eq!(name, cases::text(&case, "name"), "{case}");
        }
    }

    /// A payload longer than the buffer is hashed whole.
    #[test]
    fn a_payload_past_the_buffer_is_hashed_whole() {
        let bytes = vec![b'a'; BUFFER * 2 + 1];
        let name = Media::new(None).store(bytes.as_slice(), None).unwrap();
        let mut whole = String::new();
        for byte in Sha256::digest(&bytes) {
            write!(whole, "{byte:02x}").unwrap();
        }
        assert_eq!(name, whole);
    }

    fn hex(text: &str) -> Vec<u8> {
        (0..text.len())
            .step_by(2)
            .map(|i| u8::from_str_radix(&text[i..i + 2], 16).unwrap())
            .collect()
    }
}
