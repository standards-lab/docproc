# Evaluation · processing, tool runners and readers (spike-ai-pipeline-processing v0.9.0)

The five packages are sound to promote. `tool` handles a process carefully: it always reaps, a
cancel reaches the whole process group, a held stdin can't hang a run, and its tests drive a
stand-in binary through every path. The readers stream through a pipe with backpressure and
stop their tool on an early break. The weaknesses are at the seams schema "2" and the move into
docproc open. The three most consequential findings: the readers' unit tier runs a released
binary of each tool and copies its fixtures under a manifest, which a contract change landing
with its decoder in one pull request can't satisfy (PT-1); a tool's stderr is kept as its first
1 MiB and read only after a successful run, so warnings, which schema "2" makes typed data, are
lost on a failed or resumed read and can push the error line out (PT-2); and the memory bound
`layers.md` now gives `tool` has no mechanism the exec runner can apply as written (PT-3). The
largest simplification is one envelope reader in place of three copies, which office's move to a
stream would otherwise make four (PT-4).

Evidence was read from the spike at tag v0.9.0. Four behaviours of the exec runner were run
against it from a scratch module (PT-3, PT-5, PT-8, PT-13), and the Threader's memory was
measured the same way (PT-12); every other finding is by reading. `layers.md` and `design.md`
were read as they stood in the working tree after round 3, with the onenote review settled and
the library list carrying `--file-only`, the `encrypted` mapping, the run's bound, and death by
signal. Paths are relative to the spike's root.

## Findings

### PT-1 · The readers' unit tier needs a released binary the one-pull-request rule can't supply
- **Kind:** issue
- **Weight:** high
- **Where:** mise.toml:7-15; email/real_test.go:103-130, 144-200; onenote/real_test.go:113-135;
  office/real_test.go:38-58; email/testdata/README.md; scripts/fixtures-mail.sh
