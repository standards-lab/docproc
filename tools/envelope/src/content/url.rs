//! The content rule's URL tokens: where a plain text holds a URL, and the form two URLs compare
//! in.
//!
//! A URL is a `<scheme:…>` in angle brackets, its whitespace removed (RFC 3986 appendix C), or a
//! GFM extended autolink (GitHub Flavored Markdown 0.29, 6.9): `www.`, `http://`, or `https://`,
//! at a line's start, after whitespace, or after `*`, `_`, `~`, or `(`, then a valid domain, then
//! anything up to whitespace or `<`, less its trailing punctuation, an unbalanced `)`, and an
//! entity reference; a `www.` link takes `http:`. URLs compare after RFC 3986 §6.2.2's
//! syntax-based normalization.

use std::ops::Range;

/// Each URL in `text`, in order: where it is, and its normalized form.
pub fn find(text: &str) -> Vec<(Range<usize>, String)> {
    let mut out = Vec::new();
    let mut at = 0;
    while at < text.len() {
        let rest = &text[at..];
        let found = if rest.starts_with('<') {
            angled(rest)
        } else if may_start_autolink(&text[..at]) {
            autolink(rest)
        } else {
            None
        };
        match found {
            Some((length, url)) => {
                out.push((at..at + length, normalized(&url)));
                at += length;
            }
            None => at += rest.chars().next().map_or(1, char::len_utf8),
        }
    }
    out
}

/// A `<scheme:…>` at the start of `rest`: its length, closing bracket included, and the URL, its
/// whitespace removed. The URL holds no `<`, and something follows its colon.
fn angled(rest: &str) -> Option<(usize, String)> {
    let inner = &rest[1..];
    let scheme = scheme_length(inner)?;
    let end = inner.find(['<', '>'])?;
    if inner.as_bytes()[end] != b'>' || end <= scheme + 1 {
        return None;
    }
    let url: String = inner[..end]
        .chars()
        .filter(|c| !c.is_whitespace())
        .collect();
    (url.len() > scheme + 1).then_some((end + 2, url))
}

/// The length of a URI's scheme at the start of `text`, followed by its colon (RFC 3986 3.1:
/// `ALPHA *( ALPHA / DIGIT / "+" / "-" / "." )`).
fn scheme_length(text: &str) -> Option<usize> {
    let bytes = text.as_bytes();
    if !bytes.first()?.is_ascii_alphabetic() {
        return None;
    }
    let length = bytes
        .iter()
        .take_while(|b| b.is_ascii_alphanumeric() || matches!(b, b'+' | b'-' | b'.'))
        .count();
    (bytes.get(length) == Some(&b':')).then_some(length)
}

/// Whether an extended autolink may start after `before`: at its start, or after whitespace,
/// `*`, `_`, `~`, or `(`.
fn may_start_autolink(before: &str) -> bool {
    before
        .chars()
        .next_back()
        .is_none_or(|c| c.is_whitespace() || matches!(c, '*' | '_' | '~' | '('))
}

/// A GFM extended autolink at the start of `rest`: its length and its URL.
fn autolink(rest: &str) -> Option<(usize, String)> {
    let (domain_start, www) = if rest.starts_with("www.") {
        (0, true)
    } else if rest.starts_with("http://") {
        (7, false)
    } else if rest.starts_with("https://") {
        (8, false)
    } else {
        return None;
    };
    let domain = valid_domain(&rest[domain_start..], www)?;
    let end = rest[domain_start + domain..]
        .find(|c: char| c.is_whitespace() || c == '<')
        .map_or(rest.len(), |end| domain_start + domain + end);
    let length = trimmed(&rest[..end]);
    if length <= domain_start || www && length <= 4 {
        return None;
    }
    let link = &rest[..length];
    let url = if www {
        format!("http://{link}")
    } else {
        link.to_owned()
    };
    Some((length, url))
}

/// The length of a valid domain at the start of `text`: segments of alphanumeric characters,
/// `_`, and `-`, separated by `.`, no `_` in the last two. A `www.` link's holds a period; an
/// `http://` link's needn't.
fn valid_domain(text: &str, www: bool) -> Option<usize> {
    let mut periods = 0;
    let mut underscores = (0, 0);
    let mut length = 0;
    for c in text.chars() {
        match c {
            '.' => {
                periods += 1;
                underscores = (underscores.1, 0);
            }
            '_' => underscores.1 += 1,
            '-' => {}
            c if c.is_alphanumeric() => {}
            _ => break,
        }
        length += c.len_utf8();
    }
    let valid = length > 0 && (periods > 0 || !www) && underscores == (0, 0);
    valid.then_some(length)
}

