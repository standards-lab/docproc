# testdata

The email package's own fixture. docproc-mail's fixtures and goldens are not copied here: the
tests read them in place under `tools/mail/tests/`, through `internal/tooltest`'s `Tools`.

## built

`built/` holds one `.msg` built for this package: `msg/office-attachments.msg`, built from
`eml/office-attachments.eml` by spike-ai-pipeline-mail's fixtures generator at its tag `v0.1.0`,
in its fixtures mode, through MsgKit 3.0.6 and MimeKit 4.18.2 on the .NET SDK 10.0.402. The
source attaches five files, byte for byte. Four are this repository's committed office fixtures:
`minutes.docx`, which is `office/testdata/ooxml/tables.libreoffice.docx`; `budget.xls`, which is
`office/testdata/legacy/generated/formulas.libreoffice.xls`; `scan.tif`, the
`word/media/image1.tiff` member of `office/testdata/pictures/tiff.docx`; and `diagram.emf`, the
`word/media/image4.emf` member of `office/testdata/ooxml/word-with-attachments.docx`. Their
licenses are the office fixtures' (`office/testdata/README.md`). The fifth, `forwarded.msg`, is
spike-ai-pipeline-mail v0.3.1's generated `03-html-inline-image.msg`, attached as a file, which
docproc-mail reads back by value and writes as it is, not as an embedded message; docproc-mail's
own generated fixtures were rebuilt since, so its copy there differs byte for byte.

MsgKit's `.msg` isn't byte-stable, so a rebuild changes its SHA-256; the tests pin its content
instead, each of the four office attachments docproc-mail writes byte for byte its source. MsgKit
stores the EMF and the `.msg` as `application/octet-stream`, whatever the source's `image/emf` and
`application/vnd.ms-outlook`. dispatch's integration tier dispatches it through every stage its
attachments reach.
