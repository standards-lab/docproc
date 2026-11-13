//! The content rule (context/layers.md, schema "2", "The content rule"): which sentences of a
//! source text a target text lacks, compared by standards alone, so that no fixture tunes it.
//! docproc-mail compares a message's text body with the Markdown made from its HTML, and
//! docproc-onenote an image's OCR text with its alt text; both take the rule from here, its one
//! normative implementation, pinned by their goldens.
//!
//! - **Plain text.** Both texts are normalized to Unicode NFC. A Markdown text is read by
//!   CommonMark's semantics first ([`Text::markdown`], behind the `markdown` feature).
//! - **URLs.** A `<scheme:…>` in angle brackets, its whitespace removed, or a GFM extended
//!   autolink is one URL token, compared after RFC 3986 §6.2.2 normalization ([`url`]).
//! - **Words.** The rest splits at UAX #29 word boundaries, and a segment holding a letter or a
//!   digit (general categories L* and N*) is a word token, compared case-sensitively; symbols,
//!   punctuation, and controls are no tokens.
//! - **Alignment.** A longest common subsequence of the two token sequences, by Myers' algorithm
//!   in linear space ([`myers`]).
//! - **Units.** The source's sentences, by UAX #29 sentence boundaries, which also end one at each
//!   line break (SB4). A sentence holding a token outside the alignment is missing.
//! - **Bound.** Past [`EDITS_MAX`] edits the alignment stops, and a source sentence is missing
//!   when its token sequence is no sentence's of the target: set membership, linear in the
//!   tokens.
//!
//! The Unicode version is that of the ICU4X data the crate's exact pins carry, [`UNICODE_VERSION`],
//! part of schema "2": a bump that moves it fails the unit test asserting it.

#[cfg(feature = "markdown")]
mod markdown;
mod myers;
mod url;

use std::collections::{HashMap, HashSet};
use std::ops::Range;

use icu_normalizer::ComposingNormalizerBorrowed;
use icu_properties::CodePointMapData;
use icu_properties::props::{GeneralCategory, GeneralCategoryGroup};
use icu_segmenter::options::{SentenceBreakInvariantOptions, WordBreakInvariantOptions};
use icu_segmenter::{SentenceSegmenter, WordSegmenter};

/// The Unicode version of the content rule's normalization, boundaries, and general categories.
pub const UNICODE_VERSION: &str = "18.0.0";

/// The most edits the alignment runs to; past them, sentences are compared by set membership.
pub const EDITS_MAX: usize = 10_000;

/// A text as the rule reads it: NFC-normalized, with the ranges of it that are each one URL token
/// as they stand, a Markdown link's or image's destination, in order.
#[derive(Debug, Default)]
pub struct Text {
    text: String,
    urls: Vec<Range<usize>>,
}

impl Text {
    /// A plain text, normalized to NFC.
    pub fn plain(text: &str) -> Self {
        Self {
            text: nfc(text),
            urls: Vec::new(),
        }
    }

    /// A Markdown text's plain text, read by CommonMark's semantics: a link as its text followed
    /// by its destination as one URL token, an image as its alt text followed by its destination,
    /// emphasis and backslash escapes removed, and each block ending a line.
    #[cfg(feature = "markdown")]
    pub fn markdown(markdown: &str) -> Self {
        markdown::plain(markdown)
    }

    /// The text, NFC-normalized.
    pub fn as_str(&self) -> &str {
        &self.text
    }
}

/// The sentences of `source` that `target` lacks, in order: each is its NFC text with its
/// leading and trailing whitespace trimmed. Overlap is never returned: a sentence whose tokens
/// all align is in the target already.
pub fn missing(source: &Text, target: &Text) -> Vec<String> {
    let mut ids = Ids::default();
    let from = ids.tokens(source);
    let to = ids.tokens(target);
    let keys = |tokens: &[(usize, u32)]| tokens.iter().map(|(_, id)| *id).collect::<Vec<_>>();
    let units = sentences(&source.text);
    let mut out = Vec::new();
    let kept = myers::kept(&keys(&from), &keys(&to), EDITS_MAX);
    // Past the bound, a sentence is missing when its tokens are no target sentence's.
    let known: HashSet<Vec<u32>> = match kept {
        Some(_) => HashSet::new(),
        None => split(&to, &sentences(&target.text))
            .map(|tokens| keys(&to[tokens]))
            .collect(),
    };
    for (sentence, tokens) in units.iter().zip(split(&from, &units)) {
        let lacked = match &kept {
            Some(kept) => tokens.clone().any(|at| !kept[at]),
            None => !tokens.is_empty() && !known.contains(&keys(&from[tokens])),
        };
        if lacked {
            out.push(source.text[sentence.clone()].trim().to_owned());
        }
    }
    out
}