- **Against:** docproc `context/README.md` ("a tool's contract changes with its decoder in one
  pull request"); tests-and-docs.md ("The unit tier: black-box and hermetic"); system-tools.md
  ("Golden outputs prove a contract")
- **Finding:** `go test -race ./...`, the check's unit tier, runs `TestReal*` in all three
  readers against the real tool, which mise installs from the tool's GitHub release, and fails
  with `t.Fatalf` when it is absent. Each reader also keeps a byte-for-byte copy of its tool's
  fixtures under `testdata/`: `email/testdata/mail` (5.0 MB, with mail's goldens) and
  `onenote/testdata/onenote` (2.3 MB), each pinned by a `MANIFEST` of hashes that `TestManifest`
  checks and `TestRealVersion` ties to the pinned release, and `office/testdata/ooxml` (580 KB,
  sixteen fixtures listed by hash in its README). In docproc the
  tool and the reader change in one pull request, so no release of the tool exists for the
  reader's check to pin, and the copies would duplicate `tools/<format>/` in the same
  repository. Building the tools for the Go job instead would put .NET 11 and a Rust musl
  target into it, and a NativeAOT publish can't cross-link.
- **Proposed design:** The tools' committed goldens are the readers' fixtures, read in place.
  Each reader's unit tier replays every `.jsonl` and `.stderr` under `tools/<format>/` through
  the stand-in tool and asserts three things: no record is an `Unknown*`, each record marshals
  back to its line (the comparison `checkLines` and `marshalled` already make), and each stderr
  line parses to a typed warning. The tool's check proves binary to golden and the reader's
  proves golden to records, so the chain needs no binary in the Go job, and a field the reader
  doesn't declare fails the round trip. The test fails, never skips, when the goldens' directory
  is missing. `email/testdata/mail`, `onenote/testdata/onenote`, `office/testdata/ooxml`, the
  two `MANIFEST` tests, `TestRealVersion`, the `fixtures:*` tasks and scripts, and the three
  mise pins go; office's legacy, RTF, and picture fixtures, which are processing's own, stay. One
  integration task runs the readers over the binaries the root check just built.
- **Carried by:** library

### PT-2 · stderr is kept as its first 1 MiB and read only when the run succeeds
- **Kind:** issue
- **Weight:** high
- **Where:** email/email.go:195, 247, 312, 346-348; onenote/onenote.go:166, 215;
  office/office.go:329, 336; internal/cli/cli.go:74-102, 107-118; dispatch/email.go:103-114;
  dispatch/onenote.go:83-96
- **Against:** layers.md, "Common envelope" → stderr, and "docproc's packages [library]"
  (`Item.Warning` and `Event.Warning`, typed); validation-first (the failure names the cause)
- **Finding:** Each reader gives its tool a `cli.LimitedBuffer` that keeps the first 1 MiB of
  stderr and discards the rest without a mark. The envelope writes the `error:` line last, so a
  run that wrote 1 MiB of warnings and then failed has an `ExitError` with an empty `Message`,
  and a run that succeeded reports about its first 10,000 warnings as if they were all. Schema
  "2" adds warnings per item (an unmatched image source, a TNEF part, an unread kind), so a large
  store reaches the limit. `tool` itself keeps the opposite end, the last 4 KiB
  (tool/tool.go:128). The warnings are parsed only after the tool exits, and dispatch reads
  `Stream.Warnings()` only after the records end without error: a read that fails, or that its
  context ends, yields its error and drops every warning the tool wrote. A store resumed with
  `After` never rewrites the earlier messages' warnings, so they are lost for good.
- **Proposed design:** The reader gives the tool a pipe for stderr and parses lines as they
  arrive, so memory follows a line, never the run. Warnings leave through the record sequence
  as they are read, and whatever is still queued is delivered before the read's final error, so
  a failed read keeps them. The run's message is the last `error:` line, held apart from the
  warnings. Order between a warning and a record is best effort, since they travel on two pipes,
  and the package comment says a warning read after its message's cursor was saved can be lost
  to a crash. `LimitedBuffer` stays for the tools read in part (soffice, gs).
- **Carried by:** library

### PT-3 · The run's memory bound has no mechanism the exec runner can apply as written
- **Kind:** issue
- **Weight:** high
- **Where:** tool/exec.go:98-121; tool/container.go:145-160; tool/tool.go:17-37
- **Against:** layers.md, "docproc's packages [library]" (`tool`: "bounds a run's memory");
  roadmap.toml, the library task's backlog; data-intelligence.md (the exec runner inside an
  image that bundles the tools)
- **Finding:** The change list gives `tool` the bound, while "Running the tools" still says
  `tool` is kept and shows a `Spec` with no field for it. The code shows why the mechanism has
  to be chosen. `os/exec` has no hook between fork and exec, and the library may not take
  `golang.org/x/sys`, so a resource limit is either a raw `prlimit64` on the started pid, beside
  the raw `waitid` already there (tool/exec.go:288-301), or a cgroup the host must delegate.
  `RLIMIT_AS` bounds address space, not resident memory, and a runtime that reserves address
  space ahead of use, as .NET's collector does, can fail at start under it; that has to be
  measured on docproc-ooxml before it is chosen. The container runner passes no `--memory`. In
  Cerebro's image the pod's own limit is the only bound today, and the kernel then picks its
  victim among Cerebro and its tools. Run from a scratch module, a tool killed by a signal
  reports `tool: bash: exit code -1`, which names no signal (PT-5).
- **Proposed design:** `Spec` gains `MemoryLimit int64`, bytes, zero for none, a value the
  application supplies. The container runner passes it as `--memory`. The exec runner does two
  things after `Start`: it writes 1000 to the child's `oom_score_adj`, so under any cgroup limit
  the tool is the victim, never the host process; and, when the limit is set, it applies
  `RLIMIT_AS` by `prlimit64`. The library task measures that limit against all three tools over
  ~/docs first; if docproc-ooxml can't start under it, its bound is the collector's own hard
  limit through an environment variable, which needs PT-8's `Env`. "Running the tools" shows the
  field.
- **Carried by:** library; acceptance (the measurement)

### PT-4 · Three copies of the line reader, with office's stream about to be the fourth
- **Kind:** simplification
- **Weight:** high
- **Where:** email/email.go:102-365; onenote/onenote.go:80-311; email/model.go:320-362;
  onenote/model.go:188-233; office/office.go:178-213; email/main_test.go; onenote/main_test.go;
  office/main_test.go; internal/cli/contract_test.go
- **Against:** none: simplification (baseline-standards.md, "A type is introduced only when it
  is needed"; tests-and-docs.md, a shared test helper is hoisted once two packages need it)
- **Finding:** With the tools' names normalised, `onenote/onenote.go`'s 195 code lines differ
  from `email/email.go`'s 236 in 95 diff lines, nearly all `After` and `setPaths`; the two
  `decodeLine` functions differ in 31, and the two test stand-ins (166 and 169 lines) in 33. Each
  package declares its own `ExitError`, four aliases of the shared exit errors, a `Schema`
  constant, a `Stream` with a stored context and a read-once guard, and an `ErrSchema` that is a
  distinct value in each (office/model.go:18, email/model.go:21, onenote/model.go:20), so
  `errors.Is(err, email.ErrSchema)` doesn't match onenote's though `ErrRefused` matches across
  all three. `internal/cli/contract_test.go` exists to prove the aliases agree. Schema "2" makes
  office a line stream too, and its common envelope is now one contract for all three tools.
- **Proposed design:** One envelope reader holds what the envelope states once: run the tool
  with stdout and stderr on pipes, check the first line's kind and `schema`, yield each line's
  kind and raw JSON, parse stderr (PT-2, PT-6), map exit codes 1 to 4 and a signal to one
  `ExitError`, and resolve a media name in the media directory after checking it against the
  envelope's name rule, which replaces three path checks (email/email.go:322,
  onenote/media.go:219, office/media.go:60). It also holds one `ErrSchema`, the four exit
  errors, and `Warning`. A reader keeps its request, its arguments, and its line types. The
  types other packages name (`ExitError`, the errors, `Warning`) can't be reached through
  `internal/cli`, and dispatch can't declare `Warning` for the readers to return, since it
  imports them; so the envelope is a public package below the readers, `internal/cli` keeping
  `Spool` and `LimitedBuffer`. `Read(ctx, req)` returns the sequence itself, as sqlate's
  `Rows.Each` does (`sqlate/query/rows.go:84`), so the three `Stream` types, their stored
  contexts, and their read-once guards go. One stand-in tool in an internal test package serves
  all three readers.
- **Carried by:** library

### PT-5 · Death by signal is a `tool` change, and the two runners report it differently
- **Kind:** issue
- **Weight:** medium
- **Where:** tool/tool.go:131-150; tool/exec.go:148-154; tool/container.go:87-89;
  internal/cli/cli.go:34-55
- **Against:** layers.md, "docproc's packages [library]" (`internal/cli`: "a tool that dies by
  signal … is failed, its reason naming the signal"); design.md ON-7, ON-16
- **Finding:** The change list puts this in `internal/cli`, but the signal is lost one layer
  down. `tool.ExitError` carries `Code: -1` and prints `exit code -1`; the signal is reachable
  only by unwrapping to `*exec.ExitError` and asserting its `Sys()` to `syscall.WaitStatus`.
  Through the container runner the same death is exit code 137 or 139 from the docker CLI, a
  plain code. `cli.Outcome` returns nil for both, so a reader returns an untyped error that
  matches none of the four exit errors, `ErrFailed` included. No test in `tool` covers a signal
  outside a cancel (tool/cancel_test.go:126 names one only for a cancelled run).
- **Proposed design:** `tool.ExitError` gains `Signal`, the signal's name, empty for an exit. The
  exec runner sets it from the wait status; the container runner from codes 129 to 159, the
  shell convention docker follows, with the code kept. `Error` prints `killed by SIGSEGV`. The
  envelope's `ExitError` matches `ErrFailed` when `Signal` is set and carries the name in its
  message. Tests: the stand-in kills itself, through both runners.
- **Carried by:** library

### PT-6 · The typed warning needs a parse the change list doesn't give it
- **Kind:** issue
- **Weight:** medium
- **Where:** internal/cli/cli.go:66-102; email/email.go:186-191, 363-365;
  onenote/onenote.go:157-162; dispatch/email.go:117-124
- **Against:** layers.md, "Common envelope" → stderr; "docproc's packages [library]" (`Warning
  {tool, where, what}` "parsed from the envelope's stderr line"; `internal/cli`: "the stderr
  parse … kept"); design.md ML-7, ON-14, ON-3
- **Finding:** `ReadStderr` returns each warning as one string, `<where>: <what>` unsplit, and
  on a run that succeeded takes every other line as a warning too. Three settled rules need more
  than the kept parse. (1) ON-14: the shared Rust writer escapes a colon-space in `<where>` and
  control characters in both parts, but neither `layers.md` nor `design.md` names the escape's
  form, and the reader must invert it to show a section's name. docproc-ooxml is C# and doesn't
  use that writer. (2) ML-7: a `.pst` place is `folder 32802, message 2097188, attachment 5`, and
  "docproc joins the id to the path the folder's line gave it"; the email entry of the library
  list doesn't carry the join, and the map it needs exists only in dispatch's walk
  (`emailWalk.folders`). (3) ON-3: a refusal's reason leads with the input path, which
  `ReadStderr` strips by exact prefix (cli.go:93); dispatch's `encrypted` test then depends on
  that strip, and the envelope's `error: <reason>` doesn't say whether a reason leads with the
  path, for any tool.
- **Proposed design:** The envelope states both rules for all three tools: the escape (a
  backslash before a backslash, `\n` and `\r` for line breaks, and `:\ ` for a colon-space in
  `<where>`), and that an error's reason never holds the input path, which docproc already
  knows, so goldens need no relative-path convention for refusals. The envelope reader splits a
  warning at its first three `": "`, unescapes, and returns `Warning{Tool, Where, What}`; a
  line that doesn't fit is a `Warning` with `What` alone. The email reader keeps each folder
  line's path by id as it streams, one entry per folder, and rewrites a place's `folder <id>` to
  the path. `Program.Labels` and the path argument go.
- **Carried by:** library (the parse, the join); mail, onenote, ooxml (the envelope's two rules)

### PT-7 · A `data:` image is media no line names
- **Kind:** issue
- **Weight:** medium
- **Where:** email/email.go:296-303, 322-333; dispatch/email.go:236-258
- **Against:** layers.md, "Common envelope" → Media ("A line names it in `file`"); "docproc-mail"
  ("A `data:` image is written as media and linked by its name, with no attachment record");
  design.md ML-6
- **Finding:** The reader gives a `Path` only to attachment records (`setPaths`), and dispatch
  makes a child part only from an attachment with a `Path`. A `data:` image has neither: the
  tool writes its file and links it in the Markdown, and nothing in the message line names it.
  The reader parses no Markdown, so the image is never a part, never reaches vision, and is
  never hashed, recorded, or removed with the part's files. docproc-ooxml's `document` and
  docproc-onenote's `page` list every payload in `media`; the message line alone has no list.
- **Proposed design:** A `message` line gains an optional `media`, the files of the payloads its
  Markdown links that no attachment record names, an addition under "2" that gives all three
  tools one rule: every file written is named by a line. The reader sets a path for each as it
  does for attachments, and dispatch makes each a child part. The alternative, the reader
  scanning the Markdown for media names, makes the reader a Markdown consumer and is rejected
  here.
- **Carried by:** mail (the field); library (the reader, the stage)

### PT-8 · The exec runner hands every tool the host's whole environment
- **Kind:** issue
- **Weight:** medium
- **Where:** tool/exec.go:96-121; tool/tool.go:50-78
- **Against:** the spike's STANDARDS.md ("its temporary files live in a directory the caller
  owns"); data-intelligence.md (the exec runner inside Cerebro's image); design.md OX-3
- **Finding:** `execRun` never sets `cmd.Env`, and `Invocation` and `Spec` have no field for one,
  so each tool inherits the process's environment. Run from a scratch module, a variable set in
  the parent was read by the tool, and the runner sets no `TMPDIR`. In Cerebro that environment
  holds the gateway's key and the database's credentials, and the tools include Ghostscript,
  ImageMagick, and LibreOffice reading files of unknown origin. The container runner already
  isolates this, since a container gets none of it. A tool's temporary files go wherever the
  inherited `TMPDIR` points, not into the caller's directory, and the locale that changed
  office's bytes (OX-3) is inherited the same way. A scrubbed environment is hygiene, not
  isolation: a tool under the same user can still read `/proc/<parent>/environ`.
- **Proposed design:** The exec runner starts each program with a fixed environment: `PATH`,
  `LANG=C.UTF-8`, `HOME` and `TMPDIR` set to the invocation's `Dir` when it has one, and the
  entries of a new `Spec.Env`. Nothing else is inherited. The package comment states the
  `/proc` limit and that real isolation is the image's, a separate user for the tools being
  `tool-packaging`'s call.
- **Carried by:** library; the capstone's `tool-packaging` (the user)

### PT-9 · The office stream leaves five things of `Result` unplaced
- **Kind:** issue
- **Weight:** medium
- **Where:** office/office.go:139-163, 243-365; office/pictures.go:25-95; office/media.go:11-74;
  office/libreoffice.go:77-93
- **Against:** layers.md, "Running the tools" (`Convert` returns a `Stream`; "the document, then
  each sheet and its rows") and "docproc-ooxml" (`properties`: "Keys keep their stored order")
- **Finding:** `Convert` runs the tool to its end into a `bytes.Buffer`, lists the media
  directory, converts the pictures, and rewrites the Markdown's links, in that order. As a
  stream, each step needs a place the signature doesn't give. (1) `ConvertedFrom` and
  `MediaDir` have no home once `Result` goes. (2) EMF, WMF, and TIFF conversion must finish
  before the `document` record is yielded, since its Markdown links the pictures, and for an
  xlsx the links are on each sheet's closing `rows` line, so the rewrite runs per fragment.
  (3) `listMedia` reads the directory and types each file by its extension through a 12-entry
  table, but the `document` line's `media` now gives `file`, `content_type`, and `kind`, and
  conversion must take `picture` entries only, now that embedded and OLE payloads share the
  directory. (4) `properties` are objects whose stored key order is contract; decoded into a Go
  map the order is lost, and json/v2 marshals a map in no fixed order. (5) A legacy document's
  conversion and its cleanup run inside `Convert` today; returned as a `Stream` never ranged
  over, the converted document and soffice's profile would stay.
- **Proposed design:** `Convert` validates the request and returns; LibreOffice, the tool, the
  picture batch, and every removal run inside the sequence, so nothing is left when it isn't
  ranged over. The first record is the `Document`, yielded after the picture batch, carrying
  `ConvertedFrom` and its `Media`, each with the unmarshalled `Path` and `Converted` fields
  `email.Attachment.Path` models. `rewriteLinks` runs over each record's Markdown. `MediaFile`,
  `ContentType`, its table, and `listMedia` go. `Properties` decodes to ordered pairs through
  an `UnmarshalJSONFrom` that reads the object's tokens in order.
- **Carried by:** library

### PT-10 · The readers route media the dispatcher routes again
- **Kind:** simplification
- **Weight:** medium
- **Where:** email/route.go:19-44; onenote/media.go:206-275; dispatch/email.go:236-258;
  dispatch/onenote.go:160-186; dispatch/dispatch.go:375
- **Against:** none: simplification (layers.md, "Inspection and routing": "The dispatcher runs
  both on every part before its stage")
- **Finding:** `Attachment.Route` and `Page.Media`'s routing call `route.File` on each written
  file. Dispatch then makes the file a found part and routes it itself with `route.File`
  (dispatch.go:375), passing neither reader's route. The readers' result is used only for the
  reason text of a medium with no content (dispatch/email.go:257, dispatch/onenote.go:179). One
  rule exists only in the reader and so never applies to a dispatched part: onenote's recording
  of no known signature taking its route from its `type` (onenote/media.go:239-257, with
  `signed`). The routing brings `route` into both readers and about 1,200 lines of tests
  (email/route_test.go, email/route_integration_test.go, onenote/media_test.go).
- **Proposed design:** The readers return what the tool wrote and where: each attachment and
  medium with its `Path`. `Attachment.Route`, `Medium.Route` and `Reason`, `signed`, and both
  packages' `route` import go, and the reason a medium has no content is stated from its record
  (no `file`, or no media directory). The recording rule moves to dispatch's onenote stage as
  the found part's stored type. With `media.go`'s walk already cut, `Medium` is the decoded
  `media` entry and its `Path`.
- **Carried by:** library

### PT-11 · A tool of another schema passes the startup check
- **Kind:** issue
- **Weight:** medium
- **Where:** office/office.go:21-35, 70-97; email/email.go:23-42, 62-73; onenote/onenote.go;
  tool/tool.go:17-37, 192-229; email/model.go:333-339
- **Against:** system-tools.md ("Startup fails, not the first request"); validation-first;
  layers.md, "Common envelope" and "Versioning"
- **Finding:** A `Spec` checks a minimum version only, and each reader's own re-check is the
  same minimum. The schema is checked on the first line of the first read. A docproc-mail of a
  later schema therefore starts cleanly in Cerebro and fails every job with `ErrSchema`. The
  envelope doesn't state `--version` at all, though every `Spec` parses its output with
  `^(\d+(?:\.\d+)*)` and the stage identities take `Tool.Version` from it. The specs still name
  `spike-ai-pipeline-*` programs and the spikes' minimums, and `MailAfterMinimum` with its
  refusal (email/email.go:26-29, 164-173) gates a flag every docproc-mail has.
- **Proposed design:** The envelope states `--version`: the version alone on the first line,
  then `schema <n>` on the second, exit 0. Each reader's spec pattern requires its schema's
  line, so a tool of another schema fails its constructor with a `VersionError`, with no new
  field in `tool`. The specs name `docproc-ooxml`, `docproc-mail`, and `docproc-onenote`, their
  minimums the first schema "2" tags, and `MailAfterMinimum` goes. The minimum re-check,
  written out at ten call sites of `tool.ParseVersion` across the module (four of them in these
  readers), becomes one method, `Tool.AtLeast(minimum string) error`.
- **Carried by:** ooxml, mail, onenote (`--version`); library

### PT-12 · The Threader holds six times the backlog's figure, and its saved form has no use left
- **Kind:** issue
- **Weight:** medium
- **Where:** email/threads.go:47-71, 88-114, 462-589; email/resume_test.go; README.md:1047;
  CHANGELOG.md:498
- **Against:** roadmap.toml, the library task's backlog ("about 60 bytes a message … about 60 MB
  past a million"); layers.md, "One job, and its cancellation" ("no Threader state joins the
  checkpoint")
- **Finding:** `threadKey` is 120 bytes on amd64 before any string it points to. Fed 200,000
  synthetic messages, each with a Message-ID, a conversation index, and two references, a
  Threader held 362 bytes a message, and `Threads` raised the heap to 435. The 60 bytes is near
  the saved form's size, 185 bytes a message in the same run, not the heap's. At a million
  messages that is about 360 MB, not 60 MB. `layers.md` settles that a consumer threads the
  stored records, so no Threader state is checkpointed: `MarshalBinary` and `UnmarshalBinary`
  (130 lines, with `resume_test.go`'s cases) serve a checkpoint that no longer exists. No
  package of either spike calls the Threader outside tests.
- **Proposed design:** `Threader`, `Threads`, and `Thread` stay for the consumer `layers.md`
  names. `MarshalBinary`, `UnmarshalBinary`, the magic, and `keyReader` go. The backlog comment
  and the README state the measured figure, and the library task re-measures it on the scale
  store. Spilling keys to disk stays backlog, sized by the real number.
- **Carried by:** library; roadmap (the comment)

### PT-13 · A tool's children outlive a run that exits normally, and the host's death
- **Kind:** issue
- **Weight:** low
- **Where:** tool/exec.go:88-91, 117-133; tool/doc.go:27-35
- **Against:** none stated: the runner's promise, "so no child outlives it", covers a cancelled
  run only (tool/exec.go:88-91)
- **Finding:** The group's SIGKILL is sent only when the context has ended
  (`awaitExit(…) == nil && ctx.Err() != nil`). Run from a scratch module, a program that exited
  0 leaving a background child returned nil with the child alive; one whose child held stdout
  returned `exec: WaitDelay expired before I/O complete` after `KillAfter`, the child still
  alive. Separately, `SysProcAttr` sets `Setpgid` alone, so tools started by the exec runner run
  on when the host process is killed, and since each leads its own group a terminal's interrupt
  doesn't reach them. The package comment states the equivalent limit for containers only. The
  three docproc tools start no children; soffice does.
- **Proposed design:** The group is killed whenever its leader has exited, whatever the context
  says, which is the same call without the second condition. `Pdeathsig: SIGKILL` is set, with
  the running goroutine locked to its thread for the run, as the signal follows the thread that
  started the child. Two stand-in tests cover a normal exit with a child and a child holding
  stdout.
- **Carried by:** library

### PT-14 · The mail and onenote readers work through the exec runner only, unsaid
- **Kind:** issue
- **Weight:** low
- **Where:** email/email.go:31-42, 250; onenote/onenote.go:218; tool/container.go:186-192;
  tool/doc.go:5-8
- **Against:** tool/doc.go ("an adapter written against Tool runs the same code whichever runner
  built it"); validation-first
- **Finding:** `MailSpec` and `OneNoteSpec` say "for either runner's constructor". Both readers
  run their tool with no `Dir` and absolute paths for the input and the media directory. The
  container runner mounts only the invocation's directory, the process's own when `Dir` is
  empty, so through it the tool sees neither path and refuses the input at the first read.
  Office works today because it spools into `Dir`; reading `Request.Path` in place, as
  `layers.md` settles, gives it the same limit.
- **Proposed design:** The three docproc tools are the organisation's own static binaries, with
  no license or weight to keep out of an image, so they run through the exec runner only.
  `NewReader` and `NewConverter` refuse a tool whose `Image()` reports a container, at
  construction, and the spec comments say so. LibreOffice keeps the container runner, which is
  why a legacy document is still copied into `Dir`.
- **Carried by:** library

### PT-15 · `NewScript` has no caller
- **Kind:** simplification
- **Weight:** low
- **Where:** tool/exec.go:42-76; tool/script_test.go; README.md:60
- **Against:** none: simplification (system-tools.md names the three forms)
- **Finding:** No package of processing or of the spike root calls `NewScript`; its only uses
  are its 152-line test and the README. No tool docproc runs is a script.
- **Proposed design:** It doesn't move. system-tools.md's "a script run through its
  interpreter" stays true of the contract, since a script with an interpreter line runs through
  `NewExec`, and the `architecture` task's page says so when it promotes the principle.
- **Carried by:** library; architecture (the page's wording)

### PT-16 · The reader swaps a message line for a `Conversation`
- **Kind:** simplification
- **Weight:** low
- **Where:** email/model.go:22-32, 113-185, 356-360; dispatch/email.go:248-254
- **Against:** baseline-standards.md ("A type is introduced only when it is needed")
- **Finding:** `decodeLine` returns a `*Conversation` in place of the `*Message` for two message
  classes, the one record type that doesn't mirror its line. It drops the message's other fields
  (headers, Message-ID, importance, the subject when a topic exists), and the Threader ignores
  it. With `Body` cut, `Conversation.HTML` goes, and what is left renames six message fields.
  Dispatch already calls `Message.Conversation()` itself for a nested message.
- **Proposed design:** The stream yields the `*Message` the tool wrote, always.
  `Message.Conversation()` stays as the view dispatch's stage calls, for top-level and nested
  messages alike, and `Conversation` leaves the `Record` union.
- **Carried by:** library

### PT-17 · A missing required tool is an error where the standard panics
- **Kind:** issue
- **Weight:** low
- **Where:** email/email.go:62-65; onenote/onenote.go (`NewReader`); office/office.go:70-73
- **Against:** construction-and-execution.md ("A missing required field panics by name");
  lifecycle-and-context.md, "The wiring rule"
- **Finding:** A nil `Config.Mail`, `Config.OneNote`, or `Config.Office` returns
  `errors.New("email: no mail tool")` and its like. A nil required field is a wiring defect, and
  the settled libraries panic with the field's name, as blobfs does
  (`blobfs/data/options.go:222`, `data: WriteFileRequest.DB is nil`). A version too old stays an
  error, since the installed tool is the environment's.
- **Proposed design:** Each constructor panics with `email: Config.Mail is nil` and its like.
- **Carried by:** library

### PT-18 · Decoded times are returned as parsed, and no test runs under another zone
- **Kind:** issue
- **Weight:** low
- **Where:** email/model.go:90-93, 144-145; onenote/model.go:89-90; email/main_test.go;
  onenote/main_test.go
- **Against:** utc-times.md ("A library that parses a time from another source converts it
  before returning it"; "Tests prove UTC under a non-UTC zone")
- **Finding:** The message and page times decode straight into `time.Time` fields and are never
  converted. The tools write `Z`, which parses to UTC, so the result is right today, but a line
  with an offset would come back in a fixed zone or `time.Local`. Neither package's `TestMain`
  sets `time.Local`.
- **Proposed design:** The readers convert each decoded time with `.UTC()` before the record
  is yielded, and each package's `TestMain` sets Europe/London with a summer instant among the
  stand-in's lines, compared with `==`.
- **Carried by:** library

### PT-19 · The findings tier is the spike's evidence
- **Kind:** simplification
- **Weight:** low
- **Where:** email/findings_test.go; scripts/findings-email.sh; scripts/findings-onenote.sh;
  mise.toml:46-48, 269-285; docs/findings/
- **Against:** design.md OX-12, ML-14, ON-13 (each tool's evidence machinery stays in its spike)
- **Finding:** `email/findings_test.go` is 857 lines behind a `findings` tag: it fetches plaso's
  store over HTTP, times the scale store, and scores threads and routes for evidence 16. The
  check lints the module four times, once per tag (`integration`, `bakeoff`, `findings`). The
  library list places only the findings tests that call a model, with docinfer; the ones that
  call none are unplaced.
- **Proposed design:** The `findings` and `bakeoff` tiers, their scripts and tasks, and
  `docs/findings/` stay in the spike at v0.9.0, as the three tools' do. docproc's check lints
  the unit tier and the `integration` tag, and the acceptance rerun over ~/docs is the
  measurement from then on.
- **Carried by:** library; acceptance

### PT-20 · The container runner's lockdown has no process or privilege bound
- **Kind:** issue
- **Weight:** low
- **Where:** tool/container.go:145-160
- **Against:** tool/doc.go ("Every run is locked down"); system-tools.md (a container runner
  keeps a heavy tool out of the service's image)
- **Finding:** `runArgs` passes `--network=none`, the caller's user, and `--cap-drop=ALL`. It
  passes no `--pids-limit` and no `--security-opt=no-new-privileges`, so a converter reading a
  hostile document can fork without bound, and a setuid binary in the image can still raise its
  privileges. Memory is PT-3's.
- **Proposed design:** `runArgs` adds `--security-opt=no-new-privileges` and a fixed
  `--pids-limit`, sized against soffice's and ffmpeg's thread counts in the integration tier.
- **Carried by:** library

## For the synthesis

- **Where the envelope lives decides dispatch's shapes.** PT-4's public envelope package would
  hold `Warning`, which the library list has dispatch declare. The readers sit below dispatch,
  so either dispatch uses the envelope's type or converts it; the dispatch group's evaluation
  should settle this with PT-4. The `encrypted` prefix test (dispatch/office.go:142, and the
  onenote stage's to come) belongs beside the envelope's `ExitError` as one function.
- **Routing has one owner after PT-10.** The recording rule that moves to dispatch's onenote
  stage, and the reason text for a medium with no content, are dispatch changes the library
  list doesn't name.
- **Media integrity is free at dispatch.** A media file is named by its SHA-256, and dispatch
  hashes each output before recording it. Comparing the two detects a file a killed tool left
  truncated, at no extra read; no reader checks it today.
- **No run has a time bound.** `tool` takes the caller's context and nothing in the library sets
  a deadline for one part (dispatch/doc.go:233 mentions only the stale claim's timeout). A tool
  that loops on a crafted input holds its part until the job ends. The bound is the
  application's value, passed to dispatch, beside PT-3's memory limit.
- **The stage identity drops a build's commit.** Every spec's pattern takes the dotted numbers
  alone, so a development build `0.1.0+6ed0d0e` and the release `0.1.0` share an identity, and
  the index replays one's entries for the other.
- **Every package builds on Linux only.** The tag is on each file of `office`, `email`, and
  `onenote`, though only `tool`'s exec files need it. A consumer that threads stored records
  with `email.Threader`, as `layers.md` has it, can't compile the package elsewhere. Types and
  the Threader in files that don't import `tool` would lift that; it needs the readers split
  from their types, which PT-4's envelope makes cheaper.
- **`layers.md` disagrees with itself on `tool`.** "Running the tools" says `tool` is kept and
  shows `Spec` without a bound; the change list says changed. PT-3, PT-5, and PT-8 add
  `Spec.MemoryLimit`, `Spec.Env`, and `ExitError.Signal`, and PT-15 removes `NewScript`.
- **Contract additions this evaluation asks of the tools:** `--version`'s second line (PT-11),
  the warning escape's form and an error reason without the input path (PT-6), and the message
  line's `media` (PT-7). Each reopens a settled envelope by addition and needs the architect's
  ruling before the tool tasks start.
