//! The media store of `--media <dir>`. Each distinct payload is one file there, named by the
//! lowercase hex SHA-256 of its bytes and, when the stored extension is 1 to 8 ASCII letters and
//! digits, `.` and that extension, its case kept; a stored name is never otherwise part of a path.
//! A payload read from a reader is streamed: hashed while it is copied through a fixed buffer into
//! a temporary file in the directory, which is then renamed to its name, so a name in the
//! directory always holds the whole payload. A payload already in memory is hashed where it lies
//! ([`Media::store_bytes`], [`Payload`]), and written whole into the temporary file only when its
//! name is not yet written. Without `--media` a payload is hashed only, the names stand, and
//! nothing is written to disk.
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

const BUFFER: usize = 64 * 1024;

const EXTENSION_MAX: usize = 8;

/// A payload held in memory with its name, the name [`Media::store_bytes`] gives it, hashed once
/// when it is made; [`Media::store_payload`] stores it by that name.
pub struct Payload {
    bytes: Vec<u8>,
    name: String,
}

impl Payload {
    /// Names `bytes`, with the extension they were stored under.
    pub fn new(bytes: Vec<u8>, extension: Option<&str>) -> Self {
        let name = name(Sha256::digest(&bytes).into(), extension);
        Self { bytes, name }
    }

    /// The payload's name.
    pub fn name(&self) -> &str {
        &self.name
    }
}

pub struct Media {
    dir: Option<PathBuf>,
    written: HashSet<String>,
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
    /// payload that can't be read or written is a media fault, exit 1: one that can't be read is
    /// worded as it is without `--media`, never naming the temporary file it was being copied
    /// into, and a media file that can't be written is named.
    pub fn store(&mut self, content: impl Read, extension: Option<&str>) -> Result<String, Fault> {
        let Some(dir) = &self.dir else {
            let hash = copy(content, &mut io::sink()).map_err(|failed| {
                let (Failed::Read(err) | Failed::Write(err)) = failed;
                unreadable(err)
            })?;
            return Ok(name(hash, extension));
        };
        let temporary = temporary(dir, &mut self.temporaries);
        let written = File::create_new(&temporary)
            .map_err(Failed::Write)
            .and_then(|mut file| {
                let hash = copy(content, &mut file)?;
                file.flush().map_err(Failed::Write)?;
                Ok(hash)
            });
        let hash = written.map_err(|failed| {
            remove(&temporary);
            failed.fault(&temporary)
        })?;
        let name = name(hash, extension);
        if self.written.contains(&name) {
            remove(&temporary);
            return Ok(name);
        }
        renamed(dir, &temporary, &name, &mut self.written)?;
        Ok(name)
    }

    /// Stores one payload held in memory, as [`Media::store`] stores one read from a reader, and
    /// returns its name: hashed in place, and with `--media` written unless this run already
    /// wrote that name. A media file that can't be written is named, a media fault.
    pub fn store_bytes(&mut self, bytes: &[u8], extension: Option<&str>) -> Result<String, Fault> {
        let name = name(Sha256::digest(bytes).into(), extension);
        self.write(&name, bytes)?;
        Ok(name)
    }

    /// Stores a [`Payload`] by its name, as [`Media::store_bytes`] stores its bytes, and returns
    /// the name.
    pub fn store_payload(&mut self, payload: &Payload) -> Result<String, Fault> {
        self.write(&payload.name, &payload.bytes)?;
        Ok(payload.name.clone())
    }

    fn write(&mut self, name: &str, bytes: &[u8]) -> Result<(), Fault> {
        let Some(dir) = &self.dir else {
            return Ok(());
        };
        if self.written.contains(name) {
            return Ok(());
        }
        let temporary = temporary(dir, &mut self.temporaries);
        File::create_new(&temporary)
            .and_then(|mut file| {
                file.write_all(bytes)?;
                file.flush()
            })
            .map_err(|err| {
                remove(&temporary);
                Failed::Write(err).fault(&temporary)
            })?;
        renamed(dir, &temporary, name, &mut self.written)
    }
}

fn temporary(dir: &Path, temporaries: &mut u64) -> PathBuf {
    *temporaries += 1;
    dir.join(format!(".{}-{temporaries}.tmp", std::process::id()))
}

fn renamed(
    dir: &Path,
    temporary: &Path,
    name: &str,
    written: &mut HashSet<String>,
) -> Result<(), Fault> {
    let path = dir.join(name);
    fs::rename(temporary, &path).map_err(|err| {
        remove(temporary);
        Fault::Media(format!(
            "the media file {} can't be written: {err}",
            path.display()
        ))
    })?;
    written.insert(name.to_owned());
    Ok(())
}

/// A stored name's extension: what follows the last dot of its last path segment, when anything
/// does. [`Media::store`] keeps it only when it is 1 to 8 ASCII letters and digits.
pub fn extension_of(stored_name: &str) -> Option<&str> {
    let file = stored_name.rsplit('/').next().unwrap_or(stored_name);
    file.rsplit_once('.')
        .map(|(_, extension)| extension)
        .filter(|extension| !extension.is_empty())
}

enum Failed {
    Read(io::Error),
    Write(io::Error),
}

impl Failed {
    fn fault(self, file: &Path) -> Fault {
        match self {
            Failed::Read(err) => unreadable(err),
            Failed::Write(err) => Fault::Media(format!(
                "the media file {} can't be written: {err}",
                file.display()
            )),
        }
    }
}

fn unreadable(err: io::Error) -> Fault {
    Fault::Media(format!("a medium can't be read: {err}"))
}

fn copy(mut content: impl Read, out: &mut impl Write) -> Result<[u8; 32], Failed> {
    let mut hasher = Sha256::new();
    let mut buffer = vec![0; BUFFER];
    loop {
        let read = match content.read(&mut buffer) {
            Ok(0) => break,
            Ok(read) => read,
            Err(err) if err.kind() == io::ErrorKind::Interrupted => continue,
            Err(err) => return Err(Failed::Read(err)),
        };
        hasher.update(&buffer[..read]);
        out.write_all(&buffer[..read]).map_err(Failed::Write)?;
    }
    Ok(hasher.finalize().into())
}

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

fn usable(extension: &str) -> bool {
    (1..=EXTENSION_MAX).contains(&extension.len())
        && extension.bytes().all(|byte| byte.is_ascii_alphanumeric())
}

fn remove(path: &Path) {
    let _ = fs::remove_file(path);
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::cases;

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

    #[test]
    fn a_payload_in_memory_is_named_as_one_read_is() {
        for case in cases::group("media_names") {
            let bytes = hex(cases::text(&case, "bytes_hex"));
            let extension = extension_of(cases::text(&case, "stored_name"));
            let name = cases::text(&case, "name");
            let mut media = Media::new(None);
            assert_eq!(
                media.store_bytes(&bytes, extension).unwrap(),
                name,
                "{case}"
            );
            let payload = Payload::new(bytes, extension);
            assert_eq!(payload.name(), name, "{case}");
            assert_eq!(media.store_payload(&payload).unwrap(), name, "{case}");
        }
    }

    fn hex(text: &str) -> Vec<u8> {
        (0..text.len())
            .step_by(2)
            .map(|i| u8::from_str_radix(&text[i..i + 2], 16).unwrap())
            .collect()
    }
}
