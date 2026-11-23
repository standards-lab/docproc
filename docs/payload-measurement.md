# Payload measurement

The measurement is taken over `~/docs` as it stood on 2026-11-12. It finds, for each structured
field, the text already carried, in the Markdown or in a kept field, which is cut, and the content
the Markdown lacks, which its tool renders into the Markdown
([`structured-text.md`](structured-text.md)); labels kept as plain-text fields are not counted. The
bytes size both. It is rerun once the planned goal reviewing `~/docs` settles what the corpus holds.

The three spikes were built from their release tags (office `v0.1.0`, mail `v0.3.1`, onenote
`v0.1.0`; the commits past them touch only a README) and run over every file in `~/docs`, at any
depth, whose extension names their formats; no golden fallback was needed. Parts nested in
archives or attachments, and legacy formats LibreOffice converts first, are outside it. Only
inputs that exit 0 are measured.

```sh
S=<scratch dir>
for t in office:v0.1.0 mail:v0.3.1 onenote:v0.1.0; do mkdir -p $S/src/${t%:*}
  git -C ~/experiments/spike-ai-pipeline-${t%:*} archive ${t#*:} | tar -x -C $S/src/${t%:*}; done
(cd $S/src/mail && CARGO_TARGET_DIR=$S/target-mail cargo build --release --locked)
(cd $S/src/onenote && CARGO_TARGET_DIR=$S/target-onenote cargo build --release --locked)
(cd $S/src/office && mise trust . && mise exec -- dotnet publish src/SpikeAiPipeline.Office \
   -c Release -r linux-x64 -p:NuGetAudit=false -o $S/office-bin)
run() { # run <tool> <find -iname patterns…>; one numbered directory per input under $S/out/<tool>
  t=$1; shift; i=0; p=(); for e; do p+=(-o -iname "*.$e"); done
  find ~/docs -type f \( "${p[@]:1}" \) -print0 | sort -z | while IFS= read -r -d '' f; do
    i=$((i+1)); o=$S/out/$t/$i; mkdir -p $o; case $t in
    office)  $S/office-bin/spike-ai-pipeline-office "$f" --format markdown > $o/text.md
             $S/office-bin/spike-ai-pipeline-office "$f" --media $o/media > $o/model.json ;;
    mail)    $S/target-mail/release/spike-ai-pipeline-mail --attachments $o/media "$f" > $o/out.jsonl ;;
    onenote) $S/target-onenote/release/spike-ai-pipeline-onenote --media $o/media "$f" > $o/out.jsonl ;;
    esac 2> $o/stderr; echo $? > $o/exit; done; }
run office docx docm dotx dotm xlsx xlsm xltx xltm pptx pptm potx potm ppsx
run mail eml msg pst mbox
run onenote one   # sections only: notebooks would count their sections twice
```

office's publish needs `-p:NuGetAudit=false`: the spike's .NET 10 restore fails on
Microsoft.DotNet.ILCompiler 10.0.13's advisory GHSA-p3jp-9v83-76w9 as an error.

The measure: a structured text field is a string the contract carries outside its Markdown; its
bytes are the string's UTF-8 bytes before JSON escaping. A string is duplicated when, with
whitespace collapsed to one space and trimmed, it occurs in its line's Markdown with Markdown
backslash escapes removed and whitespace collapsed the same way (office's document, mail's message,
onenote's page). A mail body form (`text`, `html`, `rtf`, `derived.html`) is an alternative of the
Markdown made from it, and counts whole in the table; the mail bullet below compares its text
with the Markdown. The fields counted:

- **office**: every string in the model, and every numeric cell `value` as its JSON text, except
  the enumerations and references `type`, `format`, `schema`, `contentType`, `source`, `target`,
  `valueType`, `ref`, `sharedFrom`, `chartType`, `kind`, and a media block's `name`.
- **mail**: each message's (nested ones included) `subject`, `transport_headers`,
  `conversation_topic`, attachment `filename`, and body forms; addresses, folder paths, and the
  `store` line's `display_name` (one per .pst) are not counted.
- **onenote**: each page's `title`, `author`, and every string under `content` and `ink_text`
  except `kind`, `style`, `file`, `extension`, `font_color`, `highlight`, `math`, `shape`,
  `status`, `completed_time`, `type`, `number`, `prefix`, `suffix`, `bullet`, `id`, `section`, and
  the times.

Output is the bytes on stdout; office's is `model.json` plus `,"markdown":` and `text.md` as one
JSON string, as one run will write them. The run reproduced byte for byte.

| Tool | Read | Output | Markdown | Structured text | Duplicated | Share |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| office | 85 of 85 (43 docx, 25 xlsx, 17 pptx families) | 622,303 B | 53,972 B | 36,179 B | 35,526 B | 5.7% |
| mail | 72 of 79 (47 eml, 3 mbox, 16 msg, 6 pst); 599 messages | 5,034,929 B | 1,486,558 B | 3,057,909 B | 1,713,649 B | 34.0% |
| onenote | 16 of 17 sections; 20 pages | 136,702 B | 29,706 B | 26,802 B | 24,765 B | 18.1% |

Refused: 7 cpython `.eml` fixtures that are no message (`msg_11`, `18`, `19`, `37`–`40`), and
`Encryption.one`, password-protected; both as their READMEs state.

- **office.** 98% of the model's text is in the Markdown: inline text 24,237 B (24,145
  duplicated), text cell values 5,547 B (5,505), numeric cell values 391 B (359), chart
  categories, names, and titles 3,288 B (all), comment anchors and authors 1,024 B (all), picture
  alt text 797 B (all), sheet names 341 B (all); formulas 534 B (67, since Markdown shows a
  formula only without a value). The inline text and cell values not counted duplicated are in
  the Markdown under markup the measure doesn't remove: a paragraph's line break as a trailing
  backslash (92 B), a cell's line break as `<br>` (42 B), booleans as `TRUE` and `FALSE` (32 B).
  Picture titles, 20 B, are the one text the Markdown lacks. The larger waste is the model's
  shape: 564,912 B written indented is 182,072 B compact, so indentation is 382,840 B, 62% of the
  output. Compact, the text block tree is 122,509 B beside sheet tables 40,506 B, media blocks
  9,773 B, and charts 9,284 B. By family: docx 7.3%, xlsx 4.4%, pptx 3.4% duplicated.
