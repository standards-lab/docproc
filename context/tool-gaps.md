# The format coverage's open items

What route and the system tools leave to a later task, each with the page that states it as it
is now.

- **docproc-ooxml's unread kinds.** Text boxes, a pptx's comments, and an xlsx's cell notes are
  docproc-coverage's task ooxml-unread, and equations and SmartArt are backlog; until each is
  read, each kind a document holds is one warning
  ([tools/ooxml/README.md](../tools/ooxml/README.md), "Limits").
- **docproc-mail's DOS code pages.** A DOS code page the review of ~/docs finds, which the
  `codepage` crate doesn't know, becomes a table in the tool, as US-ASCII (20127) is in
  [tools/mail/src/codepage.rs](../tools/mail/src/codepage.rs).
- **route's mbox after a byte order mark.** The `route` package claims an mbox only by a "From "
  line at its first byte, so an mbox that begins with a UTF-8 byte order mark routes Text by its
  mark. docproc-mail's sniff skips the mark and reads it as an mbox
  ([tools/mail/src/sniff.rs](../tools/mail/src/sniff.rs)).