/// The tokens each sentence holds, as ranges of `tokens`: a token is its sentence's where it
/// starts.
fn split<'a>(
    tokens: &'a [(usize, u32)],
    sentences: &'a [Range<usize>],
) -> impl Iterator<Item = Range<usize>> + 'a {
    let mut at = 0;
    sentences.iter().map(move |sentence| {
        let start = at;
        while at < tokens.len() && tokens[at].0 < sentence.end {
            at += 1;
        }
        start..at
    })
}

/// `text` normalized to NFC.
fn nfc(text: &str) -> String {
    ComposingNormalizerBorrowed::new_nfc()
        .normalize(text)
        .into_owned()
}

/// The UAX #29 sentences of `text`, as ranges; a line break ends one (SB4).
fn sentences(text: &str) -> Vec<Range<usize>> {
    let mut out = Vec::new();
    let mut start = 0;
    for end in SentenceSegmenter::new(SentenceBreakInvariantOptions::default()).segment_str(text) {
        if end > start {
            out.push(start..end);
            start = end;
        }
    }
    out
}

/// A token's comparison key: a word as it stands, or a URL normalized.
#[derive(Hash, PartialEq, Eq)]
enum Key {
    Word(String),
    Url(String),
}

/// Each distinct key's number, so that the alignment compares numbers.
#[derive(Default)]
struct Ids(HashMap<Key, u32>);

impl Ids {
    /// `text`'s tokens in order, each where it starts and its key's number.
    fn tokens(&mut self, text: &Text) -> Vec<(usize, u32)> {
        let mut keys = Vec::new();
        let mut at = 0;
        for range in &text.urls {
            unforced(&text.text, at..range.start, &mut keys);
            let url: String = text.text[range.clone()]
                .chars()
                .filter(|c| !c.is_whitespace())
                .collect();
            keys.push((range.start, Key::Url(url::normalized(&url))));
            at = range.end;
        }
        unforced(&text.text, at..text.text.len(), &mut keys);
        keys.into_iter()
            .map(|(at, key)| {
                let next = u32::try_from(self.0.len()).unwrap_or(u32::MAX);
                (at, *self.0.entry(key).or_insert(next))
            })
            .collect()
    }
}

/// The tokens of `text[range]`, a stretch no destination is in: its URLs, and the words between.
fn unforced(text: &str, range: Range<usize>, out: &mut Vec<(usize, Key)>) {
    let piece = &text[range.clone()];
    let mut at = 0;
    for (span, url) in url::find(piece) {
        words(text, range.start + at..range.start + span.start, out);
        out.push((range.start + span.start, Key::Url(url)));
        at = span.end;
    }
    words(text, range.start + at..range.end, out);
}

