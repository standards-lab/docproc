//! tools/envelope/cases.json, read in place: the one table of the envelope's cases, which no
//! test here restates.

use serde_json::Value;

/// The table, parsed.
pub fn table() -> Value {
    let path = concat!(env!("CARGO_MANIFEST_DIR"), "/cases.json");
    let text = std::fs::read_to_string(path).expect("tools/envelope/cases.json is readable");
    serde_json::from_str(&text).expect("tools/envelope/cases.json is JSON")
}

/// One group of the table, such as `warnings`, as its cases.
pub fn group(name: &str) -> Vec<Value> {
    let cases = table()[name]
        .as_array()
        .unwrap_or_else(|| panic!("cases.json has a {name} list"))
        .clone();
    assert!(!cases.is_empty(), "cases.json's {name} list is empty");
    cases
}

/// A case's string field.
pub fn text<'a>(case: &'a Value, field: &str) -> &'a str {
    case[field]
        .as_str()
        .unwrap_or_else(|| panic!("a case's {field} is a string: {case}"))
}
