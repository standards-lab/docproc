# Evaluation · cmd/docproc (spike-ai-pipeline 6a631f1)

The command is sound to promote. Its composition root is go-cli-sdk-template's almost line for
line, its usage errors build nothing, its fixtures replace nodes instead of wrapping constructors,
and its tests assert on the record and the terminal rather than on structure. It is not yet thin,
but the two thick parts are ones the library's change removes: the copy of a seen part's subtree
from earlier records (`run/replay.go`) and the output store with its `Ref` (`rundir.Store`), about
a fifth of the non-test lines with their tests. What remains is harder than `layers.md` reads in
three places, the most consequential findings: the schema "2" event line can't be the embed
`layers.md` lists, since an input's error line loses its `error`, gains an empty `part`, and the
"1" `warning` is not what "2"'s `warning` replaces (DP-1); the record is the one file not written
whole, so a killed run leaves a torn record no reader accepts, and `Latest` hands exactly that run
to docinfer (DP-2); and `rundir` lands in the library's module a task after `library` tags it
v0.1.0, so `cmd/docproc/v0.1.0` has no tagged library to require under "`docproc` releases
nothing" (DP-3). The largest simplifications: `replay.go` goes whole (DP-4), and `rundir` becomes
one handle a reader opens without creating anything (DP-5).

Evidence was read from the spike at `6a631f1`; its library is spike-ai-pipeline-processing v0.9.0
(`processing:` paths), and docinfer's use of `rundir` is in `internal/docinfer` (`docinfer:`
paths). Paths are relative to the spike's root. `go vet ./internal/docproc/...` passes; nothing
else was run, so every claim below is from reading. The code writes schema "1" throughout
(`schema/time.go:10`); "2" below is `layers.md`'s target.

## Findings

### DP-1 · The schema "2" event line can't be the embed layers.md lists
- **Kind:** issue
- **Weight:** high
- **Where:** internal/docproc/schema/record.go:47-63; internal/docproc/run/execute.go:177-183,
  233-237; internal/docproc/run/replay.go:176, 197, 237-241
- **Against:** layers.md, "The nested modules" ("The record's event line embeds `dispatch.Event`,
  its fields inline, and adds only its own: `event`, `input`, `file`, `quarantine`, and `time`";
  "a record's warning is dispatch's `Warning` in place of a string") and "Dispatch" (the `Event`
  shape)
- **Finding:** Five things in the code don't fit that sentence. (1) An input's error line carries
  the error in `error` (`execute.go:235`), and `dispatch.Event.Err` is `json:"-"`; a part's
  failure moves to `reason`, but an error line has no part and no listed field, so its text has
  no place. (2) `dispatch.Event.Part` is a value tagged `json:"part"`, so an error line marshals
  an empty part (`"id":"","name":"","size":0,…`), where "1" says "absent from an error line".
  (3) `schema.Event` has a field named `Event` (the kind); embedding `dispatch.Event` declares a
  second field of that name, which doesn't compile. (4) "1"'s `warning` is the command's own
  note, "seen with n children, but no record in the run directory holds their lines"
  (`replay.go:240`), on a part line. It has no successor once the library replays children
  (DP-4); dispatch's `Warning` is a third kind of event, so the record gains a fourth line kind,
  `warning`, that "1" never had, and `event` must name it. (5) "1" writes `outputs` and
  `children` on every part line (pointers, `record.go:58-59`); the embed leaves a zero out, which
  `layers.md` settles, but the record's golden and the report's `deref` change with it.
- **Proposed design:** `rundir/schema`'s line is
  `struct { Kind string "json:event"; Input string; dispatch.Event; File *File; Quarantine …; Time *Span; Error string "json:error,omitzero" }`:
  `error` is a sixth field of its own, set on an `error` line only. `dispatch.Event.Part` gains
  `omitzero`, so an error line has no `part`. The kinds are `part`, `output`, `warning`, and
  `error`, and the schema page states that a reader may tell the first three by which of `output`
  and `warning` is set. `layers.md` drops "in place of a string" and says the "1" warning goes
  with the replay.
- **Carried by:** docproc; library (`Part`'s `omitzero`); architecture (layers.md's paragraph)

### DP-2 · The record is the one file not written whole, and a killed run's is unreadable
- **Kind:** issue
- **Weight:** high
- **Where:** internal/docproc/rundir/rundir.go:135-152 (`Begin`, `Write`), 156-163 (`End`),
  194-206 (`Latest`), 243-259 (`ReadRecord`); internal/docproc/run/execute.go:37-41;
  docs/docproc-schema.md ("Every file docproc writes is written whole")