- **mail.** Body forms are the duplication: `text` 1,461,216 B, `rtf` 145,003 B, `derived.html`
  56,631 B, `html` 50,332 B. A `.msg` is 78% duplicated (RTF and the HTML recovered from it), a
  `.pst` 46%, an mbox 31%, an `.eml` 7%. `transport_headers` is 1,305,095 B, 26% of the output;
  114,724 B of it repeats the parsed subject, addresses, and threading fields.
- **mail's text body against the Markdown.** The span is a line of the text body (split at line
  feeds, CR LF as one), whitespace collapsed and trimmed, empty lines skipped, compared with its
  message's Markdown by the duplication rule above. Of 1,235,972 B of text-body lines, 19,149 B in
  15 messages are not in the Markdown by that rule, all where the Markdown is made from HTML; where
  it is made from the text body, nothing is. docproc-mail's content rule is measured over the same
  `~/docs` as docproc-mail ships it: the docproc-envelope crate on Rust 1.100.0, with NFC, the
  dictionary-free UAX #29 word and sentence boundaries, and general categories from ICU4X 2.4.0 on
  Unicode 18.0.0, CommonMark read by pulldown-cmark 0.13.6, and the crate's own Myers alignment. A
  scratch build of the tool writes each comparison to stderr, comparing the Markdown htmd makes from
  the HTML before anything is appended. Of the 31 messages whose Markdown is made from HTML, 28 with
  a source text, the sentences the rule appends are 295 B in 7 messages, each a text body that says
  what its HTML doesn't: cpython's `msg_10.eml` (143 B, the encodings its text parts name) and
  `msg_15.eml` (18 B), the generated inline-image message as `.eml`, `.msg`, and in `mailbox.pst`
  (30 B each, "It is taped into the log book.", where the HTML shows the image), POI's
  `ASCII_UTF-8_CP1252_LCID1031_HTML.msg` (16 B), and Tika's `testMSG_chinese.msg` (28 B). Outlook
  2003's welcome message, once in Tika's `test-outlook2003.msg` and twice in libyal's `outlook.pst`,
  appends nothing: its stored text body holds U+0099 where its HTML holds `™` (the .msg's
  `PidTagBody` is `PT_UNICODE`, code unit 0x0099), a character the sending client mis-decoded, not
  one docproc-mail decodes, and as a control and a symbol both only separate words, so `SharePoint`
  matches. No decoder correction applies: PT_STRING8 is read in its message's code page, and a MIME
  part's charset label as the WHATWG Encoding Standard reads it (mail-parser maps `iso-8859-1` and
  `us-ascii` to windows-1252). The RTF's text, read as [`tool-contract.md`](tool-contract.md),
  "docproc-mail", states, compared the same way, lacks nothing where the Markdown is made from it (2
  messages); beside a stored HTML body (2 messages) it lacks 57 B, all in `testMSG_chinese.msg`: 28
  B, the line its text body also holds, and 29 B, a table's numbers 1 to 19, which the RTF's text
  runs together into one word, since nothing separates its cells. docproc-mail 0.1.4 appends none
  of these: Markdown made from HTML is the HTML's alone ([`tool-contract.md`](tool-contract.md),
  "docproc-mail").
- **onenote.** Alt text 9,684 B (all), run text 7,482 B (6,944), image `ocr_text` 5,572 B (4,926,
  every duplicated byte of it reaching the Markdown inside the same image's alt text), filenames
  3,240 B (2,865), run links 114 B (all, as link targets), tag labels 57 B (27), ink alternatives
  79 B (8). The run text missing, 538 B, is every title's date and time paragraphs; the
  `ocr_text` missing, 646 B, is OCR text no alt text holds, all of it in 6 images with no alt
  text; the tag labels missing, 30 B, are "To Do", which the spike writes only as a task box.
  No image carries a `hyperlink`. Media written over mail and onenote: 235 MB.
- **onenote's OCR text against the alt text.** docproc-onenote's content rule is measured over
  the same 17 sections, unchanged against `~/docs`' MANIFEST.json, as docproc-onenote ships it:
  onenote_parser 2.1.0, and the docproc-envelope crate on Rust 1.100.0, with NFC, the
  dictionary-free UAX #29 word and sentence boundaries, and general categories from ICU4X 2.4.0
  on Unicode 18.0.0, both texts read as plain text with no CommonMark reader, and the crate's own
  Myers alignment. A scratch build of the tool writes each image's comparison of its `ocr_text`
  with its `alt_text` to stderr. Of the 135 images holding OCR text (5,572 B), the sentences the
  rule appends are 566 B, whitespace collapsed, in those 6 images: interop's
  `FileSyncWOPI_OneNoteWithODB.one` (414 B in 40 sentences, the recognizer's reading of a scan as
  CJK characters) and five in `LargeData.one` (30 or 31 B each, a number and "This is a testing
  document."). The other 80 B of the 646 are the line breaks and spaces around those sentences
  and one sentence, `',`, that holds no word, which the rule never appends. Every other image's
  OCR text is in its alt text by the rule.

