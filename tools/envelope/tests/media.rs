//! The media store against a real directory, written under Cargo's temporary directory for
//! integration tests, so a check writes only target/.

use std::fs;
use std::io::{self, Read};
use std::path::PathBuf;

use docproc_envelope::{Fault, Media};

const ABC: &str = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad";

fn scratch(test: &str) -> PathBuf {
    let dir = PathBuf::from(env!("CARGO_TARGET_TMPDIR")).join(format!("envelope-media-{test}"));
    let _ = fs::remove_dir_all(&dir);
    dir
}

fn entries(dir: &PathBuf) -> Vec<String> {
    let mut names: Vec<String> = fs::read_dir(dir)
        .unwrap()
        .map(|entry| entry.unwrap().file_name().into_string().unwrap())
        .collect();
    names.sort();
    names
}

#[test]
fn the_directory_is_created_once_the_input_is_read() {
    let dir = scratch("created").join("made");
    let mut media = Media::new(Some(dir.clone()));
    assert!(!dir.exists());
    media.create().unwrap();
    assert!(dir.is_dir());
    assert!(entries(&dir).is_empty());
    let name = media.store(&b"abc"[..], Some("txt")).unwrap();
    assert_eq!(name, format!("{ABC}.txt"));
    assert_eq!(fs::read(dir.join(&name)).unwrap(), b"abc");
    assert_eq!(entries(&dir), [name]);
}

#[test]
fn a_repeated_payload_is_written_once() {
    let dir = scratch("once");
    let mut media = Media::new(Some(dir.clone()));
    media.create().unwrap();
    let first = media.store(&b"abc"[..], Some("txt")).unwrap();
    fs::remove_file(dir.join(&first)).unwrap();
    let second = media.store(&b"abc"[..], Some("txt")).unwrap();
    assert_eq!(second, first);
    assert!(entries(&dir).is_empty());
}

#[test]
fn without_a_directory_a_payload_is_hashed_only() {
    let mut media = Media::new(None);
    media.create().unwrap();
    assert_eq!(
        media.store(&b"abc"[..], Some("png")).unwrap(),
        format!("{ABC}.png")
    );
}

#[test]
fn a_directory_that_cant_be_created_is_a_media_fault() {
    let dir = scratch("blocked");
    fs::create_dir_all(&dir).unwrap();
    let file = dir.join("file");
    fs::write(&file, b"").unwrap();
    let fault = Media::new(Some(file.join("below"))).create().unwrap_err();
    assert!(matches!(&fault, Fault::Media(reason) if reason.starts_with("the media directory ")));
    assert_eq!(fault.code(), 1);
}

#[test]
fn a_payload_that_cant_be_read_leaves_nothing() {
    struct Failing;
    impl std::io::Read for Failing {
        fn read(&mut self, _: &mut [u8]) -> std::io::Result<usize> {
            Err(std::io::Error::other("the reader failed"))
        }
    }
    let dir = scratch("failing");
    let mut media = Media::new(Some(dir.clone()));
    media.create().unwrap();
    let fault = media.store(Failing, None).unwrap_err();
    assert_eq!(fault.code(), 1);
    assert!(entries(&dir).is_empty());
}

#[test]
fn a_payload_that_cant_be_read_is_worded_as_a_read_failure() {
    let dir = scratch("unreadable");
    fs::create_dir_all(&dir).unwrap();
    for media in [None, Some(dir.clone())] {
        let fault = Media::new(media.clone())
            .store(Unreadable, None)
            .unwrap_err();
        assert_eq!(fault.code(), 1, "{media:?}");
        assert_eq!(
            fault.reason(),
            "a medium can't be read: the payload is gone",
            "{media:?}"
        );
    }
    assert!(entries(&dir).is_empty());
}

#[test]
fn a_media_file_that_cant_be_written_is_named() {
    let dir = scratch("unwritable").join("missing");
    let fault = Media::new(Some(dir.clone()))
        .store(&b"abc"[..], None)
        .unwrap_err();
    assert_eq!(fault.code(), 1);
    let temporary = dir.join(format!(".{}-1.tmp", std::process::id()));
    assert_eq!(
        fault.reason(),
        format!(
            "the media file {} can't be written: No such file or directory (os error 2)",
            temporary.display()
        )
    );
}

struct Unreadable;

impl Read for Unreadable {
    fn read(&mut self, _: &mut [u8]) -> io::Result<usize> {
        Err(io::Error::other("the payload is gone"))
    }
}