- **Against:** the task's crash-safety ask; docs/docproc-schema.md's own claim; layers.md, "The
  nested modules" (`cmd/docinfer` reads the run directory)
- **Finding:** `Run.Write` encodes into a `bufio.Writer` that is flushed only in `End`. Run 1 over
  ~/docs wrote 12,008 lines (11,085 output and 923 part lines) in 9.5 MB, about 800 bytes a line against a 4,096-byte buffer, so the
  file on disk ends mid-line for nearly the whole 27 minutes. A run killed by SIGKILL, the OOM
  killer, power loss, or a panic never reaches `End`: its manifest has no `end`, its record ends
  in a torn line, and up to a buffer of finished lines is lost though the index has recorded
  their parts. `ReadRecord` fails the whole record on the torn line (`event N: unexpected EOF`),
  so `docproc report` and docinfer's `source.Open` both refuse the run. `Latest` returns the
  greatest id whether or not the run ended, so the default run of `docproc report <dir>` and of
  docinfer (docinfer: source/source.go:41-44) is that run, or one still running. The killed run
  also leaves its `.work-*` directory, which holds every in-flight part's files and which no
  later run removes, and the `.tmp-*` files of any write in flight.
- **Proposed design:** `Write` flushes each line (one `write` per line; no fsync). A reader
  treats a record whose manifest has no `end` as possibly torn: a last line that doesn't end in a
  newline is dropped, never an error. `Latest` returns the latest run that ended, and a reader
  given an unended run by id says so. The schema page states the record is appended, not renamed.
  `Begin` removes `.work-*` directories left under the root, which is safe under the
  single-process rule PD-7 proposes for `FileIndex`; `rundir`'s doc states one writer per run
  directory.
- **Carried by:** docproc

### DP-3 · rundir lands in the library's module after `library` tags it
- **Kind:** issue
- **Weight:** high
- **Where:** internal/docproc/rundir, internal/docproc/schema (the packages); go.mod:5-9
- **Against:** layers.md, "docproc's packages [library]" (`rundir, rundir/schema … [docproc]`)
  and "The nested modules" ("They version with the library, which both commands require"); the
  goal record's decision "`reports` tags cmd/docproc/v0.1.0; `docproc` releases nothing;
  `library` tags docproc, storage, postgres"; release-and-ci (pinned `require` versions are the
  committed state)
- **Finding:** `rundir` and `rundir/schema` are packages of the base module, built by the
  `docproc` task, which runs after `library` has tagged the base module v0.1.0. The command's
  module requires the base module by version, so `cmd/docproc/v0.1.0` needs a tagged base module
  that holds `rundir`, and no task tags one. `cmd/docinfer`, in another repository, needs the
  same tag. The `reports` task may change `rundir` again (DP-7, DP-8), after which the tag must
  move once more.
- **Proposed design:** The `docproc` task tags the base module's next minor (v0.2.0) when
  `rundir` lands, and `reports` tags it again only if it changes `rundir`, before it tags
  `cmd/docproc/v0.1.0`. The alternative, building `rundir` in `library` so v0.1.0 holds it, tags
  a public package before its one writer exists and is the weaker choice. The review note's tag
  list and the decision "`docproc` releases nothing" change either way. A committed `go.work`
  over the base module and `cmd/docproc` carries the change between tags.