/// The length of `link` less its trailing punctuation (`?`, `!`, `.`, `,`, `:`, `*`, `_`, `~`), a
/// trailing `)` with more `)` than `(` in the link, and a trailing entity reference, repeatedly.
fn trimmed(link: &str) -> usize {
    let mut end = link.len();
    loop {
        let text = &link[..end];
        let Some(last) = text.chars().next_back() else {
            return 0;
        };
        if matches!(last, '?' | '!' | '.' | ',' | ':' | '*' | '_' | '~') {
            end -= 1;
        } else if last == ')' && text.matches(')').count() > text.matches('(').count() {
            end -= 1;
        } else if last == ';' {
            let name = text[..end - 1]
                .bytes()
                .rev()
                .take_while(u8::is_ascii_alphanumeric)
                .count();
            if name == 0 || !text[..end - 1 - name].ends_with('&') {
                return end;
            }
            end -= name + 2;
        } else {
            return end;
        }
    }
}

/// `url` after RFC 3986 §6.2.2's syntax-based normalization: the scheme and host in lowercase,
/// a percent-encoded unreserved character decoded and every other's hex digits in uppercase, and
/// an absolute URI's dot segments removed (5.2.4).
pub fn normalized(url: &str) -> String {
    let (scheme, rest) = match scheme_length(url) {
        Some(length) => (Some(url[..length].to_ascii_lowercase()), &url[length + 1..]),
        None => (None, url),
    };
    let (authority, rest) = match rest.strip_prefix("//") {
        Some(rest) => {
            let end = rest.find(['/', '?', '#']).unwrap_or(rest.len());
            (Some(&rest[..end]), &rest[end..])
        }
        None => (None, rest),
    };
    let path_end = rest.find(['?', '#']).unwrap_or(rest.len());
    let (path, tail) = rest.split_at(path_end);
    let mut out = String::with_capacity(url.len());
    if let Some(scheme) = &scheme {
        out.push_str(scheme);
        out.push(':');
    }
    if let Some(authority) = authority {
        out.push_str("//");
        let host_start = authority.rfind('@').map_or(0, |at| at + 1);
        out.push_str(&percent_normalized(&authority[..host_start]));
        let host = percent_normalized(&authority[host_start..]);
        out.push_str(&lowercase_outside_escapes(&host));
    }
    let path = percent_normalized(path);
    if scheme.is_some() {
        out.push_str(&without_dot_segments(&path));
    } else {
        out.push_str(&path);
    }
    out.push_str(&percent_normalized(tail));
    out
}

/// `text` with each `%hh` naming an unreserved character (`ALPHA / DIGIT / "-" / "." / "_" /
/// "~"`) decoded, and every other's hex digits in uppercase.
fn percent_normalized(text: &str) -> String {
    let bytes = text.as_bytes();
    let mut out = String::with_capacity(text.len());
    let mut at = 0;
    while at < text.len() {
        let hex = bytes
            .get(at + 1..at + 3)
            .filter(|hex| bytes[at] == b'%' && hex.iter().all(u8::is_ascii_hexdigit));
        if let Some(hex) = hex {
            let value =
                u8::from_str_radix(std::str::from_utf8(hex).unwrap_or("00"), 16).unwrap_or(0);
            if value.is_ascii_alphanumeric() || matches!(value, b'-' | b'.' | b'_' | b'~') {
                out.push(char::from(value));
            } else {
                out.push('%');
                out.push_str(
                    &std::str::from_utf8(hex)
                        .unwrap_or("00")
                        .to_ascii_uppercase(),
                );
            }
            at += 3;
        } else {
            let c = text[at..].chars().next().unwrap_or('\0');
            out.push(c);
            at += c.len_utf8();
        }
    }
    out
}

/// `host` in ASCII lowercase, but each percent-encoding's hex digits.
fn lowercase_outside_escapes(host: &str) -> String {
    let mut out = String::with_capacity(host.len());
    let mut escape = 0;
    for c in host.chars() {
        if escape > 0 {
            escape -= 1;
            out.push(c);
        } else {
            if c == '%' {
                escape = 2;
            }
            out.push(c.to_ascii_lowercase());
        }
    }
    out
}

