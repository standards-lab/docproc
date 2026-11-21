# docproc

The extraction library adjacent to Go Elemental, with the system tools it runs nested under
`tools/`, a `code` project managed with the marathon workflow.

- [`README.md`](README.md): the base module, `github.com/standards-lab/docproc`: its packages,
  the system tools each stage needs, its limits, and its development tasks.
- [`README.md`, "The command"](README.md#the-command): docproc/cmd/docproc, the docproc command,
  a nested module on its own tags; its changelog is
  [`cmd/docproc/CHANGELOG.md`](cmd/docproc/CHANGELOG.md).
- [`storage/README.md`](storage/README.md): docproc/storage, the content store over go-storage, a
  nested module on its own tags.
- [`postgres/README.md`](postgres/README.md): docproc/postgres, the index over sqlate on
  PostgreSQL, a nested module on its own tags.
- [`context/README.md`](context/README.md): the working context.
- [`docs/layers.md`](docs/layers.md): docproc's boundaries as signatures and schemas and the pages
  holding its built work's reasoning, the tool contract
  ([`docs/tool-contract.md`](docs/tool-contract.md)) and the design review
  ([`docs/design-review.md`](docs/design-review.md)) among them.
- [`STANDARDS.md`](STANDARDS.md): the judgement calls the standards-reviewer applies.
- [`tools/ooxml/README.md`](tools/ooxml/README.md): docproc-ooxml, its tasks run from
  `tools/ooxml`.
- [`tools/mail/README.md`](tools/mail/README.md): docproc-mail, its tasks run from `tools/mail`.
- [`tools/onenote/README.md`](tools/onenote/README.md): docproc-onenote, its tasks run from
  `tools/onenote`.
- [`tools/envelope/src/lib.rs`](tools/envelope/src/lib.rs): docproc-envelope, the Rust tools'
  shared envelope crate, tested by each Rust tool's check.
- `mise run check`: the check.
- `mise run integration`: the providers' live tiers, `integration:postgres` and
  `integration:storage`, each against its own compose service; `mise run test:integration`, the
  pre-tag run, the tools tier and cmd/docproc's built-binary tier over this checkout's tool
  builds, then `integration`.