- **Carried by:** roadmap (the `docproc` and `reports` summaries; the review note's tags)

### DP-4 · replay.go goes whole, and the run stops reading every earlier record
- **Kind:** simplification
- **Weight:** medium
- **Where:** internal/docproc/run/replay.go:1-249; internal/docproc/run/execute.go:47-53, 88,
  93-94, 97-105, 202-207, 221-225; internal/docproc/run/output.go:106-108;
  internal/docproc/run/export_test.go:27-38; internal/docproc/run/replay_test.go;
  internal/docproc/app/record_test.go:375-560; internal/docproc/report/report.go:89-91, 171-173
- **Against:** none: simplification (it follows from layers.md, "Dispatch", "A seen part replays
  its entry's log from the index", which the change list for `cmd/docproc` doesn't draw out)
- **Finding:** The spike's dispatcher never replays a seen part's children, so the command copies
  each seen part's subtree from the earliest record that holds it: 249 lines, a second copy of
  the report's tree builder (`replay.go:53-96` against `report/report.go:201-252`), a `warning`
  field, a warnings column on the terminal and in the report, and three tests. To do it,
  `readSubtrees` loads every earlier run's whole record into memory before the run starts
  (`replay.go:135-159`), a cost that grows with each run over the directory. The target's
  dispatcher replays the log, children included, so each of these has no caller. "Every run's
  record is whole on its own" then holds from the index alone, and the case the spike can only
  warn about, an index kept while `runs/` is deleted (`TestRun_ReRunNoRecordWarns`), replays
  fully.
- **Proposed design:** `cmd/docproc`'s `run` drops `replay.go`, `subtrees`, the `warnings`
  counters, and the summary's warnings row; `recorder.part` writes one line per event. The
  re-run tests keep their assertions (every line seen, the record whole) and lose the copy's.
  The `docproc` task's line in "The nested modules" names the removal.
- **Carried by:** docproc

### DP-5 · rundir is a writer type plus free functions; a reader needs one handle that creates nothing
- **Kind:** simplification
- **Weight:** medium
- **Where:** internal/docproc/rundir/rundir.go:42-55 (`Open`), 179-267 (`Runs`, `Latest`,
  `ValidID`, `Load`, `ReadManifest`, `ReadRecord`, `WriteRunFile`), 335-360 (`WriteFile`);
  internal/docproc/report/commands.go:28-33; docinfer: source/source.go:37-55,
  outdir/outdir.go:108, 125, 151, 162, report/commands.go:30, infer/commands.go:39
- **Against:** the task's ask (what docinfer needs, nothing more); layers.md, "docinfer's API"
  (`source` reads the run directory "over the run directory's `FileContent`")
- **Finding:** The writer goes through `*Dir`, and `Open` makes the directory and its index, so a
  reader can't use it: a mistyped path would create a run directory. Readers therefore use seven
  free functions over `(root, id)`. Three things follow. `ValidID` is a check each caller must
  remember before `ReadManifest`, and four call sites do (one in docproc, three in docinfer); it
  also passes any single path element (`"nope"` is valid in `TestValidID`), and `Runs` lists
  every directory under `runs/`, so a stray directory named `zz` is `Latest`. In "2" a reader
  must open the content store, and nothing here hands it one or names its directory. And
  `WriteFile` and `WriteRunFile` are exported for docinfer's own directory (`outdir` writes its
  manifest and chunk files with them), which is not a read of docproc's run directory; `Load`
  keeps one production caller once DP-4 lands.
- **Proposed design:** One type with two constructors. `rundir.Open(root)` reads: it fails with
  `fs.ErrNotExist` when root is no run directory and creates nothing. `rundir.Create(root)`
  writes: it makes the directory, the index, and the content store. Readers' methods are
  `Runs()`, `Latest()`, `Manifest(id)`, `Record(id)` (DP-22), and `Content()`; each that takes an
  id refuses one that isn't a run of this directory, so `ValidID` is unexported, and the schema
  check (DP-6) happens once in `Manifest`. The writer adds `Index()`, `Begin`, and `Quarantine`.
  `Load`, `WriteRunFile`, and `WriteFile` leave the public API: the report writes its pages with
  the command's own helper, and docinfer's `outdir` owns its 25-line atomic write.
