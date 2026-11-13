# docproc

The extraction library adjacent to Go Elemental, with the system tools it runs nested under
`tools/`, a `code` project managed with the marathon workflow.

- [`context/README.md`](context/README.md): the working context.
- [`STANDARDS.md`](STANDARDS.md): the judgement calls the standards-reviewer applies.
- [`tools/ooxml/README.md`](tools/ooxml/README.md): docproc-ooxml, its tasks run from
  `tools/ooxml`.
- [`tools/mail/README.md`](tools/mail/README.md): docproc-mail, its tasks run from `tools/mail`.
- [`tools/envelope/src/lib.rs`](tools/envelope/src/lib.rs): docproc-envelope, the Rust tools'
  shared envelope crate, tested by each Rust tool's check.
- `mise run check`: the check.
