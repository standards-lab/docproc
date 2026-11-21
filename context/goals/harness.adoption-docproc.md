# goal · harness.adoption-docproc

- **State:** building
- **Task:** docproc-comments
- **Branch:** docproc-comments

## Tasks

1. [x] docproc
2. [ ] docproc-comments

## Task brief · docproc-comments

**Problem**

Task docproc brought docproc's configuration, scripts, and context to the five kinds. Its Go,
Rust, and C# code still carries about 10,000 comment lines outside them: docs on unexported
items and in tests, and comments inside function bodies. Much of their reasoning has no other
home yet. envelope, the Rust crate both tools share, is never linted by clippy.

**Behaviors**
1. Go: package comments and doc comments on exported identifiers of exported types stay. Doc
   comments on unexported identifiers, on exported methods and fields of unexported types, and in
   test files go, with comments inside function bodies. Build constraints and directives stay.
2. Rust: `//!` headers and docs on `pub` items in source stay. Private `///`, `//`, and docs on
   test functions and test helpers go.
3. C#: docs on `public` and `protected` members of the tool stay. Other `///`, every `//`, and
   the test project's docs go. Mail's fixture generator follows the same rule.
4. Reasoning a removed comment carried that the code doesn't already say moves into a name, an
   exported doc, a package comment, or a `docs/` page, with a one-line `See` reference where the
   code needs a pointer.
5. Behavior is unchanged: every module's and tool's tests pass with their goldens unchanged.
   Renames stay within unexported identifiers. No CHANGELOG lines.
6. envelope is linted by clippy with warnings denied in both Rust tools' checks, on its own
   locked crate graph; currency reports that graph and upgrade rewrites it; its one existing lint
   is fixed. (C1)
7. lint-context and lint-references pass; each `docs/` page that receives reasoning keeps one
   home for each statement.

**Test seams**
- `mise run check` (check:go and each tool's check); lint-references; `mise run currency`.

**Slices**
1. Go foundations: tool, inspect, route, rundir, sevenzip, internal helpers.
2. Go readers I: document, office, onenote.
3. Go readers II: audio, picture, media, email.
4. Go stages.
5. Go dispatch.
6. Go nested modules and the command: storage, postgres, cmd/docproc.
7. envelope's clippy run.
8. Rust: envelope and onenote.
9. Rust: mail.
10. C#: ooxml and mail's fixture generator.

**Out of scope**

Package-comment and exported-doc content, except where it receives moved reasoning;
configuration, script, SQL, and Python comments (task docproc); C# suppression rules; coordinator
edits; releases.

**Door**

Two-way: no release; every change is a comment, an unexported rename, or a lint fix.

## Progress

slices 10/10 committed (+ fixture generators) · standards ✓ · spec ✓ · editor ✓

## Decisions

## Pending edits

- coordinator · context/system-tools.md:63 → docs/tool-contract.md, "Versioning".
- coordinator · context/ai-strategy.md:61, :94 and docs/ai-strategy.md:222, :225 → layers.md at
  docproc commit f3633c3; docs/ai-strategy.md:129 → docs/changes.md, "The nested modules".
- coordinator · context/ai-strategy.md:109 and docs/ai-strategy.md:422 → docs/acceptance.md.
- coordinator · context/data-intelligence.md:27, :99 → docs/changes.md; :85, :96 and
  docs/data-intelligence.md:46 → layers.md at docproc commit f3633c3; docs/data-intelligence.md:5
  → context/cerebro.md.
- coordinator · docs/messaging.md:50 and roadmap/capstone.data-intelligence.toml:94 →
  context/cerebro.md, "State and subjects".
- coordinator · roadmap/v1.ai.inference.toml: summary :9 and :26; drop context entry :19.
- coordinator · roadmap/v1.ai.processing.toml:170 and :192: `final` writes to docs/acceptance.md.
- go-ai · context/README.md:10 and docs/design.md:19: cite layers.md "go-ai's surface" at docproc
  commit f3633c3.