- **Carried by:** docproc (the docinfer goal takes `outdir`'s write)

### DP-6 · The run directory has no version of its own, and docproc checks none
- **Kind:** issue
- **Weight:** medium
- **Where:** internal/docproc/rundir/rundir.go:42-55, 130, 217-239;
  internal/docproc/report/report.go:52-63; internal/docproc/run/replay.go:137-150;
  docs/docproc-schema.md ("`index/` is processing's `dispatch.FileIndex`, and its format is
  processing's, not this contract's")
- **Against:** layers.md, "The nested modules" ("The run directory moves to schema \"2\"";
  "`cmd/docinfer`'s `source` reads \"2\" alone"); the roadmap's `docproc` summary ("a versioned
  run directory")
- **Finding:** The version is written once per run, into its manifest (`rundir.go:130`), and the
  only code that compares it is docinfer's. `docproc report` renders whatever `Load` decodes, and
  `docproc run` opens the directory's index and store with no check. The parts every run shares,
  `index/`, the store, and `quarantine/`, carry no version at all. The move makes this concrete:
  ~/docs run directories of schema "1" exist, the acceptance task reruns over ~/docs, and a "2"
  command pointed at one would read a `FileIndex` of the old layout (one log per entry is new,
  and PD-7 reshapes its files), find `outputs/` where it expects the content store, and render a
  "1" record whose `warning` is a string where "2" decodes an object. `schema.Version` also now
  covers shapes it doesn't own: the embedded `dispatch` shapes have their own
  `dispatch.Version`.
- **Proposed design:** `Create` writes a root file, `rundir.json`, holding the schema; `Open` and
  `Create` over an existing directory refuse another schema, or a directory with `runs/` and no
  such file, naming both versions. The manifest keeps `schema` for a run read alone. The schema
  page states that "2" includes `dispatch`'s shapes at `dispatch.Version` "1", and that a break
  there is a break here.
- **Carried by:** docproc

### DP-7 · With content by SHA-256, the store's layout joins the contract and the report can't link
- **Kind:** issue
- **Weight:** medium
- **Where:** internal/docproc/rundir/rundir.go:63-81 (`Store`), 269-311 (`stageDir`, `extensions`,
  `ext`); internal/docproc/run/execute.go:91, 147-157, 175; internal/docproc/report/report.go:
  18-22 (doc), 74-79, 255-263; internal/docproc/report/report.html.tmpl:212, 219
- **Against:** layers.md, "The nested modules" ("stores outputs in the run directory's
  `FileContent`"; "found by its `sha256` in place of \"1\"'s `ref` path"); the roadmap's
  `reports` summary ("every output inline")
- **Finding:** `Store`, the per-part counter, `stageDir`, and the extension table go with `Ref`,
  as `layers.md` says. Two consequences are not in the change list. The "1" store is the report's
  link target: every output is `<a href>` or `<img src>` to `../../outputs/<sha>/<stage>/<n><ext>`,
  and the extension is what lets a browser open a `.txt` or `.md` over `file://`. A content file
  named by its SHA-256 has no extension, so a linked text downloads instead of opening; an image
  still renders, since browsers sniff images. And the page must name a file path, so where
  `FileContent` puts a content (flat today, sharded if PD-7 lands) stops being the library's
  private layout: the report, and any reader that isn't Go, follows it. `Render` "reads nothing
  but its arguments" today; inlining text ends that.
- **Proposed design:** The schema page states the content store's directory and the path of a
  content within it, and `rundir` exports that path for a SHA-256, so the layout is fixed with
  schema "2". The report inlines text, Markdown, and record outputs by reading the content store,
  and points `<img>` and `<audio>` at the content file; `Render` takes the reader's handle
  (DP-5), and its determinism test holds over a fixture directory. `run`'s output handler writes
  the line and nothing else.
- **Carried by:** docproc (the layout and the path); reports (the pages)

### DP-8 · A page per input keyed by its SHA-256 collides, and some inputs have none
- **Kind:** issue
- **Weight:** medium
- **Where:** internal/docproc/report/report.go:130-166 (inputs by path);
  internal/docproc/run/execute.go:233-240; internal/docproc/schema/part.go:13-14;
  docs/findings/docproc.md ("The 17 seen inputs are content another input in ~/docs shares")
- **Against:** the roadmap's `reports` summary and data-intelligence.md ("one page per input,
  keyed by the input's SHA-256"; docinfer's pages "linked to docproc's by the input's SHA-256")
- **Finding:** The spike's report keys an input by its path. The planned key fails on its own
  corpus: 17 of ~/docs's 592 inputs hold content another input holds, so two inputs with
  different paths, file metadata, and times map to one page. An input whose dispatch stopped
  before its own event has no part and no SHA-256 at all (the golden's unreadable file), and a
  part that couldn't be read has an empty one.
- **Proposed design:** A page per distinct input content, named by its SHA-256, listing every
  input path that holds it with each one's file metadata, outcome, and time; the tree and outputs
  are shown once, since they are the content's. The index page lists every input, error inputs
  included, each linking to its content's page when it has one. docinfer's link by SHA-256 holds
  unchanged. The `reports` summary says "per input content".
- **Carried by:** reports; roadmap (the summary's wording)

### DP-9 · An index failure is recorded as an input's error, and the run exits 0
- **Kind:** issue
- **Weight:** medium
- **Where:** internal/docproc/run/execute.go:28-31 (doc), 109-128, 233-241;
  internal/docproc/run/doc.go:13-16; processing: dispatch/pool.go:38-40
- **Against:** cli-applications, "Exit codes and command words" ("any other failure returns
  `ExitFailure`")
- **Finding:** `execute`'s doc lists "the index failing" among the errors that stop the whole
  dispatch. The pool doesn't yield it that way: "An error that stops one input's dispatch, its
  input unreadable or the index failing, is yielded with that input". The loop tells the two
  apart by `pe.Input.Path == ""`, so an index that can't be written (a full disk, a permission)
  becomes an `error` line per input, the run goes on through every input, and it exits 0. The
  same holds in "2" for a content put that fails. Beyond that, exit 0 with failed parts is the
  spike's stated choice, and a script has only the summary table to tell a clean run from one
  where every input failed.
- **Proposed design:** The library types an error of its stores (the index, the content store)
  apart from an error of the input, which PD-3 already asks for; `run` stops the run on the
  first, as it does on a record write, so the manifest names it and the exit is 1. Failed parts
  and unreadable inputs keep exit 0, stated in the command's help, and the summary's last line
  gives the counts of failed parts and error inputs so a script can read one line.
- **Carried by:** library (the error's type); docproc

### DP-10 · The nested module's layout is unstated, and Go Elemental's has two readings
- **Kind:** issue
- **Weight:** medium
- **Where:** cmd/docproc/main.go; internal/docproc/{app,apptest,run,report}
- **Against:** cli-applications, "The composition root's layer files" (`cmd/<name>/main.go`,
  `internal/app`); topology-and-naming, "Application-layer import direction"; tool-beside-library
  ("never the packages' home")
- **Finding:** `layers.md` names the module (`docproc/cmd/docproc`) and the spike packages it is
  built from, not where they sit. go-web-service's `tools/slab` is the settled nested tool:
  `tools/slab/cmd/slab/main.go`, `internal/app`, and domain packages at the module's root, which
  are public. Applied here that gives `cmd/docproc/cmd/docproc/main.go` and a public
  `cmd/docproc/run`, so the install path repeats itself and `run.Commands` becomes API under
  `cmd/docproc/v0.1.0`. The spike keeps `run` and `report` under `internal/` because its root
  module held two commands.
- **Proposed design:** `main.go` at the module's root, so
  `go install github.com/standards-lab/docproc/cmd/docproc@v0.1.0` is the install line; the
  composition root at `internal/app` with `internal/apptest`; `run` and `report` under
  `internal/` too, since the module's only public surface is the command and the logic an
  integrator calls is the library's. STANDARDS.md states the exception to "domain packages at the
  root level" for a command nested in its library's repository.
- **Carried by:** docproc; architecture (cli-applications or tool-beside-library, a nested
  command module's layout)

### DP-11 · rundir's contract is tested from the command, which becomes another module
- **Kind:** issue
- **Weight:** medium
- **Where:** internal/docproc/app/record_test.go:145-188, internal/docproc/app/testdata/
  record.golden.jsonl; internal/docproc/rundir/rundir_test.go:46-200;
  internal/docproc/report/testdata/manifest.json
- **Against:** the goal record's decision "contracts proven by committed golden outputs";
  tests-and-docs, "The unit tier" (tests co-located with the source they cover)
- **Finding:** The record's golden, the manifest's assertions, and the re-run tests live in
  `app`'s test package and run the whole command. `rundir`'s own tests cover `Store`,
  `Quarantine`, `Begin`/`Write`/`End`, and `ValidID`; no test in the repository calls `Runs`,
  `ReadManifest`, `ReadRecord`, `WriteRunFile`, or `WriteFile` directly, and `Latest` and `Load`
  appear only as helpers in `run/output_test.go:76-80`. Those are the functions docinfer calls.
  After the move the base module can't import the command, so the public packages ship with the
  reader half untested in their own module and with no committed example of a schema "2"
  directory. PD-22 finds the same gap one layer down.
- **Proposed design:** `rundir/testdata` holds one committed run directory of schema "2": the
  root version file, a manifest, a record with a line of each kind and outcome, one content, one
  quarantined file. `rundir`'s tests read it through the reader's API and write it again through
  the writer's, comparing text. The command's golden test stays as the proof that `run` produces
  that shape, and `cmd/docinfer`'s tests may read the same fixture by copying it.
- **Carried by:** docproc

### DP-12 · A path argument that is a symbolic link, or no file at all, yields nothing and says nothing
- **Kind:** issue
- **Weight:** medium
- **Where:** internal/docproc/run/commands.go:83-133 (`collect`; the `switch` at 113-131 has no
  default)
- **Against:** domain-files, "A CLI domain's commands" ("A state change is never silent");
  tool-beside-library (usable with no knowledge of the internals)
- **Finding:** `collect` reads each argument with `Lstat` and handles a regular file and a
  directory. Anything else falls through: a symbolic link to a directory, a common way to name a
  corpus, gives zero inputs, a run directory with an empty record, a summary of `total 0`, and
  exit 0. The same holds for a FIFO or a device, and for a directory that holds no regular file.
  The doc comment states that a link given as an argument is no input; nothing tells the
  operator.
- **Proposed design:** An argument the operator names is followed when it is a symbolic link
  (`os.Stat`), since naming it is the consent a link found under a walked directory lacks; those
  stay unfollowed. An argument that is neither a file nor a directory is a failure naming it, and
  so is a run whose arguments yield no input, both before any tool is looked up (DP-17).
- **Carried by:** docproc

### DP-13 · export_test.go exports the command's body, because the stages have no seam
- **Kind:** issue
- **Weight:** medium
- **Where:** internal/docproc/run/export_test.go:11-38; internal/docproc/run/output_test.go:
  23-106; internal/docproc/app/infrastructure.go:20, 35-37; internal/docproc/run/commands.go:55
- **Against:** tests-and-docs, "The unit tier" (`export_test.go` "exists only to export a hook
  for what the API cannot reach: a clock or a probe"; a fixture "replaces a node's constructor")
- **Finding:** `export_test.go` exports `Execute`, `Replay`, `Brief`, and `ErrorRunes`. `Replay`
  goes with DP-4. `Execute` exists so a test can run a stage that fails with a chosen error
  (`failing`, `output_test.go:24-33`): the graph's node is the tool lookup, and the stages are
  built from it inside `Run`, so no fixture can supply a stage. `Brief` is tested directly for
  the same reason.
- **Proposed design:** The node is the stage builder, not the lookup:
  `Stages *graph.Node[func(context.Context, []route.Route) stages.Set]`, whose constructor
  returns `stages.Build` over `exec.LookPath`. `apptest.Tools(a, lookup)` replaces it with
  `stages.Build` over the test's lookup, and a new `apptest.Stages(a, set)` with a fixed set.
  The terminal-line test then runs the app with a failing stage and asserts on stdout, and
  `export_test.go` is deleted.
- **Carried by:** docproc

### DP-14 · FileOf, Stat, Stater, and Detect move with Statx, so rundir/schema is shapes alone
- **Kind:** simplification
- **Weight:** low
- **Where:** internal/docproc/schema/file.go:34-87; internal/docproc/schema/part.go:62-81
  (`PartOf`); internal/docproc/run/execute.go:196-198, 245-253
- **Against:** layers.md, "The nested modules" ("`schema`'s `Statx` … moves to `cmd/docproc`'s
  `run`, its one caller")
- **Finding:** `layers.md` moves `Statx` alone. `FileOf` takes a `Stater` only so a test can
  stand in for `Statx`, and its one caller is `recorder.file`, which calls it with the relative
  argument and then overwrites `Arg` with the absolute one (`execute.go:247-251`; "It doesn't
  touch arg: the caller makes it absolute"). `Detect` opens a file and imports `net/http` for
  `DetectContentType`; its one caller is `recorder.part`. Left behind, a public shapes package
  keeps a function type whose production value lives in another module and an `net/http` import.
  `PartOf` goes with the aliases.
- **Proposed design:** `rundir/schema` holds types, the kind constants, `Version`, and `Time`,
  and imports `dispatch`, `inspect`, and `encoding/json`. `run` holds one `fileOf(in input)`
  that reads the stat and builds the `File` with its absolute argument in one step, and `detect`;
  the birth-time probe is the one hook `run`'s `export_test.go` may keep.
- **Carried by:** docproc

### DP-15 · The manifest's `record` and `run` repeat the path, and a reader trusts `record`
- **Kind:** simplification
- **Weight:** low
- **Where:** internal/docproc/rundir/rundir.go:130, 149, 243-244;
  internal/docproc/schema/record.go:118, 123, 134; internal/docproc/app/record_test.go:105-107
- **Against:** none: simplification
- **Finding:** `record` is always `runs/<run>/record.jsonl` and `run` is always the directory's
  name; the test helper asserts both equalities. `ReadRecord` joins the manifest's `record` to
  the root unchecked, so the one path a reader takes from a file's content is the one that can
  leave the run directory, while the id, taken from a flag, is guarded by `ValidID`.
- **Proposed design:** The record's path is the layout's, stated on the schema page; the manifest
  drops `record`, `Run.Record` goes, and the reader opens `runs/<id>/record.jsonl`. `run` stays,
  for a manifest read alone.
- **Carried by:** docproc

### DP-16 · The manifest's Tool and StageState are field-for-field copies of stages.Tool and Row
- **Kind:** simplification
- **Weight:** low
- **Where:** internal/docproc/run/execute.go:273-296 (`toolsOf`, `stagesOf`);
  internal/docproc/schema/record.go:148-171; internal/docproc/stages/stages.go:45-65
- **Against:** the goal record's decision "no shape repeats another"; layers.md, "The nested
  modules" ("`Tool`, and `StageState` stay its own")
- **Finding:** With `stages` and `rundir/schema` in one module, `schema.Tool` is `stages.Tool`
  with `Err` as a string, and `schema.StageState` is `stages.Row` with strings for its typed
  fields; `toolsOf` and `stagesOf` exist to copy one into the other. `layers.md` keeps them
  apart, written before `stages` was the library's.
- **Proposed design:** `stages.Tool` and `stages.Row` gain the manifest's JSON names, `Tool`
  carrying its failure as `Error string` beside an untagged `Err`; `schema` aliases both, and
  the two copy functions go. Cerebro gets the same shapes for reporting what a replica wired.
- **Carried by:** library; docproc

### DP-17 · Every tool is probed before the inputs are checked
- **Kind:** issue
- **Weight:** low
- **Where:** internal/docproc/run/commands.go:54-63; internal/docproc/app/app_test.go:315-330
  ("A path that doesn't exist fails the run, after the stage table")
- **Against:** domain-files, "A CLI domain's commands" (bad input builds no dependency)
- **Finding:** `Run` builds the stages, which runs each tool's version check, LibreOffice's in a
  container, prints both tables, and only then walks the paths. A mistyped path costs the whole
  probe and scrolls its error under two tables. The check needs the filesystem, so it can't be
  `Validate`'s, but nothing makes it wait for the tools.
- **Proposed design:** `Run` collects the inputs first, then builds the stages; a path error is
  the only output of a run that has one.
- **Carried by:** docproc

### DP-18 · quarantine's flags.json repeats the record and keeps the first occurrence's flags
- **Kind:** simplification
- **Weight:** low
- **Where:** internal/docproc/rundir/rundir.go:83-108; internal/docproc/run/execute.go:185-191;
  internal/docproc/schema/record.go:97-105
- **Against:** none: simplification (PI-4 bears on it)
- **Finding:** `Quarantine` writes the part's flags beside its file, once per SHA-256, and keeps
  what is there. The same flags are on the part's record line. PI-4 shows flags depend on the
  name as well as the bytes, so a second occurrence under another name has other flags, and
  `flags.json` holds only the first's. The caller also skips `Quarantine` when the part has no
  file (`execute.go:185`), which in "2" is every replayed quarantined child, so its line names no
  kept file though one is there.
- **Proposed design:** The kept file is `quarantine/<sha256>`, with no `flags.json`; the line's
  `quarantine` is that one relative path, set whenever the file exists, whether this event
  copied it or an earlier run did. The flags are the record's.
- **Carried by:** docproc

### DP-19 · The port to go-cli-sdk v0.4.0 and go-core v0.12.0 touches three places and adds a test file
- **Kind:** issue
- **Weight:** low
- **Where:** go.mod:8-9; internal/docproc/app/app.go:45-47;
  internal/docproc/run/commands.go:68; internal/docproc/apptest/apptest.go:38-42;
  internal/docproc/app (no `graph_test.go`)
- **Against:** cli-applications, "The composition root's layer files" ("`Run` is one
  `cli.Program{Root, Graph, LifecycleConfig}.Run` call"); go-cli-sdk-template at v0.4.0
- **Finding:** The spike is on go-cli-sdk v0.2.0 and go-core v0.10.0. `App.Run` calls `cli.Run`
  with `cli.WithGraph`, a functional option that v0.3.0 replaced with `cli.Program`; `--exclude`
  uses `cli.StringsVar`, removed in v0.4.0 for `fs.Var(cli.StringsValue(…))`; `apptest.Builds`
  passes `Observe` a `func(string)`, where go-core v0.12.0's takes a `graph.Ref`. The template's
  `graph_test.go` (`TestGraph_NodesNamedByTheirFields`, `TestGraph_LeafLayers`), built on
  `Command.Leaves`, has no counterpart here.
- **Proposed design:** The `docproc` task starts `internal/app` and `internal/apptest` from the
  template at v0.4.0 and adds the stages node and its fixtures (DP-13), rather than porting the
  spike's files forward.
- **Carried by:** docproc

### DP-20 · The manifest doesn't say what wrote it or how many inputs it was given
- **Kind:** issue
- **Weight:** low
- **Where:** internal/docproc/schema/record.go:114-138; internal/docproc/run/execute.go:55-62;
  internal/docproc/app/version.go:26-35
- **Against:** the roadmap's `acceptance` summary (reruns over ~/docs, compared with the spike's
  runs)
- **Finding:** The manifest records each tool's version and each stage's identity, and not the
  command's own version or the library's, though `moduleVersion` reads the first for the
  `version` command. It lists the path arguments and not how many inputs they came to, so a
  reader of a stopped run can't tell how many were never dispatched; the terminal prints no
  count either. Both are additions "2" can take at no cost now.
- **Proposed design:** The manifest gains `docproc` (the command's module version and the
  library's, from the build info) and `collected`, the number of inputs the arguments named.
- **Carried by:** docproc

### DP-21 · --workers refuses a value above this machine's CPUs
- **Kind:** issue
- **Weight:** low
- **Where:** internal/docproc/run/commands.go:44-46, 69; internal/docproc/app/app_test.go:203-204
- **Against:** none: simplification
- **Finding:** `--workers 8` is a usage error on a four-CPU runner, so one command line doesn't
  work on every host, and the usage test computes its expectation from `runtime.NumCPU()`. The
  bound guards nothing the pool needs: a worker mostly waits on a tool's process.
- **Proposed design:** `--workers` is at least 1, with the default unchanged.
- **Carried by:** docproc

### DP-22 · ReadRecord returns the whole record as a slice
- **Kind:** simplification
- **Weight:** low
- **Where:** internal/docproc/rundir/rundir.go:243-260; internal/docproc/report/report.go:145-148
- **Against:** none: simplification
- **Finding:** A record is read into `[]schema.Event` and the report then groups it by input into
  a second map. Run 1 over ~/docs is 9.5 MB and 12,008 lines, which is fine; a mail store of a
  hundred thousand messages is not, and the per-input pages of `reports` and docinfer's `source`
  each need one pass, not a slice.
- **Proposed design:** `Record(id)` returns `iter.Seq2[schema.Event, error]`, yielding lines in
  order and ending on the first that doesn't decode (or, for an unended run, DP-2's torn last
  line).
- **Carried by:** docproc

### DP-23 · An input's time is stamped on the pool's feeder, through a sync.Map
- **Kind:** simplification
- **Weight:** low
- **Where:** internal/docproc/run/execute.go:90, 130-141, 255-263;
  internal/docproc/schema/record.go:107-112
- **Against:** none: simplification
- **Finding:** `timed` stores `time.Now()` when the pool's feeder pulls an input, which is before
  a worker is free, so the one input waiting at the feeder is charged the wait: with every
  worker busy its `time` includes however long the next worker took to finish. The record's
  schema documents the wait, and the recorder, which "holds no lock", needs a `sync.Map` for this
  one field because the feeder is another goroutine.
- **Proposed design:** The span starts at the input's first event and ends at its last, both
  read on the loop's goroutine, with a plain map; the schema page says so. An input's time is
  then its own work, less its inspection before the first event.
- **Carried by:** docproc

### DP-24 · execute takes six inputs, and schema.Time keeps the zone it was given
- **Kind:** issue
- **Weight:** low
- **Where:** internal/docproc/run/execute.go:32; internal/docproc/schema/time.go:19-25;
  internal/docproc/schema/stat_linux.go:18, 24; internal/docproc/schema (no `main_test.go`)
- **Against:** construction-and-execution, "The rule" (four required inputs); utc-times ("Every
  `time.Time` a Go Elemental library returns is in `time.UTC`"; "Tests prove UTC under a non-UTC
  zone")
- **Finding:** `execute(ctx, w, set, out, args, inputs, workers)` has six inputs after its
  context. `schema.Time` formats in UTC and decodes to UTC, but a value built with
  `schema.Time(time.Now())` or from `Statx` holds local time, so `time.Time(m.Start)` is local in
  the writer and UTC in a reader, and the two compare unequal with `==`. Once `rundir/schema` is
  a public library package the zone test applies, and the package has no `TestMain`.
- **Proposed design:** The run is a struct with a method (`runner{Out, Args, Inputs, Workers,
  Stages}.run(ctx, w)`), which also gives the recorder's fields one home. `schema` exports
  `TimeOf(t time.Time) Time`, which converts to UTC, as the only way the writer makes one, and
  its tests run under Europe/London in `main_test.go`.
- **Carried by:** docproc

### DP-25 · The roadmap's `docproc` summary names a role `stages` now has
- **Kind:** issue
- **Weight:** low
- **Where:** internal/docproc/stages/stages.go:165-222; internal/docproc/run/commands.go:55
- **Against:** the roadmap, `[goals.v1.ai.processing.tasks.docproc]` ("docproc's README names it
  as the shipped composition root of its adapter convention, since every other wiring in the
  repository is a test's")
- **Finding:** The wiring of adapters to tools is `stages.Build`, which `layers.md` moves into
  the library so Cerebro and the command wire alike. The command's whole share of it is one call.
  The README sentence the summary asks for would name the command as the home of wiring it no
  longer holds.
- **Proposed design:** The README names `stages` as the shipped wiring and `cmd/docproc` as its
  first caller; the task's summary is reworded to match.
- **Carried by:** roadmap
