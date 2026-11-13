# Evaluation · processing, synthesis (spike-ai-pipeline-processing v0.9.0)

Taken whole, processing is sound to promote, and its four groups agree more than they differ:
their findings converge on a module whose packages are right and whose seams are not. The format
packages have exactly one caller, the root's `stages`, yet each is built as if hosts wired it
directly, so every cross-cutting rule (what a failure means, what a host may bound, what was not
read) is decided again per package, or not at all. The three most consequential cross-package
findings: a failed run is classified in five places by five rules that disagree at the signal,
the timeout, and the refusal, and the groups' proposals inherit the disagreement (PX-1); the
module's shape follows from one fact the groups each saw half of, that only `stages` calls the
format packages, which settles where the adapters, the envelope reader, and the build tags live
(PX-2); and every group adds a value the host must supply (limits, timeouts, memory, caps,
concurrency, inspection options) while `stages.Build(ctx, lookup, exclude)`, the one door
`layers.md` gives a host, takes none of them (PX-3).

Evidence was read from the spike at tag `v0.9.0` with a clean tree, the root at `6a631f1`
(`root:` paths), and mail v0.3.1 (`mail:`). The import graph is `go list`'s; counts are `grep`'s
over non-test files. Nothing in the spike was run or written. Group claims this note leans on
were checked in the code: the three opens of each part (PX-10), the `tool` errors' facts (PX-1),
mail's cursor matched by equality (decision 4), the stage table's all-or-nothing wiring (PX-12),
and the callers of each package (PX-2). No finding here or in the four groups proposes engaging
an external repository; PI-5's 7-Zip floor is a pin, and every gap is filled in docproc's code.

## Findings

### PX-1 · A failed run is classified in five places, and the proposals disagree where they meet
- **Kind:** issue
- **Weight:** high
- **Where:** tool/tool.go:131-150 (`ExitError`, `Code` -1 for a signal); tool/exec.go:144-153
  (a cancelled run is a `cancelError`, never an `ExitError`); internal/cli/cli.go:15-41 (codes 1
  to 4); office/office.go:171-178, email/email.go:107-114, onenote/onenote.go:92-99 (the
  aliases); dispatch/office.go:136-143 (`strings.Contains(exit.Message, "encrypted")`);
  dispatch/dispatch.go:600-612 (the outcome switch); dispatch/stage.go:66-76
- **Spans:** PT-3, PT-5, PT-13, PS-2, PS-5, PD-3, PD-10, PD-11; design.md ON-7, ON-16
- **Finding:** Whether a failure is the content's (the same on every run, so recorded) or the
  run's (retried) is decided separately by `tool` (exit, signal, cancel), `internal/cli` (codes
  1 to 4), each reader's `ExitError`, each stage adapter, and the dispatcher's switch. The four
  groups each propose a rule for their layer, and the rules collide. PS-2 records a unit's
  failure as a warning when the error is a `*tool.ExitError` and not when the run "was killed",
  but a run killed by a signal is a `*tool.ExitError` with `Code: -1` today, so the rule can't
  be applied as written. PS-5 makes a per-run timeout's expiry a recorded warning; PS-2 says a
  run ended with its context is unrecorded, and a timeout is a context. PD-11 records only a
  stage error matching `ErrRefused`; ON-16, settled, has a stack overflow's SIGSEGV "recorded
  failed", which under PD-11 is never recorded, so a `.pst` holding one deeply nested `.one` is
  read whole on every dispatch. PT-3's memory bound ends a tool by signal or by a failed
  allocation, and no group says which class that is. PD-10 removes the one place a flag decided
  an outcome, which leaves the stage's error as the only input, so the error's class has to be
  right.