/// `path` with its `.` and `..` segments removed, by RFC 3986 5.2.4.
fn without_dot_segments(path: &str) -> String {
    let mut input = path;
    let mut out = String::with_capacity(path.len());
    while !input.is_empty() {
        if let Some(rest) = input
            .strip_prefix("../")
            .or_else(|| input.strip_prefix("./"))
        {
            input = rest;
        } else if input.starts_with("/./") {
            input = &input[2..];
        } else if input == "/." {
            input = "/";
        } else if input.starts_with("/../") || input == "/.." {
            input = if input == "/.." { "/" } else { &input[3..] };
            let cut = out.rfind('/').unwrap_or(0);
            out.truncate(cut);
        } else if input == "." || input == ".." {
            input = "";
        } else {
            let skip = usize::from(input.starts_with('/'));
            let end = input[skip..]
                .find('/')
                .map_or(input.len(), |end| skip + end);
            out.push_str(&input[..end]);
            input = &input[end..];
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn urls(text: &str) -> Vec<(&str, String)> {
        find(text)
            .into_iter()
            .map(|(range, url)| (&text[range], url))
            .collect()
    }

    /// GitHub Flavored Markdown 0.29's extended autolink examples, as plain text.
    #[test]
    fn gfm_extended_autolinks() {
        assert_eq!(
            urls("www.commonmark.org/help for more information."),
            [(
                "www.commonmark.org/help",
                "http://www.commonmark.org/help".to_owned()
            )]
        );
        assert_eq!(
            urls("Visit www.commonmark.org/a.b."),
            [(
                "www.commonmark.org/a.b",
                "http://www.commonmark.org/a.b".to_owned()
            )]
        );
        assert_eq!(
            urls("www.google.com/search?q=Markup+(business))")[0].0,
            "www.google.com/search?q=Markup+(business)"
        );
        assert_eq!(
            urls("(www.google.com/search?q=Markup+(business))")[0].0,
            "www.google.com/search?q=Markup+(business)"
        );
        assert_eq!(
            urls("www.google.com/search?q=commonmark&hl;")[0].0,
            "www.google.com/search?q=commonmark"
        );
        assert_eq!(
            urls("www.commonmark.org/he<lp")[0].0,
            "www.commonmark.org/he"
        );
        assert_eq!(
            urls("Visit https://encrypted.google.com/search?q=Markup+(business)")[0].0,
            "https://encrypted.google.com/search?q=Markup+(business)"
        );
        assert_eq!(
            urls("http://localhost:8080/x")[0].1,
            "http://localhost:8080/x"
        );
        assert!(urls("www.a_b.c_d.com nowww.example.com x:www.example.com www.").is_empty());
        assert_eq!(urls("*www.example.com*")[0].0, "www.example.com");
    }

    /// An angle-bracket URI of any scheme, split across lines, is one URL; without a scheme, or
    /// unclosed, it is none.
    #[test]
    fn angle_bracket_uris() {
        assert_eq!(
            urls("see <http://a.example/x\n   y> and <mailto:me@x.example>."),
            [
                (
                    "<http://a.example/x\n   y>",
                    "http://a.example/xy".to_owned()
                ),
                ("<mailto:me@x.example>", "mailto:me@x.example".to_owned()),
            ]
        );
        assert!(urls("<b> <1x:y> <http: > <http://x <x:y").is_empty());
    }

    /// RFC 3986 6.2.2's examples, and its dot-segment examples (5.2.4).
    #[test]
    fn syntax_based_normalization() {
        assert_eq!(
            normalized("HTTP://www.Example.com/"),
            "http://www.example.com/"
        );
        assert_eq!(
            normalized("example://a/b/c/%7Bfoo%7D"),
            "example://a/b/c/%7Bfoo%7D"
        );
        assert_eq!(
            normalized("eXAMPLE://a/./b/../b/%63/%7bfoo%7d"),
            "example://a/b/c/%7Bfoo%7D"
        );
        assert_eq!(
            normalized("http://User%3a@EX%41MPLE.com/%7e%2f"),
            "http://User%3A@example.com/~%2F"
        );
        assert_eq!(without_dot_segments("/a/b/c/./../../g"), "/a/g");
        assert_eq!(without_dot_segments("mid/content=5/../6"), "mid/6");
        assert_eq!(normalized("../a.png"), "../a.png");
    }
}
