# The tools' open items

What the system tools leave to a later task, each with the page that states it as it is now.

- **docproc-ooxml's unread kinds.** Reading text boxes, equations, SmartArt, a pptx's comments,
  and an xlsx's cell notes is backlog; until then each kind a document holds is one warning
  ([tools/ooxml/README.md](../tools/ooxml/README.md), "Limits").
- **docproc-ooxml's arm64 floor.** Its address-space floor under an RLIMIT_AS set before exec is
  measured on x64 alone; on arm64 it is unmeasured
  ([docs/acceptance.md](../docs/acceptance.md), "Peak memory").
- **docproc-mail's DOS code pages.** A DOS code page the review of ~/docs finds, which the
  `codepage` crate doesn't know, becomes a table in the tool, as US-ASCII (20127) is in
  [tools/mail/src/codepage.rs](../tools/mail/src/codepage.rs).