- **Proposed design:** One classification, made once. `tool` reports the facts: `ExitError`
  gains `Signal` (PT-5) and `TimedOut`, and `Invocation` gains the run's own time bound so a
  bound's expiry is told from the caller's context ending without each of the two dozen run
  sites comparing contexts. `dispatch` declares the classes a stage may end with (`ErrEncrypted`,
  `ErrNoProcessor`, `ErrRefused`, and PD-3's `ErrTooLarge`). `stages` holds one unexported
  function over a run's error that every adapter calls, for a unit and for a part alike:
  - the content's, recorded (a unit's warning, or the part Failed with `ErrRefused`): the tool
    exited nonzero by its own code, the envelope's 3 and 4 included; the tool died by its own
    fault (SIGSEGV, SIGABRT, SIGBUS, SIGFPE, SIGILL), which covers ON-16 and an allocation
    refused under PT-3's limit; the run passed its own time bound; the tool's output didn't
    parse;
  - the run's, unrecorded and retried: the context ended; SIGKILL or SIGTERM docproc didn't
    send (the kernel's OOM kill, an operator); the program didn't start; the envelope's 1 (not
    written) and 2 (usage, docproc's own defect); any error on docproc's side of the pipe;
  - encrypted: only a stage's `ErrEncrypted` (PD-10), mapped from a refusal's reason by one
    function beside the envelope's `ExitError`, not a `Contains` per stage.
  PS-2's "part fails only when nothing of its content can be read" then reads: every unit's
  failure was the content's. The cost is stated once in `dispatch`'s doc: a recorded failure
  stays until its entry is dropped or ages out, so a host that raises a bound drops what the
  old bound failed. The acceptance run counts recorded failures by class over ~/docs.
- **Carried by:** library; acceptance (the count)

### PX-2 · The module's shape follows from one fact: only `stages` calls the format packages
- **Kind:** simplification
- **Weight:** high
- **Where:** root: internal/docproc/stages/stages.go:12-22 (the only non-test importer of
  `document`, `email`, `media`, `office`, `onenote`, `picture`, `sevenzip`, `tool`; the root
  otherwise imports `dispatch`, `route`, and `audio` alone); dispatch/{adapter,archive,…}.go;
  tool/exec.go:117-308 (the module's only `syscall` use); the 48 non-test files tagged
  `//go:build linux` in `office`, `email`, `onenote`, `document`, `picture`, `audio`, `media`,
  `dispatch`, and `sevenzip`; doc.go:1-4
- **Spans:** PD-16, PT-4, PT-10, PT-14, PS-14, PD-17, PI-14; the Linux notes in PT's and PS's
  synthesis sections
- **Finding:** Three groups each drew one consequence of the same fact. PD-16 moves the stage
  adapters out of `dispatch` because their one caller is `stages`. PT-4 wants a public envelope
  package because `dispatch` "imports the readers" and so can't declare `Warning` for them;
  after PD-16 it no longer does, and the constraint PT-4 reasoned from is gone. PT's and PS's
  synthesis notes ask to lift the Linux tag from the types a consumer decodes; the tag is on 48
  files outside `tool`, and one file in it needs it. Seen together: `dispatch` after PD-16 imports
  `route` and
  `inspect` only; the readers' run halves, the adapters, and the envelope have one in-module
  caller; and what another module reaches is `dispatch`, `route`, `inspect`, `stages.Build`,
  `rundir`, and the record types docinfer decodes (`audio.Seam`, the plans, the readers' line
  types, `email.Threader`).
- **Proposed design:** The module, each concept with one home:
  - `route`, `inspect`: as kept, portable; `route` owns extensions and the encrypted-package
    detector (PI-12), `inspect` the checks, `Options`, and the verdict (PX-6).
  - `dispatch`: the walk, the `Stage` contract and its error classes, the wire shapes
    (`Warning` stays here, as `layers.md` has it), `Limits`, `Content`, `Index`, and their file
    and memory forms; imports `route` and `inspect` only, no build tag. `dispatch/dispatchtest`
    is public (PD-17), since docinfer's `job` tests need the fixture stage.
  - `tool`: `Tool`, `Spec`, `Invocation`, the errors, and `AtLeast`, portable; the exec runner
    alone is tagged, with a stub constructor elsewhere that returns an error. Every other tag in
    the module goes, so a consumer threads mail or decodes a plan on any platform.
  - `internal/cli` holds the whole envelope reader PT-4 describes (the line reader, the stderr
    parse, the exit mapping, the media name rule, one `ErrSchema`) beside `Spool`,
    `LimitedBuffer`, and PS-13's `lineWriter`. It stays internal: no caller outside `stages`
    runs a reader, and it yields a warning as its parsed `where` and `what`, which `stages`
    makes a `dispatch.Warning` with the tool's name, so no shape is declared twice and no
    reader imports `dispatch`. `internal/tooltest` is the one stand-in harness (PS-14), internal
    because no other module's tests run a tool.
  - `office`, `email`, `onenote`: line types and a reader each, the reader exec-only (PT-14) and
    routing nothing (PT-10). `sevenzip`, `document`, `picture`, `audio`, `media`: adapters;
    `audio` and `media` also hold their plan records (PD-16).
  - `stages`: `Build`, its options (PX-3), the adapters, unexported, the failure classification
    (PX-1), the identities (PX-8), and the one function that turns a reader's media into found
    parts (PX-14). `rundir`, `rundir/schema`: as `layers.md` has them.
  - No root package (PI-14). `layers.md`'s package table and "Running the tools" change to
    this: `tool` changed, `internal/cli` grown, `dispatch` shrunk, `stages` grown.
- **Carried by:** library; architecture (layers.md's package table)

### PX-3 · Every group adds a value the host must supply, and `stages.Build` takes none
- **Kind:** issue
- **Weight:** high
- **Where:** root: internal/docproc/stages/stages.go:165 (`Build(ctx, lookup, exclude)`),
  256-345 (each stage built from a zero request); layers.md, "Dispatch" (`Build`'s signature);
  dispatch/dispatch.go:94-104 (`Limits`, none by default); tool/tool.go:17-37 (`Spec`)
- **Spans:** PI-1, PI-8, PI-15, PS-4, PS-5, PS-10, PS-17, PT-3, PT-8, PT-20, PD-3, PD-13, PD-14,
  PD-20; PT's and PS's synthesis notes on time bounds and output caps
- **Finding:** The four evaluations propose at least fourteen host-supplied values: the bomb
  rule and an entry bound (PI-1, PI-8, PI-15), page and window concurrency (PS-17), a pixel
  bound, an output cap, ImageMagick's limits, a frame's largest side (PS-4, PS-5), the window
  limit, SVG density, and DPI (PS-10), a run's time bound (PS-5, PT), its memory, environment,
  and process count (PT-3, PT-8, PT-20), and the inspection options given once (PD-20). Each
  group places its values on its own package's request or `Spec`. But a host never builds those:
  `layers.md` gives it `stages.Build(ctx, lookup, exclude)`, which constructs every `Spec` from
  the package's own function and every stage from a zero request (root: stages.go:269, 328-345).
  As settled, none of the proposed values can be set by Cerebro or `cmd/docproc`. Two proposals
  also lean on a default that is absent: PD-13 bounds an archive's sibling set by
  `Limits.Parts`, and PI-1 names `Limits.Bytes` "the only absolute bound", while both are zero
  unless the host sets them.
- **Proposed design:** `Build(ctx, lookup, opts Options) (Set, error)`, `Options` one struct
  whose zero value is today's wiring, the error only for options it refuses
  (`inspect.Options.Validate`, a request's own check), so validation happens once at the
  boundary. It carries `Exclude`; the inspection options, which `Build` validates and hands to
  the archive and document stages as the bomb rule and returns in `Set` for
  `dispatch.Config.Inspect`, so one value exists; the run bounds `Build` applies to every
  `Spec` it builds (a unit run's timeout, a stream run's idle bound, the memory limit, extra
  environment); `Concurrency`; and each stage's request (`document`, `picture`, `audio`, the
  video plan). The library keeps shipping no policy: each default is none or the format's own,
  `cmd/docproc`'s configuration carries the operator's starting values, and Cerebro's carries
  its own. `layers.md` states each bound once, with its owner and what passing it does:
  - per dispatch (parts, bytes, depth): `dispatch.Limits`; the part is Limited, unrecorded;
  - per archive (bomb ratio and size, per entry and summed; entries listed): `inspect.Options`;
    a flag, and in the archive and document stages' identities;
  - per part's outputs (pages, frames, pixels, a frame's side): the stage's request, in its
    identity; past it the part is prepared with what it has and a warning, recorded, so a
    5,000-page PDF capped at 1,000 isn't rendered again by every dispatch;
  - per run (time, memory, processes): `tool`; PX-1 decides the outcome; in no identity;
  - per process (puts in flight, concurrency, the release timeout): `dispatch.Config` and
    `Options`; no outcome.
  The time bound is two values because the runs are two kinds: a unit run (a page, a picture, a
  form read) takes a deadline, and a stream run (a store, a decode, a whole archive) can't, so
  it is ended when it writes nothing for the idle bound.
- **Carried by:** library; docproc (`cmd/docproc`'s configuration); architecture (layers.md's
  `Build`)

### PX-4 · "The part held something that wasn't read" has seven spellings
- **Kind:** simplification
- **Weight:** medium
- **Where:** dispatch/archive.go:213-229 (`unwritten`: a record whose Meta holds `check`,
  `class`, `evidence`, and `outcome` as strings); dispatch/zip.go:66-76, dispatch/sevenzip.go:42-53,
  dispatch/tar.go:101-186 (three entry record types); dispatch/email.go:162, 257-261;
  dispatch/onenote.go:131, 177-183 (`skipped` lines and media with no content as records with a
  `reason`); dispatch/office.go:107-109, dispatch/text.go:176-181 (`Meta["warnings"]`);
  dispatch/email.go:111, dispatch/onenote.go:93 (a `warnings` record); dispatch/stage.go:78-99
  (`QuarantineError`); dispatch/index.go (`Child`)
- **Spans:** PI-7, PT-10, PS-2, PD-19; PI's synthesis note on checks raised as strings;
  layers.md, "docproc-onenote" (a `skipped` line's `encrypted: true`)
- **Finding:** A consumer that asks what a job didn't read must look in seven places: the
  part's flags; an archive entry's record, whose Meta spells an outcome (`quarantined`,
  `encrypted`) and an `inspect` check as strings; a tool's `skipped` line, passed through as a
  record; an attachment's or medium's record with a `reason`; a `warnings` record; a
  `Meta["warnings"]` string; and a child part's outcome. The groups each convert one spelling:
  PD-19 and PS-2 move warnings to `Item.Warning`, PI-7 keeps "the entry recorded with its check
  as the stage already does", PT-10 has the reason "stated from its record". Three consequences
  are missed. An entry past the bomb rule is counted as an output, not as a quarantined part, in
  `Entry.Outputs`, `job.Done.Parts`, and Cerebro's manifest. A section a `.onepkg` holds
  encrypted, which the tool reports on a `skipped` line, has no path to the Encrypted outcome
  `layers.md` gives a lone section. And a policy can't weigh a check that exists only as a Meta
  string.
- **Proposed design:** Three typed forms, and nothing else. A flag says something about the
  part's safety (PX-6). A warning says a unit of the part read badly or not at all, and has no
  identity of its own: a page, a window, a form, a tool's `skipped` item with no content to
  name. A child says an occurrence was found, and its outcome says what became of it, whether
  or not it had a file: `Found` may carry no path and instead an outcome (quarantined,
  encrypted, no processor yet, failed), a reason, and flags. The dispatcher yields that child's
  own event and records it as `Child{Part, Outcome}`, which `layers.md` already replays "as
  recorded" for those outcomes; its `Part` has a name and `Where`, and no SHA-256 or size. The
  archive stage's unwritten entries (encrypted, past the bomb rule, a link or device under
  PI-7), a reader's medium or attachment with no content, and a `skipped` line that names a
  section or message all become such children. The three entry record types, `unwritten`, the
  `medium` record, the `warnings` records, and both `Meta["warnings"]` go; counts by outcome
  become true.
- **Carried by:** library; architecture (layers.md's `Found` and "Dispatch")

### PX-5 · Two groups order a store's warnings differently, and only one order survives a resume
- **Kind:** issue
- **Weight:** medium
- **Where:** email/email.go:346-348; internal/cli/cli.go:74-102; dispatch/email.go:103-114;
  dispatch/onenote.go:83-96; layers.md, "Dispatch" (the replay's skipped prefix)
- **Spans:** PT-2, PT-6, PD-19, PD-2
- **Finding:** PT-2 has warnings leave "through the record sequence as they are read", near the
  record that caused them. PD-19 has `layers.md` state that "a tool's warnings follow its
  outputs", after the last record, and then has to state an inconsistency it creates: a resumed
  preparation carries only its tail's warnings while a resumed replay carries them all. The
  replay skips the log's prefix through the cursor's item (PD-2), so warnings logged where they
  were read are skipped with their messages, and the two resumes agree. Warnings at the end of
  the log are never skipped by a cursor and are lost whole when a read fails. The proposals
  conflict, and PT-2's is the one consistent with PD-2.
- **Proposed design:** PT-2's order. A warning is appended to the log where the reader yields
  it; `layers.md` says the order between a warning and a record is best effort (two pipes), that
  a warning never precedes its store's first record, and that a resumed replay and a resumed
  preparation both carry the warnings read after the cursor. PD-19's two sentences are dropped.
  PD-22's golden of a log uses the fixture stage, whose order is fixed, so the best-effort order
  pins no bytes.
- **Carried by:** library

### PX-6 · Flags get four partial designs; one covers them
- **Kind:** simplification
- **Weight:** medium
- **Where:** inspect/inspect.go:236-256 (`check`), 375-455 (name checks);
  dispatch/dispatch.go:415-425
  (`RecordFlags`), 608, 647-652 (`encrypted(p.Flags)`); dispatch/archive.go:165-183 (`bombRule`);
  dispatch/stage.go:78-99 (a stage can add flags only by ending the part quarantined);
  dispatch/document.go:265
- **Spans:** PI-4, PI-8, PI-13, PD-6, PD-10, PD-20; PI's synthesis notes on PDF structure and
  three owners of the options
- **Finding:** PI-4 splits flags by what they are a function of (content, name). PD-6 moves
  them from the index's own table into the entry. PI-8 and PD-20 are one finding, the options
  validated and defaulted in three places, with two different method sets proposed. PI-13 wants
  the document stage to raise PDF flags from qpdf's view, which no channel allows: a stage adds
  flags only through `QuarantineError`, which ends it. Each design is sound alone, and they
  interlock: PD-6's entry can hold only PI-4's content flags, since the entry is keyed by
  content; PD-6's verdict over recorded flags needs PI-4's name flags computed per occurrence;
  and PI-13's flags need a place in the entry PD-6 creates.
- **Proposed design:** `inspect.Options` gets three methods and no others: `Validate() error`,
  `Bomb() BombRule` (PI-8's, which the archive and document stages hash and apply), and
  `Verdict(flags []Flag) Verdict` (PD-6's). PD-20's `Check() (Options, error)` is dropped as
  the same thing twice. Content flags are recorded in the part's `Entry`; name flags come from
  `inspect.Name(name, kind)` at every occurrence; the verdict is taken over both under the
  current options, for a part seen or not. A stage raises a flag about its own part by yielding
  it: `Item` gains `Flag`, one more of its one-of. A flag yielded before the part's first
  output is weighed, and a quarantine verdict stops the stage, which is what `QuarantineError`
  does today, so `QuarantineError` and `ErrQuarantined` go; a flag yielded later is recorded
  and reported on the part's own event and never unwinds events already out. The document
  stage yields qpdf's findings this way, and the archive stage its listing's. `pdf-encrypt`
  stays a flag and decides no outcome (PD-10).
- **Carried by:** library; architecture (layers.md's `Index`, `Item`, and the open question)

### PX-7 · Untrusted text reaches the wire by six fields, and two groups each guard one
- **Kind:** issue
- **Weight:** medium
- **Where:** inspect/zip.go:146 (evidence); dispatch/zip.go:87 (`Where`, `Found.Name`);
  dispatch/dispatch.go:608-612 (`Reason = stageErr.Error()`) with tool/tool.go:142-148 (the
  last 4 KiB of a tool's stderr, raw, inside that error); dispatch/email.go:261
  (`Meta["filename"]`); dispatch/stage.go:188 (records marshalled by `encoding/json/v2`, the
  index by v1)
- **Spans:** PI-6, PD-12; PD's synthesis note on the two JSON packages
- **Finding:** PI-6 bounds and cleans a flag's evidence inside `inspect`; PD-12 normalises
  `Where` inside the dispatcher. The same bytes reach an event, an index row, and Cerebro's
  stream by four more fields neither covers: `Part.Name` (a stored name, which PD-12 mentions
  but only for UTF-8), `Event.Reason` and `Entry.Reason` (a failed part's reason embeds up to
  4 KiB of the tool's stderr, which for gs, magick, and 7zz quotes file content and stored
  names), `Warning.Where` and `What` (a tool's line, unescaped by PT-6's rule into arbitrary
  text), and `Output.Meta` values (a filename the tool decoded, which may hold U+0000, valid in
  JSON and refused by PostgreSQL's `jsonb`). Each is an index failure for `docproc/postgres`,
  which stops the dispatch, and the dispatcher treats an index failure as fatal.
- **Proposed design:** The guard is at the one exit. Before an event is yielded or an item
  appended, the dispatcher holds every string of the wire shapes to one rule: valid UTF-8, no
  C0 or C1 control but tab and line feed in a reason, no U+0000 anywhere, and a stated cap per
  field (PD-12's for `Where`, PI-6's for evidence, one for a name, a reason, a warning's parts,
  a Meta value). `Where` is normalised before the uniqueness rule, as PD-12 has it. `inspect`
  keeps quoting names in evidence for the reader's sake, and its guarantee becomes the
  dispatcher's. One test dispatches a fixture stage that yields hostile bytes in every field
  and marshals each event and log item with the package the wire uses. That package is
  `encoding/json` for `dispatch`'s shapes and the index, as the settled workspace libraries
  use; a record output is the bytes its stage made.
- **Carried by:** library

### PX-8 · A stage's identity is assembled by hand from fields its request doesn't own
- **Kind:** issue
- **Weight:** medium
- **Where:** dispatch/adapter.go:16-49 (`Versions`, `adapterIdentity`, the caller's `Version`
  string); dispatch/document.go:127-135 (the literal 150, five experiment fields);
  dispatch/audio.go:132-137 (four of the request's fields); dispatch/office.go:60-63;
  dispatch/video.go:160; root: stages.go:256-345 (each stage given `dispatch.Versions(…)` of the
  tools it was also given)
- **Spans:** PS-10, PS-11, PD-8, PD-20; PS's and PT's synthesis notes on identities
- **Finding:** Each adapter picks the fields it believes change its output and restates the
  package's defaults to hash them, and the version is a string its caller passes beside the
  tools themselves. PS-11's omitted fields, PS-10's literal 150, and PD-20's thresholds denied
  by `layers.md` are three instances; PT's note that a build's commit is dropped from the
  version is a fourth; PX-3's bounds would be a fifth, each new field one more place to forget.
  Nothing ties an identity to what the stage obeys.
- **Proposed design:** An identity is derived, never assembled. Each format package's request
  has one method that checks it and returns it with every default filled, and `stages` hashes
  that value whole with the adapter version; a field that doesn't change the output (a
  directory, a path, concurrency) is tagged out of the JSON in the request itself, where the
  field is declared. The version is computed from the tools the stage holds, in a fixed order,
  from each tool's whole version line, so a development build and a release differ. The schema
  a reader decodes and the bomb rule are fields of what is hashed. `dispatch.New` refuses two
  stages of one name and different identities (PD-8). `layers.md`'s sentence on what is in an
  identity becomes: the resolved request, the tools' versions, and the adapter's own.
- **Carried by:** library

### PX-9 · The four groups' test findings are one design, and two of its parts conflict
- **Kind:** simplification
- **Weight:** medium
- **Where:** mise.toml:7-48; inspect/clean_test.go:20; route/real_test.go:33, 122;
  email/testdata/mail, onenote/testdata/onenote, office/testdata/ooxml (7.9 MB of copies);
  eleven `main_test.go` stand-ins; dispatch/tier_integration_test.go:18-34;
  dispatch/index_test.go:116-352; document/vision_test.go
- **Spans:** PI-9, PI-16, PT-1, PT-4, PT-19, PS-8, PS-14, PD-17, PD-18, PD-22; the fixture notes
  in PI's and PD's synthesis sections
- **Finding:** Read together, the ten findings say the same four things, each from one package:
  the unit tier needs real tools and other packages' files; fixtures cross package and
  repository lines by copy; the stand-in harness is rebuilt per package; and the findings and
  bake-off tiers are the spike's evidence. Two details conflict. PS-14 places the stand-in in a
  public `tool/tooltest`, PT-4 in an internal test package. PI-9 moves the clean-fixture sweep
  to "dispatch's or `stages`'", which after PD-16 has one answer.
- **Proposed design:** Four tiers for the whole module, stated once in STANDARDS.md. Unit: every
  package over its own `testdata` and the stand-in from `internal/tooltest`; the readers also
  replay `tools/<format>/`'s committed goldens in place (PT-1), which is the one sanctioned
  read outside a package's directory. Integration (`-tags integration`): the real tools, and
  one whole-fixture test in `stages` that holds every fixture's route and outcome (PD-18's
  split of `TestTierRoutes`, and PI-9's sweep). Conformance: `dispatch/dispatchtest`, public,
  run by the base module and both providers (PD-17). Golden: the dispatch shapes as JSON Lines
  (PD-22) and the tools' own. The `findings` and `bakeoff` tiers, `corpustest`, `visiontest`,
  `audiotest`, `textscore`'s speech half, and the vision harness stay in the spike at v0.9.0;
  ~/docs is read only by the acceptance task, named by a variable. `layers.md`'s row for
  `internal/goldentest, mediatest, textscore, corpustest` changes to match.
- **Carried by:** library; acceptance; architecture (layers.md's helpers row)

### PX-10 · Format packages work out again what the part already carries
- **Kind:** simplification
- **Weight:** low
- **Where:** dispatch/dispatch.go:368-378 (`hashFile` returns the head, then `route.File`
  opens the file again); route/route.go:173-179, 255-322 (three more opens);
  inspect/inspect.go:304-322 (open, head, `Identify` a third time); picture/convert.go:250-267;
  document/prepare.go:100-119; email/route.go:19-44; dispatch/stage.go:140-146 (a found part's
  `SHA256` "which it never trusts the stage for")
- **Spans:** PS-9, PS-15, PT-10, PI-12, PD-3; PI's synthesis note on three reads; PT's on media
  integrity
- **Finding:** By the time a stage runs, the dispatcher holds the part's path, size, SHA-256,
  kind, and route. `route.File` and `Inspect` each reopen the file and identify its head again;
  `picture` identifies and hashes it a third time; `document` copies it; the readers route
  their media before the dispatcher routes it; and an output that is the part's own file is
  hashed again. Five findings each remove one instance.
- **Proposed design:** One rule: a package below `dispatch` takes what the dispatcher knows as
  input and derives none of it. `route.File` and `inspect.Inspect` keep their signatures for a
  caller that triages a path, as thin wrappers over forms that take the open file, its size,
  and its kind, which the dispatcher calls with the file it hashed. `picture.Request` and the
  stage requests take the kind and a path. A found part whose name is a SHA-256 (the envelope's
  media rule) is checked against the hash the dispatcher takes anyway; a mismatch is a file a
  killed tool left short, and fails that child as the run's fault (PX-1).
- **Carried by:** library

### PX-11 · `Output.Meta` is a second wire vocabulary nobody declares
- **Kind:** issue
- **Weight:** medium
- **Where:** dispatch/audio.go:164, 229; dispatch/video.go:238-261 (`window`, `count`, `start`,
  `end`, `cut`, `samples`, `seam_before`, `seam_after`, `frame`, `time`, numbers as strings);
  dispatch/email.go:209-222 (`"markdown": "follows"`, a protocol signal); dispatch/document.go
  (`page`, `count`, `fields`); root: internal/docinfer/infer/sources.go:174-190 (`strconv.Atoi`
  of `meta["window"]` and `meta["count"]`, an error when either doesn't parse)
- **Spans:** PD-22, PS-10; layers.md, "Dispatch" (`Item.After`, `Meta` under `sheet`)
- **Finding:** `Meta` is `map[string]string`, and about thirty keys are written across ten
  stage files as string literals. The consumer parses them: the root's docinfer reads a
  window's index and count back with `Atoi` and fails the run when they don't parse. So the
  keys and their number formats are contract between `stages` and docinfer, versioned by
  `dispatch.Version`, with no declaration, no golden (PD-22's fixture stage writes none of
  them), and facts carried twice: a window's span and seams are typed in the plan record and
  stringified in its Meta. One key is a control signal `Item.After` now replaces.
- **Proposed design:** The keys a consumer reads are exported constants in the package whose
  stage writes them (`audio`, `media`, `document`, `picture`, the readers), each documented
  with its format, and the whole-fixture test (PX-9) holds every key a stage wrote to that
  set. `"markdown": "follows"`, `outcome`, `check`, `class`, `evidence`, and `warnings` leave
  Meta for the typed fields PX-4 and `layers.md` give them. A window's typed facts stay in the
  plan record alone, and its Meta keeps the index docinfer joins on. Whether Meta becomes typed
  per kind is the capstone's `metadata` task's; this only makes today's contract explicit.
- **Carried by:** library; the docinfer goal (it reads the constants)

### PX-12 · Every adapter supports tool sets no wiring builds
- **Kind:** simplification
- **Weight:** low
- **Where:** root: internal/docproc/stages/stages.go:110-126 ("a stage either runs as the table
  names it or not at all"); dispatch/archive.go:193, dispatch/zip.go:98 (`sevenZip == nil`);
  dispatch/document.go:108-135 (`QPDF`, `PDFDetach` optional, `qpdf` in the identity);
  office/office.go:215-271, office/pictures.go:47 (`ErrNoLibreOffice`, no magick);
  picture/convert.go:52-57, 341-343; dispatch/picture.go:65; dispatch/office.go:140
- **Spans:** PD-10, PD-16, PS-16
- **Finding:** The stage table requires every tool a route names and leaves the route out
  otherwise. The adapters and three format packages still carry the partial modes: an archive
  stage without 7-Zip, a document stage without qpdf or pdfdetach, office without LibreOffice
  or magick, picture without either. Each mode has its error value, its branch, its identity
  field, and its tests, and none can occur. PD-10 proposes more code for one of them, mapping
  Ghostscript's password refusal for a document stage wired without qpdf.
- **Proposed design:** A stage has its tools or doesn't exist. With the adapters unexported in
  `stages` (PX-2), each takes its tools as required fields; `ErrNoLibreOffice` (twice),
  `ErrNoMagick`, `ErrNoSevenZip`, their branches, the `qpdf` identity field, and PD-10's
  Ghostscript mapping go. `picture.ErrNoReader` stays: what a given `magick` build reads is a
  fact of the host found at construction. `Build` still reports a route missing; a host that
  requires a route reads `Set.Rows` and fails its own startup.
- **Carried by:** library

### PX-13 · The LibreOffice runner is written twice
- **Kind:** simplification
- **Weight:** low
- **Where:** office/libreoffice.go:95-165; picture/metafile.go:89-171 (`soffice`,
  `sofficeError`, `sofficeMessage`, the same signatures; the bodies differ by the package
  prefix in three messages and picture's profile registry); office/libreoffice.go:22 and
  picture/convert.go:57 (two `ErrNoLibreOffice`)
- **Spans:** PS-13, PT-9
- **Finding:** `office` converts a legacy document and `picture` a metafile through the same
  private function, copied: a profile directory per run, the output directory, the message
  scraped from soffice's output, the cleanup. `office` already imports `picture` for
  `ConvertBatch` and `LibreOfficeSpec`. PS-13 lists five duplicated helpers and not this one,
  the largest; PT-9 reorders office's use of it without noting the copy.
- **Proposed design:** One runner, in `picture`, which owns the spec and the image: an
  exported method on its converter that converts inputs to a target with an optional profile
  registry, which `office` calls for a legacy document. PX-3's bounds and PX-1's
  classification then apply to soffice in one place.
- **Carried by:** library

### PX-14 · Three readers turn media into parts by three rules
- **Kind:** simplification
- **Weight:** low
- **Where:** dispatch/office.go:112-128; dispatch/email.go:234-261; dispatch/onenote.go:160-186;
  email/email.go:322, onenote/media.go:219, office/media.go:60 (three path checks)
- **Spans:** PT-7, PT-9, PT-10; PS's synthesis note on office's pictures; layers.md,
  "docproc's packages [library]" (onenote's `Where`)
- **Finding:** Under schema "2" each tool lists what it wrote in `media` (PT-7 completes this
  for a message), with `file`, `content_type`, and `kind`. The three adapters still differ in
  how a listed medium becomes a part: office dedups by path and renames a converted picture's
  `Where`, mail yields attachments by number and embedded messages by class, onenote looks for
  a released file by name. `Where` is spelled `media <name>`, `<message>, attachment <n>`, and
  `page <id>, <outline path>`. PS's concern that office's pictures would each cost a soffice
  run doesn't arise: they are already found parts, converted in office's batch first.
- **Proposed design:** One function in `stages` takes a line's `media`, the media directory,
  and the line's own `Where`, checks each name by the envelope's rule, and yields each as a
  found part (or an unread child, PX-4) with `Where` as `<line>, media <n>`, n its index in the
  list, which `layers.md` already settles for onenote and which is unique by construction
  (PD-13). An attachment keeps `attachment <n>`, since its record is its own. Office's
  converted pictures pass through the same function after the batch.
- **Carried by:** library

### PX-15 · The archive vocabulary is spelled three times, and its check names freeze at "1"
- **Kind:** simplification
- **Weight:** medium
- **Where:** route/identify.go (`SevenZip`, `MSCF`, `Zstd`, `ZIP`); sevenzip/listing.go:15-27
  (`SevenZ`, `Cabinet`, `Zstd`, `ZIP`); inspect/listing.go:30-42 (`Listed`, `EntryType`,
  `listingChecks`); dispatch/sevenzip.go:14-36, 55-68, 107-111 (the joining table and two
  conversions); inspect/inspect.go:42-163 (24 check constants for seven rules)
- **Spans:** PI-12, PI-14, PI-19; PI's synthesis notes "One vocabulary, three spellings" and
  "Checks are format times rule"
- **Finding:** PI left both questions to this pass. On the first: `sevenzip.Format` and
  `route.Kind` name the same four formats, and `inspect.Listed` mirrors `sevenzip.Entry`, with
  a table and two converters in the archive stage to join them. `sevenzip`'s listing types
  build on the standard library alone. On the second: a check's name is a policy key and wire
  vocabulary once `dispatch.Version` "1" ships, and each rule exists once per format
  (`zip-symlink`, `tar-symlink`, `7z-symlink`). PI-19's xz and bzip2, and any later format,
  add a constant per rule, and a policy that means "quarantine archives with links" must name
  every format, today's and tomorrow's.
- **Proposed design:** `sevenzip` reads a `route.Kind` (it may import `route`, which is on
  the standard library alone, as `inspect` does), so `Format` and the joining table go.
  `inspect.CheckListing` keeps its signature;
  `Listed` stays `inspect`'s, since `inspect` runs no program and shouldn't import an
  adapter's package, and the one converter is `stages`'. Check names become the rule alone
  (`archive-traversal`, `archive-absolute-path`, `archive-symlink`, `archive-hardlink`,
  `archive-device`, `archive-bomb`, `archive-encrypted`), the format already on the part as
  `ContentType.Kind` and in the evidence. Seven constants replace 24, a new format adds none,
  and the rename costs nothing now and a version later.
- **Carried by:** library

### PX-16 · Conventions the groups each ask for once per package
- **Kind:** simplification
- **Weight:** low
- **Where:** eleven `<Tool>Spec` functions and their `<Tool>Minimum` constants; the
  `tool.ParseVersion` re-checks;
  `ErrRequest` declared in `email`, `onenote`, `audio`, and `sevenzip`, beside `ErrOptions`,
  `ErrDPI`, `ErrScale`, `ErrRegion`; every tool-taking constructor returning an error for a nil
  tool; three one-field `Config` structs, four positional constructors; media/run.go:723,
  dispatch/file.go:260 (local times);
  inspect/doc.go, route/doc.go (each rule stated three times)
- **Spans:** PI-14, PI-17, PT-11, PT-17, PT-18, PS-12, PS-13, PS-16, PD-21; PI's and PD's
  synthesis notes on adapters and constructors; the goal's decision leaving constructors to
  `library`
- **Finding:** Nine findings ask for the same six rules, each for the packages its group read.
  Made per package they would be made nine times, and the goal's record already leaves one of
  them, constructors, to be made once.
- **Proposed design:** Six lines in STANDARDS.md, applied across the module in one pass. A
  constructor takes a `Config` struct, panics by name on a nil required field
  (`document: Config.Ghostscript is nil`), and returns an error only for the environment's
  fault, a tool too old (construction-and-execution; blobfs). An adapter declares
  `Spec() tool.Spec` and `Minimum`, unprefixed where the package has one tool and named for
  the tool where it has several, and checks versions through `Tool.AtLeast` alone. A request a
  package refuses matches that package's one `ErrRequest`. Every time returned or written is
  UTC, and each package's `TestMain` sets a non-UTC zone (utc-times). A contract is stated once,
  on its symbol; `doc.go` inventories. The module builds on every platform, and only running a
  tool needs Linux (PX-2).
- **Carried by:** library; docproc (STANDARDS.md)

## Merges and conflicts

- PI-8, PD-20 → one finding; merged into PX-6. Conflict on the method set: PI-8's `Validate`
  and `Bomb` with PD-6's `Verdict`, not PD-20's `Check`, which returns what the other two give.
- PI-4, PD-6 → merged into PX-6. Compatible only together: the entry holds content flags, the
  name flags are computed per occurrence.
- PI-13 (flags from qpdf), PD-10 → PX-6 supplies the channel PI-13 assumed (`Item.Flag`);
  PD-10's Ghostscript mapping is superseded by PX-12.
- PT-5, PS-2, PS-5 (the timeout's outcome), PD-11, PD-3 (the put's failure class), PT-3 (what
  the memory bound's death is) → merged into PX-1. Conflict: PS-2's and PS-5's rules disagree
  on a timeout, and PD-11 disagrees with settled ON-16 on a signal; PX-1's table is
  recommended because it classifies by the facts `tool` already has.
- PT-4 → kept, relocated by PX-2. Conflict with `layers.md` (`internal/cli` kept,
  `dispatch.Warning`): the envelope reader stays internal and `Warning` stays `dispatch`'s,
  because PD-16 removes the import that made PT-4 ask for a public package.
- PD-16, PT-10, PT-14, PI-14 (no root package) → merged into PX-2. PT-10's "moves to
  dispatch's onenote stage" reads "`stages`'".
- PS-14, PT-4 (the stand-in) → conflict on the home: `internal/tooltest`, not `tool/tooltest`,
  since no other module's tests run a tool (tests-and-docs).
- PT-11 (`Tool.AtLeast`), PS-13 (`atLeast`) → one finding; in PX-16.
- PT-17, PS-16, PI-14 (names), PT-18, PD-21, PS-12 (`Arrived`), PI-17 → merged into PX-16.
- PT-2, PD-19 → conflict on a store's warning order; PT-2's recommended (PX-5). PD-19's
  removal of `Meta["warnings"]` stands, in PX-4.
- PT-6 → stands; its parsed warning is `internal/cli`'s two fields, made a `dispatch.Warning`
  in `stages` (PX-2).
- PI-7, PT-10 (the reason text), PD-19, PS-2 (unit warnings) → merged into PX-4.
- PI-6, PD-12 → merged into PX-7; the guard is the dispatcher's, for six fields, not two.
- PS-10, PS-11, PD-8 (names), PD-20 (thresholds in the identity) → merged into PX-8.
- PI-1, PI-15, PS-4, PS-5, PS-17, PT-3, PT-8, PT-20, PD-13, PD-14 → each stands for its own
  mechanism; the values' way in and their owners are PX-3. Conflict: PS-5 has a part past its
  output cap `limited`, PS's note asks for one place; PX-3 recommends the stage's request, in
  its identity, recorded with a warning, since `Limited` is never recorded. PD-13's reliance
  on `Limits.Parts` needs the entry bound PI-15 adds, since `Limits` is none by default.
- PI-9, PI-16, PT-1, PT-19, PS-8, PD-17, PD-18, PD-22 → merged into PX-9.
- PS-9, PS-15, PI-12, PD-3 (the reused hash) → merged into PX-10.
- PS-13, PT-9 → both stand; PX-13 adds the copy neither names.
- PT-7, PT-9 (media from the `document` line), PT-10 → PX-14 gives them one function.
- PI-14 (`route.PNGSignature`, one caller), PS-13 (`media` should use it) → no conflict once
  both land: two callers, the export stays.
- PI-3, PS-3, PS-17 → the same defect in three adapters, one process per unit over a source
  read from its start; each fix stands in its package, and no shared helper is warranted.
- PS-7, PS-8, PT-19, PD-18, PT-12 (the Threader's saved form), PT-15 → consistent with each
  other: surface with no caller stays in the spike. PS-12 is the exception (decision 14).
- PD-1, PD-2, PD-4, PD-5 → stand as written; each changes a `layers.md` sentence or a goal
  decision and is listed below.
- PD-7 → stands; consistent with PD-4, since a single-process `FileIndex` has no lease to lose.
- PI-18, PI-19, and PI's note on HTML → roadmap backlog comments; nothing to merge.

## Decisions for the architect

1. **Which failures are recorded** (PX-1; PS-2, PS-5, PD-11, ON-16). Options: PD-11's narrow
   rule (only a tool's exit 3); PX-1's table (a tool's own exit, its own fatal signal, and its
   own time bound are the content's; an outside kill, a cancel, and a failed start are the
   run's). Recommend PX-1's: it is the only rule under which ON-16's signal is "recorded
   failed" and a `.pst` with one bad attachment records.
2. **A replay miss** (PD-1; the goal's "prepares the replaying part in place"). Options: the
   nearest ancestor with a file prepares again, events possibly repeated; found parts' bytes
   kept in `Content`; `ErrStale` for redelivery. Recommend PD-1's; it refines the decision
   rather than reversing it, and `layers.md` states at-least-once events.
3. **A lost claim** (PD-4). Options: unwind the part in place (`layers.md`); end the dispatch
   with `ErrClaimLost` for redelivery. Recommend PD-4's: a claim is lost only by a stalled
   process, and unwinding needs a context tree and repeats events anyway.
4. **The resume cursor** (PD-2; the goal's "at most `Input.After`"). Options: numeric
   comparison; equality, the cursor opaque. Recommend equality: mail matches by `id == after`
   (mail: src/pst.rs:433) and node ids don't ascend in read order. Supersedes the decision.
5. **`Event.Keep`** (PD-5). Options: keep it; drop it, the body copying or moving a file it
   wants. Recommend dropping: six callers, all tests.
6. **Flags** (PX-6; PI-4, PD-6). Options: `Index.Flags` and `RecordFlags` as settled, policy
   frozen at preparation; flags in the entry, name flags per occurrence, the verdict under the
   current policy. Recommend the second; it answers `layers.md`'s open question. Sub-choice:
   `Item.Flag` replacing `QuarantineError`; recommend it, as one channel for PI-13's flags.
7. **The module's shape** (PX-2; PD-16, PT-4). Options: adapters stay in `dispatch` and the
   envelope goes public; adapters move to `stages`, the envelope reader internal, `Warning` in
   `dispatch`. Recommend the second. With it, `Read` and `Convert` return the sequence itself
   (PT-4), a change to "Running the tools".
8. **`stages.Build`'s options** (PX-3). Options: the settled three arguments, bounds set per
   package by a host that wires its own stages; one `Options` struct. Recommend `Options`;
   without it Cerebro can set none of the bounds the groups add.
9. **Default bounds** (PI-1, PX-3). Options: `Limits` and run bounds none in the library, the
   commands' configurations carrying values; `dispatch.New` refusing a zero `Limits.Bytes`.
   Recommend the first (baseline-standards: no policy in a library), with `cmd/docproc`
   shipping starting values and acceptance dispatching PI-1's bombs through it.
10. **Envelope additions** (PT-6, PT-7, PT-11). Three additions under schema "2" to our own
    tools: `--version`'s second line `schema <n>`; the warning escape's form and an error
    reason that never leads with the input path; a `message` line's `media`. Recommend all
    three before the tool tasks start; the alternative to PT-7, the reader parsing Markdown,
    is worse, and without PT-11 a tool of another schema fails every job, not startup.
11. **Archive entries that harm no stage** (PI-7). Options: links, devices, traversing and
    absolute names stay unsafe, the archive quarantined whole, and `inspect`'s definition
    reworded; they become active, the entry an unread child (PX-4), the rest read. Recommend
    active; bombs and unreadable directories stay unsafe.
12. **Check names before Version "1"** (PX-15). Options: 24 per-format names; seven rule
    names. Recommend the rule names; no evidence shows a policy that differs by format.
13. **Silence detectors** (PS-3, PS-11). Options: both, three decodes with the default; the
    tracker alone, one decode, audio in an MP4 and an M4A windowed alike, about 250 lines
    less, at 0.009 to 0.021 worse word error rate on the three measured corpora. Recommend the
    tracker alone, with acceptance measuring word error rate on ~/docs' audio before the tag;
    silencedetect remains at v0.9.0 if the number disappoints.
14. **Live sources** (PS-12). Options: `media`'s reader, URL, and pacing surface promoted with
    a doc saying no stage uses it; left in the spike until `protocol` names its owner.
    Recommend leaving it, by the rule PS-7 and PS-8 apply to every other callerless surface.
15. **The unread child** (PX-4). Options: unread entries, media, and skipped items stay
    records with string Meta; `Found` may carry an outcome and no file. Recommend the second;
    it makes outcome counts true and gives a package's encrypted section its outcome.
16. **A store's warning order** (PX-5). Options: after the last record (PD-19); where read,
    best effort (PT-2). Recommend PT-2's; only it agrees with decision 4 on a resume.
17. **Constructors and names** (PX-16; the goal's deferred decision). Options: errors for a nil
    tool, as the spike; a panic by name, as blobfs and the standard. Recommend the panic, and
    the five other lines of PX-16 with it.
18. **Test tiers** (PX-9). Options: `layers.md`'s helpers row as written; the findings and
    bake-off tiers, `corpustest`, and the model harnesses stay in the spike, readers replay
    the tools' goldens in place, `dispatchtest` public. Recommend the second.
19. **`FileIndex`** (PD-7). Options: cross-process claim files with `Renew`; single-process,
    claims in memory. Recommend single-process; its one user is one process per run directory.
20. **The time bound's reach** (PX-1, PX-3). Options: a recorded timeout's bound in the
    stage's identity, so raising it prepares everything again; out of the identity, the host
    dropping what the old bound failed. Recommend out of the identity, stated in `dispatch`'s
    doc.