/// The word tokens of `text[range]`: its UAX #29 word segments holding a letter or a digit.
fn words(text: &str, range: Range<usize>, out: &mut Vec<(usize, Key)>) {
    let piece = &text[range.clone()];
    let categories = CodePointMapData::<GeneralCategory>::new();
    let mut start = 0;
    for end in WordSegmenter::new_for_non_complex_scripts(WordBreakInvariantOptions::default())
        .segment_str(piece)
    {
        if end <= start {
            continue;
        }
        let segment = &piece[start..end];
        let word = segment.chars().any(|c| {
            let category = categories.get(c);
            GeneralCategoryGroup::Letter.contains(category)
                || GeneralCategoryGroup::Number.contains(category)
        });
        if word {
            out.push((range.start + start, Key::Word(segment.to_owned())));
        }
        start = end;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The Unicode version is part of schema "2": every ICU4X data crate the rule reads carries
    /// the one this crate states, so a bump that moves it fails here, a reviewed change.
    #[test]
    fn the_unicode_version_is_the_stated_one() {
        assert_eq!(icu_normalizer_data::unicode_tag!(), UNICODE_VERSION);
        assert_eq!(icu_properties_data::unicode_tag!(), UNICODE_VERSION);
        assert_eq!(icu_segmenter_data::unicode_tag!(), UNICODE_VERSION);
    }

    fn missing_plain(source: &str, target: &str) -> Vec<String> {
        missing(&Text::plain(source), &Text::plain(target))
    }

    fn words_of(text: &str) -> Vec<String> {
        let mut out = Vec::new();
        words(text, 0..text.len(), &mut out);
        out.into_iter()
            .map(|(_, key)| match key {
                Key::Word(word) => word,
                Key::Url(url) => url,
            })
            .collect()
    }

    /// Words are UAX #29's segments with a letter or a digit; marks stay in their word. The
    /// segmenter is the dictionary-free one, so a run of ideographs is one word.
    #[test]
    fn words_are_segments_holding_a_letter_or_a_digit() {
        assert_eq!(
            words_of("Hello, world! It's 3.14 -- e\u{301}te\u{301} \u{2122} a_b"),
            ["Hello", "world", "It's", "3.14", "e\u{301}te\u{301}", "a_b"]
        );
        assert_eq!(words_of("中文邮件，测试"), ["中文邮件", "测试"]);
        assert!(words_of("... -- !? \u{99} \u{2122}").is_empty());
    }

    /// Sentences end at UAX #29 boundaries and at every line break.
    #[test]
    fn sentences_end_at_boundaries_and_line_breaks() {
        let text = "One. Two? three\nfour\r\nfive";
        let got: Vec<&str> = sentences(text).into_iter().map(|r| &text[r]).collect();
        assert_eq!(got, ["One. ", "Two? ", "three\n", "four\r\n", "five"]);
    }

    /// Only the sentences holding an unaligned token are missing, in order; punctuation,
    /// symbols, case of the surroundings, and NFC differences are no difference, but a word's
    /// case is.
    #[test]
    fn a_sentence_with_an_unaligned_token_is_missing() {
        let source = "Hello there. It is taped into the log book.\nCafe\u{301} — open!\nbye";
        let target = "**Hello** there!\n\nCafé open\n\nBye";
        assert_eq!(
            missing_plain(source, target),
            ["It is taped into the log book.", "bye"]
        );
        assert!(missing_plain("", "x").is_empty());
        assert_eq!(missing_plain("a b. C", ""), ["a b.", "C"]);
        // A lowercase word after a full stop continues the sentence (UAX #29, SB8).
        assert_eq!(missing_plain("a b. c", ""), ["a b. c"]);
        assert!(missing_plain("-- !!\n\u{99}", "").is_empty());
    }

    /// A URL in angle brackets, split across lines, is the one URL token a GFM autolink or a
    /// differently written form of it is.
    #[test]
    fn urls_compare_normalized() {
        assert!(
            missing_plain(
                "See <HTTP://Example.COM/a/./b/../c%7e\n d>",
                "See http://example.com/a/c~d"
            )
            .is_empty()
        );
        assert!(missing_plain("Visit www.example.com.", "Visit http://www.example.com").is_empty());
        assert_eq!(
            missing_plain("Visit www.example.com.", "Visit www.example.org"),
            ["Visit www.example.com."]
        );
    }

    /// Past the edit bound, a sentence is missing when no target sentence has its tokens.
    #[test]
    fn past_the_bound_sentences_compare_by_membership() {
        let source: String = (0..EDITS_MAX)
            .map(|n| format!("Word{n}. "))
            .collect::<String>()
            + "Kept here.";
        let target: String = (0..EDITS_MAX)
            .map(|n| format!("Other{n}. "))
            .collect::<String>()
            + "kept, here!";
        let got = missing_plain(&source, &target);
        assert_eq!(
            got.len(),
            EDITS_MAX + 1,
            "every w sentence and the case-changed one"
        );
        let target = target.replace("kept", "Kept");
        let got = missing_plain(&source, &target);
        assert_eq!(got.len(), EDITS_MAX);
        assert_eq!(
            got.last().map(String::as_str),
            Some(&*format!("Word{}.", EDITS_MAX - 1))
        );
    }
}
