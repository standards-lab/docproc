# goal · harness.adoption-docproc

- **State:** building
- **Task:** docproc
- **Branch:** docproc

## Tasks

1. [ ] docproc
2. [ ] docproc-comments

## Task brief · docproc

**Problem**

docproc fails lint-context by about 40,000 words, has no marathon pin or lint wiring, and CI never
runs a context lint. Its notes describe built work, its configuration and script comments carry
reasoning, and its v1.ai.processing record is over three budgets.

**Behaviors**
1. Currency reports nothing trailing except the held MimeKit and typed-path. The storage and
   postgres modules require docproc v0.4.1, each with a CHANGELOG line under Unreleased.
2. The root mise.toml pins marathon 0.22.0 under `task.run_auto_install = false`, and currency
   reports a newer marathon tag.
3. Every golangci-lint configuration requires each suppression to name its linter and give a
   reason (nolintlint with require-explanation and require-specific). Every Rust crate denies
   `allow_attributes_without_reason`.
4. lint-context passes: notes and the README are under budget; every map entry is marked and links
   its home; built-work reasoning lives in `docs/` pages, the tool contract on its own page and the
   acceptance results on theirs, content unchanged; README detail moves to `docs/`, content
   unchanged; Cerebro's boundary is a `(planned)` note; layers.md's sections on other repositories'
   built work are gone.
5. Every pointer to a moved section names its new page. lint-references passes with no skips.
6. Every configuration-file and script comment, ooxml's Python fixture generator included, is one of
   the five kinds. Headers are a short description plus `See` lines; a script's section comments
   become named functions.
7. STANDARDS.md's comment rule covers code, tests, scripts, and every configuration file. Its
   "homes" line names the new homes, and its suppression line names Go's and Rust's stock rules;
   C# stays reviewer judgement.
8. The orientation points at `standards-lab/roadmap/index.toml`.
9. The v1.ai.processing record passes lint-context and still binds mail-hardening, final, its sync,
   and its holds. The shipped brief goes.
10. The check's `lint` runs shellcheck, actionlint, the tag-routing test, and both lints, failing if
    any fails, printing `FAIL: <lint> is not on PATH; run mise install` when a lint is missing.
    CI's `lint` job installs the pin, runs on every change, and passes. Task descriptions name what
    runs.

**Test seams**
- `mise run check`; lint-context; lint-references; `mise run currency`.

**Slices**
1. Upgrade: storage and postgres require docproc v0.4.1.
2. Pin and suppression rules.
3. Cross-repo sections out of layers.md; the Cerebro note.
4. layers.md and design.md into `docs/`, the tool contract's page included; in-repo pointers.
5. Acceptance and README detail into `docs/`; the map and the orientation pointer.
6. The v1.ai.processing record.
7. Configuration comments and STANDARDS.md.
8. Script and generator comments.
9. Lints wired into the check and CI.

**Out of scope**

Go, Rust, and C# code comments (docproc-comments), except pointer repointing; coordinator, go-ai,
and docinfer edits (pending edits); v1.ai.processing task work; releases.

**Door**

Two-way: no release.

## Progress

slices 9/9 committed · standards ✓ · spec ✓ (1 gap: fixture-script See paths) · editor ✓

## Decisions

- docproc: C# suppression reasons stay reviewer judgement; .NET has no stock rule. (P5)

## Pending edits

- coordinator · context/system-tools.md:63 → docs/tool-contract.md, "Versioning".
- coordinator · context/ai-strategy.md:61, :93 and docs/ai-strategy.md:222, :225 → layers.md at
  docproc commit f3633c3; docs/ai-strategy.md:129 → docs/changes.md, "The nested modules".
- coordinator · context/ai-strategy.md:108 and docs/ai-strategy.md:422 → docs/acceptance.md.
- coordinator · context/data-intelligence.md:27, :99 → docs/changes.md; :85, :96 and
  docs/data-intelligence.md:46 → layers.md at docproc commit f3633c3; docs/data-intelligence.md:5
  → context/cerebro.md.
- coordinator · docs/messaging.md:50 and roadmap/capstone.data-intelligence.toml:94 →
  context/cerebro.md, "State and subjects".
- coordinator · roadmap/v1.ai.inference.toml: summary :9 and :26; drop context entry :19.
- coordinator · roadmap/v1.ai.processing.toml:170 and :192: `final` writes to docs/acceptance.md.
- go-ai · context/README.md:10 and docs/design.md:19: cite layers.md "go-ai's surface" at docproc
  commit f3633c3.
- docinfer · (adopt branch) context/README.md:12, context/command.md:16, context/command-api.md:3
  cite layers.md at docproc commit 673ed3d, which keeps the docinfer sections: no change;
  docs/no-defaults.md:23's "docproc's layers" → layers.md at f3633c3.
