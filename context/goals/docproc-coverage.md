# goal · docproc-coverage

- **State:** idle
- **Task:** none
- **Branch:** none

## Tasks

1. [x] text-encoding-keys
2. [x] archives-and-mpeg
3. [x] text-last-resort
4. [ ] pdf-text-padding
5. [ ] office-soffice
6. [ ] ooxml-unread
7. [ ] ooxml-arm64-floor
8. [ ] tnef

## Decisions

- text-encoding-keys: held MimeKit at 4.18.2: 5.0.0 declined, PstBuilder.Eml 1.3.0 is built on
  MimeKit 4.
- text-encoding-keys: held MsgKit at 3.0.6: 3.1.0 declined, it needs MimeKit.Core 5.
- text-encoding-keys: held typed-path at 0.12.4: 0.13.0 declined, onenote_parser 2.1.0 requires
  ^0.12.3.

## Pending edits

- docs-corpus · once docproc-coverage syncs, tell corpus.docs (context/goals/corpus.docs.md, task
  docproc-final; standards-lab #219) the final release's tags, docproc, storage, postgres, and
  cmd/docproc, and the run-directory schema it writes; that `#!` scripts and other unlisted text
  route text; and that exec-4's check:polyglot can reopen. Intermediate releases go untold.
- coordinator · roadmap/docproc-backlog.toml: a text retry for a part whose stage fails at
  runtime, such as a corrupt PDF.
