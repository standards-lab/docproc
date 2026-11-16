//! Unicode general categories, for a tool that sorts characters by them: docproc-onenote's math
//! port sorts an equation's characters into runs by them. Each predicate reads the general
//! category from icu_properties, the content rule's own source, so the tools read one Unicode
//! version, [`UNICODE_VERSION`](crate::content::UNICODE_VERSION), and link no second Unicode
//! table.

use icu_properties::CodePointMapData;
use icu_properties::props::{GeneralCategory, GeneralCategoryGroup};

fn category(c: char) -> GeneralCategory {
    CodePointMapData::<GeneralCategory>::new().get(c)
}

/// Whether `c` is a format character (Cf), such as U+200B ZERO WIDTH SPACE or U+2061 FUNCTION
/// APPLICATION.
pub fn is_format(c: char) -> bool {
    category(c) == GeneralCategory::Format
}

/// Whether `c` is a space separator (Zs), such as U+0020 SPACE or U+00A0 NO-BREAK SPACE; a line
/// or paragraph separator, or a control such as a tab, is not one.
pub fn is_space_separator(c: char) -> bool {
    category(c) == GeneralCategory::SpaceSeparator
}

/// Whether `c` is a number (N*): a decimal digit (Nd), a letter number (Nl), or another number
/// (No).
pub fn is_number(c: char) -> bool {
    GeneralCategoryGroup::Number.contains(category(c))
}

/// Whether `c` is a math symbol (Sm), such as `+` or U+2211 N-ARY SUMMATION.
pub fn is_math_symbol(c: char) -> bool {
    category(c) == GeneralCategory::MathSymbol
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn format_characters_are_cf() {
        for c in ['\u{200b}', '\u{2061}', '\u{feff}', '\u{e0001}'] {
            assert!(is_format(c), "{c:?}");
        }
        for c in ['a', ' ', '\u{0301}', '\t'] {
            assert!(!is_format(c), "{c:?}");
        }
    }

    #[test]
    fn space_separators_are_zs() {
        for c in [' ', '\u{a0}', '\u{2009}', '\u{3000}'] {
            assert!(is_space_separator(c), "{c:?}");
        }
        for c in ['\t', '\n', '\u{2028}', '\u{2029}', '\u{200b}'] {
            assert!(!is_space_separator(c), "{c:?}");
        }
    }

    #[test]
    fn numbers_are_every_n_category() {
        for c in ['7', '\u{0663}', '\u{216b}', '\u{bd}', '\u{1d7d8}'] {
            assert!(is_number(c), "{c:?}");
        }
        for c in ['x', '+', '\u{221e}'] {
            assert!(!is_number(c), "{c:?}");
        }
    }

    #[test]
    fn math_symbols_are_sm() {
        for c in ['+', '=', '\u{2212}', '\u{2211}', '\u{221e}', '\u{2192}'] {
            assert!(is_math_symbol(c), "{c:?}");
        }
        for c in ['-', '(', '\u{2061}', 'x'] {
            assert!(!is_math_symbol(c), "{c:?}");
        }
    }
}
