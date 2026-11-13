# Evaluation · processing, stages and media (spike-ai-pipeline-processing v0.9.0)

The four packages are sound to promote: each tool sits behind an adapter that checks its version
at construction, runs under the caller's context, forces its coder or device, and writes
reproducible bytes, and `media`'s one-run stream is the best-built part of the group. What is
weak is what happens around a failure and around size, plus a layer of experiment surface that
the spike's own findings ruled out. The three most consequential findings: every digitally
signed PDF fails its document part, because the form reader itself refuses a signature field's
value, not qpdf as the roadmap says (PS-1); any one failed form read, attachment listing, page,
or window fails the whole part after its other outputs are made, so nothing is recorded and
every rerun prepares it all again (PS-2); and the audio stage decodes the source from its start
once per window, and twice more to detect, while `media` already cuts the same bytes from one
decode (PS-3).

Evidence was read from the spike at tag `v0.9.0`; paths are relative to its root, and `root:`
paths to `~/experiments/spike-ai-pipeline`. Two claims were checked by running a tool: qpdf
12 over `~/docs/pdf/large/cfr-2024-title32-vol1.pdf` (PS-1), and gs 10.08.0's page count over
a one-page PDF whose page tree states two billion pages, which gs counts as 1, so no finding
rests on a forged count. Nothing else was run: PS-4's page size and PS-6's duration mismatch
are read from the code, and say so.

## Findings

### PS-1 · A signed PDF fails its whole document part in the form reader
- **Kind:** issue
- **Weight:** high
- **Where:** document/qpdf.go:254-257, 290-334; document/qpdf_test.go:187; dispatch/document.go:164-170
- **Against:** roadmap.toml, `[goals.v1.ai.processing.tasks.library]` (the form read that fails
  after 1,823 pages render); validation-first (the failure names the cause)
- **Finding:** qpdf doesn't refuse the file. `qpdf --json=2 --json-key=acroform` over
  `cfr-2024-title32-vol1.pdf` exits 3, success with warnings, which `Form` accepts, and writes
  one field: `"fieldtype": "/Sig"`, `"fullname": "USGPOSignature"`, `"value": "3033 0 R"`.
  Object 3033 is the signature dictionary (`/ByteRange`, `/Contents`). `parseForm` calls
  `qpdfValues` on every field before it looks at the field's type, and `qpdfText` accepts only
  `u:`, `b:`, `n:/`, and `/` prefixes, so `"3033 0 R"` returns `value "3033 0 R": neither a
  string nor a name` and the whole form fails. A signature field's `/V` is a dictionary by the
  PDF specification, so this is every signed PDF, not one odd file: the roadmap's "qpdf refuses
  a field whose value is an indirect reference" names the wrong party and too narrow a case.
  The package has `SignatureField`, but its only test gives the field `"value": null`
  (qpdf_test.go:187), an unsigned one.
- **Proposed design:** A field's value never fails the form. `qpdfValues` returns no value for
  anything that isn't a string, a name, or an array of them (an object reference, a dictionary,
  a number), and the field is kept with its name and type. A signature field gains `Signed bool`
  (its value is present), which is the fact a consumer wants from it. Only JSON that doesn't
  parse, or isn't version 2, fails `Form`. A unit case takes the JSON above verbatim, and the
  integration tier gains one signed fixture. The roadmap's sentence is corrected.
- **Carried by:** library; roadmap (the summary's wording)

### PS-2 · One failed read fails the part, and a failed part is never recorded
- **Kind:** issue
- **Weight:** high
- **Where:** dispatch/document.go:151-182, 288-326; dispatch/audio.go:176-195;
  dispatch/video.go:196-200; dispatch/dispatch.go:610-617
- **Against:** roadmap.toml, library task ("could be a warning beside the pages"); layers.md,
  "Dispatch" (`Item.Warning`, "the index records it, so a seen part replays its warnings")
- **Finding:** The document stage collects a failed `RequiresPassword`, a failed `Form`, a
  failed attachment listing, and every failed page into `failed`, prepares everything else, and
  then yields the joined error. The dispatcher sets `complete = false` for that error and
  `record` returns without writing an entry, so the pages just yielded are in no index entry and
  the next dispatch renders them all again. The audio stage does the same with one failed window
  of hundreds, and the video stage ends on a run error after the frames and windows it yielded.
  So the rule is stricter than the roadmap states: not only a form read, but one page gs refuses
  out of 1,823, costs the whole part on every rerun. The policy was reasonable while a stage
  could only yield outputs or end with an error; `Item.Warning` now gives it a third answer.
- **Proposed design:** One rule for the four stages. A failure of something beside the content
  (the password probe, the form, the attachment listing) and a failure of one unit among many
  (a page, a TIFF page, a window, a frame) is a `Warning{Tool, Where, What}`, `Where` naming the
  unit ("form", "attachments", "page 7", "window 12"), and the part is prepared with the outputs
  it has. A part fails only when nothing of its content can be read: the count fails, the probe
  finds no stream, or every unit fails. A warning is recorded only for a tool's own refusal, a
  `*tool.ExitError` or a parse of its output; a run that was killed, never started, or ended
  with the context still fails the part unrecorded, so a transient fault isn't replayed as a
  permanent gap. A host that wants a warned part prepared again drops its entry (`Index.Drop`).
- **Carried by:** library

### PS-3 · The audio stage decodes the source once per window, and media already avoids it
- **Kind:** issue
- **Weight:** high
- **Where:** audio/windows.go:262-283 (`-i` with `atrim`, no seek), 111; audio/audio.go:348-420
  (volumedetect, then silencedetect, each a whole decode); media/run.go:447-575;
  dispatch/audio.go:153-176; root: internal/docproc/stages/stages.go:328-332
- **Against:** roadmap.toml, library task (the 4.4 h audiobook, 1,614 s against 332 s for
  everything else)
- **Finding:** Each window is its own ffmpeg run over the whole input, trimmed by
  `atrim=start_sample:end_sample` after `aresample`, so window n decodes everything before it:
  645 windows of the audiobook decode about 322 times its length in total. Before that, the
  default detector decodes the source twice (volumedetect for the mean, silencedetect for the
  silences), and the source is probed twice (`detect`, then `Windows`). The stage is wired with
  no concurrency (`AudioConfig{Splitter, Version}`), so the runs are serial. `media` cuts
  windows from one decode already: `audioWriter` feeds ffmpeg's PCM to a `Tracker` and a
  `Planner` and slices each window from the stream, and its doc states, and the streams
  finding's behavior 8 holds, that such a window "is the same bytes audio's Splitter.Windows
  cuts". The two paths decode with the same arguments (`decodeArgs` and the window run, less
  the trim). Seeking per window (`-ss`) is the weaker fix: the resampler would start cold at
  each seek and seek accuracy varies by codec, so a window's bytes, and the SHA-256 that names
  it, would differ from today's and between formats.
- **Proposed design:** One decode streamed into windows, with the cutting in `audio`. `audio`
  gains the stream cutter `media` holds now: PCM in, windows out, by a `Planner`, with the
  window's samples sliced from the stream and written through `WAV`. `media` calls it in place
  of its `audioWriter`, and `Splitter.Windows` becomes one `decodeArgs` run cut by the plan's
  windows, so its windows are byte for byte what v0.9.0 cuts and no golden or index identity
  moves. That brings a file to three decodes with the default detector, linear in its length.
  The architect then chooses between keeping two detectors and keeping one: under the tracker
  the stage is one decode and one probe, `Detect`'s silencedetect half and the `Detector` switch
  go (about 250 lines and their tests), and audio in an MP4 and audio in an M4A are windowed
  alike, where today the first goes through the video stage's tracker and the second through
  silencedetect. The streams finding prices that: the tracker is 0.015 worse in word error rate
  on AMI (0.322 against 0.307), 0.021 on Tears of Steel, 0.009 on NASA, and better under a level
  change (0 windows cut at the limit under added noise where silencedetect cut 24 on AMI).
- **Carried by:** library; the detector choice by the `design` task

### PS-4 · A PDF's pages are rendered ahead of the consumer without bound, in memory
- **Kind:** issue
- **Weight:** medium
- **Where:** document/prepare.go:151-184 (the slot freed at line 164 once the outcome is in its
  buffered channel), 202-219; dispatch/document.go:311-316
- **Against:** Go Elemental has no page for it; the spike's own doc ("The package sets no size
  limit on a page's image", document/doc.go:60); layers.md, "Dispatch" (outputs are files put
  into `Content`)
- **Finding:** A page's worker releases its concurrency slot as soon as its PNG is in its
  page's channel, not when the consumer takes it. `Concurrency` therefore bounds the gs runs in
  flight and nothing bounds the pages finished and waiting: with a consumer slower than gs
  (0.126 s a page in the bake-off), every remaining page of the PDF is rendered and held as
  `[]byte`. The doc comment says so ("its slot already freed"). The consumer in docproc hashes
  and puts each page into `Content` before the next is taken, which over object storage is
  slower than a render. Each PNG is then written to `page-N.png` by the stage, so it is held
  once in the channel and again on disk. Corrections double it (`png` and `corrected`). Nor is
  a page's raster bounded: the DPI is fixed per request, and a page with the largest MediaBox
  PDF allows, 14,400 points square, is 30,000 pixels square at 150 dpi (computed, not run).
  `audio.Splitter.windows` has the same shape but hands over temporary files, and `media` hands
  over on an unbuffered channel, so the three streams bound three different things.
- **Proposed design:** A page is a file. `Render` writes into the part's directory, where the
  PDF already is, and `PageImage` carries `Path` in place of `PNG`, so the stage's `WriteFile`
  goes and memory is one page's text. A page's slot is released when its outcome is taken, so at
  most `Concurrency` pages are rendered and not yet consumed. A request gains `MaxPixels`: a
  page past it at the request's DPI is rendered at the largest DPI that fits and its output's
  Meta carries the DPI used. If PS-3 lands, the ordered, bounded fan-out is `document`'s alone
  and needs no shared helper.
- **Carried by:** library

### PS-5 · No tool run has a time or resource bound, and no stage caps its outputs
- **Kind:** issue
- **Weight:** medium
- **Where:** document/ghostscript.go:199-213; picture/convert.go:462-503; picture/pages.go:73-75;
  media/media.go:186-190; media/run.go:823-836; dispatch/dispatch.go:97-104
- **Against:** system-tools.md ("runs it under the caller's context"); roadmap.toml, library
  backlog ("The `tool` package could bound every run's memory"); baseline-standards (a bound is
  the application's value, taken as a parameter)
- **Finding:** No file in the four packages derives a deadline: every `Run` gets the caller's
  context as it came, so a page gs never finishes, or an SVG librsvg never finishes, holds the
  part until the dispatch's own context ends, and the dispatcher renews the part's claim for as
  long as the stage runs. No `magick` run passes `-limit` (memory, map, disk, area, time), and
  none of the gs runs bounds its raster. A multi-page TIFF is converted in one `magick` run that
  reads every image before it writes the first, with no page cap. A video's frames are written
  at the source's resolution unless `MaxSide` is set, and `stages` sets none, each PNG read
  whole in `handOver`; nothing caps how many scene changes a film yields. `dispatch.Limits`
  counts parts, bytes, and depth, never a part's outputs, so a PDF's pages, a TIFF's pages, a
  video's frames, and an audio's windows are each unbounded per part.
- **Proposed design:** Each stage's request takes two host-supplied bounds, zero meaning none:
  a per-run timeout, applied as `context.WithTimeout` around each tool run, whose expiry is that
  unit's warning (PS-2), and an output cap per part (pages, frames), past which the part is
  `limited` with what it yielded. `picture.Config` takes ImageMagick's limits as a small struct
  the adapter turns into `-limit` arguments on every run. Memory for the other tools stays the
  `tool` package's backlog item, which this group's evidence supports: gs, magick, and ffmpeg
  all read hostile input. `stages` sets a `MaxSide` for frames, since the vision model's image
  budget is fixed and a 4K frame buys nothing.
- **Carried by:** library; a `tool` task for memory

### PS-6 · The audio plan trusts ffprobe's duration, not the decoded samples
- **Kind:** issue
- **Weight:** medium
- **Where:** audio/audio.go:242-280, 362, 409-418; dispatch/audio.go:158;
  audio/windows.go:119-123, 292-295; media/run.go:510-528
- **Against:** correctness; roadmap.toml, library task (a failed window fails the part, PS-2)
- **Finding:** `PlanWindows` is given `det.Source.Duration`, the container's `format.duration`
  or the stream's, and every silence is clipped to it. ffprobe estimates that figure from the
  bitrate for a headerless or truncated MP3 and reads it from a header that can be wrong for
  others. Where it is longer than the audio, the last windows lie past the decoded end, their
  runs write nothing, each fails with "ffmpeg wrote no samples", and by PS-2 the part fails.
  Where it is shorter, the audio past it is in no window and is never transcribed, silently.
  `media` has neither fault: it ends its plan at `tracker.Duration()`, the samples ffmpeg
  actually wrote. Read from the code; no source with a wrong duration was run.
- **Proposed design:** The plan ends where the decode ends. With PS-3's one decode, the cutter
  gives the planner the decoded sample count as the duration, as `media` does, and the plan
  record's duration is the decoded one, with ffprobe's beside it when they differ by more than
  a frame. ffprobe then only finds the stream. A truncated-MP3 fixture proves both directions.
- **Carried by:** library

### PS-7 · Corrections, regions, and the text-skip are experiment surface the findings ruled out
- **Kind:** simplification
- **Weight:** medium
- **Where:** picture/correct.go (all 249 lines); document/prepare.go:47-65, 191-219, 232-250;
  document/renderer.go:55-102; document/ghostscript.go:117-139, 215-246;
  dispatch/document.go:113-135; root: internal/docproc/stages/stages.go:256, 269
- **Against:** layers.md, "docproc's API" ("depends on no model concept"); data-intelligence.md,
  `text-layer` ("A skip rule must tell a thin layer from a whole one"); none otherwise:
  simplification
- **Finding:** The one wiring that exists builds `NewPreparer(render, nil)` with
  `ImagesRequest{Text: true}`. Nothing outside tests builds a `Corrector`, names a correction,
  sets `Region`, or sets `SkipText`. Each came from an experiment whose finding was negative:
  corrections help "not reliably, on either build", and the staged two-pass flow, which used
  `Assess`, the correction catalog, and header and footer regions, pays "not as built"
  (docs/findings/documents.md:37, 41). `Correction.Description` returns a sentence "for a
  prompt" and `Assess` measures "for a layer above to choose corrections by": both are a model's
  inputs, in the library that knows no model. `SkipText` skips a render when `HasText`, text
  that isn't blank, which a page number satisfies, so it is the rule the `text-layer` task says
  a skip rule must not be; the decision needs the text and the image, which docinfer has. What
  rides on them: `RegionRenderer`, `Region`, `CheckRegion`, `ErrRegion`, `RenderRegion` and its
  30 lines of PostScript, `TextExtractor` and `Renderer` as interfaces with one implementation
  and two type assertions in `page`, `Assessment`, `GrayscaleColored`, `ParseCorrection`, and
  five fields of the document stage's identity.
- **Proposed design:** docproc's `document` prepares a page's text and a page's image, and
  nothing else: `ImagesRequest{DPI, Concurrency, Text}` plus PS-4's and PS-5's bounds, over
  `*Ghostscript` directly. `Region`, `SkipText`, the corrections stage, and `Assess` stay in the
  spike at v0.9.0, where the findings cite them. If docinfer later wants a correction on a
  retry, it runs `magick` on the image it holds, and the catalog's prompt text is written there.
  The stage's identity keeps `dpi` and `text`. This removes about 450 lines of production code
  and the tests over them.
- **Carried by:** library

### PS-8 · The vision harness and the bake-off tiers can't move as layers.md says
- **Kind:** issue
- **Weight:** medium
- **Where:** document/vision_test.go (1,038 lines, built on `linux` alone); document/*_bakeoff_test.go
  (2,551 lines); document/export_test.go; document/ghostscript.go:99-115; .golangci.yml (the
  depguard allow list); internal/visiontest, internal/audiotest
- **Against:** layers.md, "docproc's API" (requires `golang.org/x/text` only; the client is
  imported "only from tests and the test helpers `internal/visiontest` and
  `internal/audiotest`, which move to docinfer with the tests that call a model");
  tests-and-docs (`export_test.go` exports only a clock or a probe)
- **Finding:** `vision_test.go` carries no integration or bake-off tag, so the unit tier of
  `document` imports spike-ai-pipeline-client, and a go.mod that holds `golang.org/x/text`
  alone can't build it. The file isn't a test of `document`: it is an inference layer written
  as test code (page requests, retries with a correction, an ordered stream, a final digest
  request) with 13 tests of itself against a stand-in generator. The four bake-off files and
  `vision_integration_test.go` run on it. They are `package document_test` and reach
  `GhostscriptRender` and `GhostscriptRenderArgs` through `export_test.go`, which compiles only
  into `document`'s own test binary, so none of them can become docinfer's tests as they stand.
  The hook exists for the bake-off's measurement, and `render`'s `stdin` and `usage` parameters
  exist for the hook. `audio/findings_test.go`, `media/findings_test.go`, and
  `media/harness_test.go` are tagged and move cleanly; `audiotest` drives `Splitter.Windows`,
  which PS-3 changes.
- **Proposed design:** The bake-off tiers and the vision harness aren't promoted. They are the
  record of settled questions (the renderer, the placement, the corrections, the flow) and stay
  in the spike at v0.9.0, which the findings already cite by commit. docinfer's pipeline is the
  inference layer, tested there. docproc's `document` keeps its unit, integration, and golden
  tiers, `export_test.go` and `render`'s extra parameters go, and the two Ghostscript tests that
  use the hook read the stand-in's log instead, as the others do. docproc's depguard rule drops
  the client. `visiontest` and `audiotest` go to docinfer only if its integration tier wants
  them; `mediatest` stays.
- **Carried by:** library; the docinfer goal

### PS-9 · Images takes a reader and spools the PDF a second time
- **Kind:** simplification
- **Weight:** medium
- **Where:** document/prepare.go:100-119; dispatch/document.go:279-290
- **Against:** layers.md, "Input from a stream, or a path" ("The stages read paths … So one
  spool at the input serves every stage")
- **Finding:** `Preparer.Images` takes an `io.Reader` and copies it into `req.Dir` through
  `cli.Spool`. Its one caller holds the PDF as a file: the stage opens `p.Path` and hands the
  open file in, so every PDF is on disk twice while its pages render, and the copy is made
  before the count. qpdf, pdfdetach, and gs all read the first copy by path in the same stage.
- **Proposed design:** `Images(ctx, path, req)`. The request's `Dir` stays only as where the
  page files go (PS-4). `document` then imports nothing of `internal/cli` but `LimitedBuffer`.
- **Carried by:** library

### PS-10 · A model's limits are library constants
- **Kind:** issue
- **Weight:** medium
- **Where:** audio/plan.go:14; audio/windows.go:24; document/prepare.go:20; picture/convert.go:35;
  dispatch/document.go:135
- **Against:** baseline-standards ("A library ships no policy the application must own … The
  library takes the value as a parameter"); layers.md ("depends on no model concept"); the
  spike's STANDARDS.md ("A tool's options … are per-request values, never library policy")
- **Finding:** `WindowLimit` is a constant, 30 s, the audio model's input limit, and no request
  can change it; `CheckOverlap` and the planner are written against it. A transcription model
  with another limit needs a library release. `SampleRate`, 16 kHz, is documented as "the rate
  a speech model's audio input takes". `SVGDensity` is a constant 150 "the DPI a PDF's pages are
  rendered at for vision", while the PDF's DPI is a request field, so the two can't be set
  together. `document`'s default DPI is unexported, and the document stage's identity restates
  it as the literal 150.
- **Proposed design:** `audio.Request` gains `Limit`, zero meaning `DefaultWindowLimit`, 30 s,
  and the audio and video stages hash it into their identities. `picture.Request` gains
  `Density` for an SVG, zero meaning the default. `document` exports `DefaultDPI`, and the stage
  reads it. `SampleRate` stays a constant: it is the windows' format, and a model that wants
  another rate is a second format, not a setting. Each default is documented as a starting
  point the host's configuration overrides.
- **Carried by:** library

### PS-11 · The audio stage's identity leaves out four fields it obeys
- **Kind:** issue
- **Weight:** low
- **Where:** dispatch/audio.go:27-33, 120-141, 151-153; audio/audio.go:73-109
- **Against:** layers.md, "Dispatch" (a part is seen when its stage's identity is the current
  one)
- **Finding:** `AudioConfig.Request` is an `audio.Request`, passed whole to `Detect`. The
  identity hashes `Overlap`, `MinSilence`, `Below`, and `Threshold`. A host that sets
  `Detector: DetectorTracker`, or the tracker's `Percentile`, `FloorWindow`, or `Margin`, gets
  other windows under the same identity, so a shared index replays the old ones. The doc
  comment lists the four hashed fields as the ones the stage reads, but nothing clears the
  rest. The cause is one request type serving two detectors.
- **Proposed design:** Settled by PS-3's detector choice: with one detector the request holds
  only that detector's fields and the stage hashes all of them. With two, the stage hashes the
  detector and every field the chosen one reads, as the video stage's `VideoPlan` does.
- **Carried by:** library

### PS-12 · media's live sources have no way in through dispatch
- **Kind:** issue
- **Weight:** low
- **Where:** media/media.go:86-151, 208-232, 319-336, 426; media/run.go:106-117, 458, 723;
  media/sources_test.go, media/sources_integration_test.go (807 lines)
- **Against:** layers.md, "Input from a stream, or a path" (`Reader` is spooled; "no stage reads
  a stream directly"); data-intelligence.md ("Open items pass to `protocol` (live sources …)");
  utc-times ("Every `time.Time` a Go Elemental library returns is in `time.UTC`")
- **Finding:** `Source.Reader`, `Source.URL`, `Source.Paced`, `Tracks`, the scheme table, the
  container network check, and `Event.Arrived` serve a source that arrives as it plays. The
  video stage uses `Source{File}` alone, and `dispatch.Input` is a path or a reader that is
  spooled whole before any stage runs, so a live source can't reach `media` through docproc at
  all. The code is measured and tested, and the capability is a named open item, so it isn't
  dead, but it is a second entry point with no owner in the layers. `Arrived` is `time.Now()`
  as it comes, in the host's zone. The rtsp refusal's message explains the test rig ("ffmpeg
  can't serve it to a test, so it is unproven"), not the caller's mistake.
- **Proposed design:** The `design` task states which it is: a live source is Cerebro's call to
  `media.Streamer` directly, beside the dispatcher, or it waits for a dispatch input of its own.
  Until `protocol` settles it, the live surface is kept and its doc says no stage uses it.
  `Arrived` is converted to UTC. The rtsp message says the scheme isn't supported.
- **Carried by:** architecture (the `design` and `protocol` tasks); library for the two edits

### PS-13 · The same helpers are written two to five times
- **Kind:** simplification
- **Weight:** low
- **Where:** document/qpdf.go:61; picture/spec.go:59; audio/spec.go:50; document/ghostscript.go:60-66;
  media/media.go:67-83; audio/audio.go:511; media/run.go:856, 375, 840;
  audio/windows.go:360, 312; document/qpdf.go:160
- **Against:** none: simplification
- **Finding:** `atLeast` is three identical functions and two inline copies. `lineWriter` is
  the same type in `audio` and `media`, differing by one nil check. `writeTemp` and
  `removeTemp` are in both. `media` declares `pngSignature` where `route.PNGSignature` exists
  and `document` uses it. `capped` is two types of one name with opposite behaviour: `document`'s
  fails the write past its limit, `audio`'s discards it silently.
- **Proposed design:** `tool.Tool` gains `AtLeast(minimum string) error`, since the check is
  about a Tool and every adapter makes it. `lineWriter` moves to `internal/cli` beside
  `LimitedBuffer`. PS-3 leaves one `writeTemp`, in `audio`. `media` reads `route.PNGSignature`.
  `audio`'s `capped` goes with the per-window run.
- **Carried by:** library

### PS-14 · Every package rebuilds the stand-in tool harness
- **Kind:** simplification
- **Weight:** low
- **Where:** document/main_test.go (307 lines); picture/main_test.go (461); audio/main_test.go
  (291); media/main_test.go (360); and seven more files outside the group
- **Against:** tests-and-docs ("A shared test helper stays in its test package until more than
  one test package needs it; then it is hoisted into a `<pkg>test` package")
- **Finding:** Eleven test files in the module mark the test binary as a stand-in through an
  environment variable, re-execute it through a link named for the tool, set `GORACE`, print a
  version, append each run to a JSON log, and read the log back. What each tool does with its
  arguments differs; the mechanics don't. The settings travel in the process environment, so
  the tests of a package can't run in parallel.
- **Proposed design:** `tool/tooltest`: a `Main(m, programs map[string]func(args []string) int)`
  that does the re-execution, a `Link(t, name)` that returns a `*tool.Tool` over the stand-in,
  and the run log. Each package keeps only its tools' behaviour. A stand-in's settings go in a
  file the link names, not the environment, so tests can run in parallel.
- **Carried by:** library

### PS-15 · picture reads each file three times to name a PNG nobody looks up
- **Kind:** simplification
- **Weight:** low
- **Where:** picture/convert.go:156-162, 197-218, 250-267, 423-448; dispatch/picture.go:63
- **Against:** none: simplification
- **Finding:** `ConvertPages` identifies the picture's kind and hashes all of it to name its
  PNG `<sha256>.png`, then copies it into `picture-*/0/`. The dispatcher has already hashed the
  part and identified its kind (`p.SHA256`, `p.Kind`), and docproc finds an output by its own
  SHA-256 in `Content`, never by the input's. The copy earns its place (an SVG's relative
  references find nothing, and the container runner mounts one directory); the hash doesn't.
  `Convert`, the single-PNG form, has no caller outside the package's tests: the stage calls
  `ConvertPages` and office calls `ConvertBatch`.
- **Proposed design:** `Request` takes the `Kind` the caller knows and an output stem, as
  `Picture` already does for a batch; `identify` and its second read go. `Convert` is
  unexported or removed, leaving `ConvertPages` and `ConvertBatch`.
- **Carried by:** library

### PS-16 · Constructors disagree with each other and with the construction rule
- **Kind:** issue
- **Weight:** low
- **Where:** document/prepare.go:33; document/ghostscript.go:56, 127; document/qpdf.go:49;
  document/pdfdetach.go:54; picture/correct.go:107; picture/convert.go:87; audio/audio.go:58;
  media/media.go:67
- **Against:** construction-and-execution ("Optional inputs go in one options struct", "A
  missing required field panics by name", four required inputs); blobfs/data/options.go:216-242
- **Finding:** `picture.NewConverter`, `audio.NewSplitter`, and `media.NewStreamer` take a
  `Config`; `NewGhostscript`, `NewQPDF`, `NewDetacher`, and `NewCorrector` take a Tool
  positionally; `NewPreparer(render, correct)` takes an optional dependency as a positional nil.
  Every one returns an error for a nil required Tool, where the standard, and blobfs, panic by
  name for a wiring mistake and keep errors for input (here, a version too old).
  `RenderRegion(ctx, pdf, page, dpi, region, w)` has five required inputs past the context.
- **Proposed design:** One shape: each adapter is built from a `Config` struct of its Tools, a
  nil required Tool panics as `document: Config.Ghostscript is nil`, and a version below the
  minimum returns the `*tool.VersionError`. PS-7 removes `NewPreparer`'s optional argument and
  `RenderRegion`. The spike's STANDARDS.md never listed the construction page; docproc's does.
- **Carried by:** library

### PS-17 · A PDF costs two gs processes a page, run one at a time as wired
- **Kind:** simplification
- **Weight:** low
- **Where:** document/prepare.go:191-208; document/ghostscript.go:171-213;
  root: internal/docproc/stages/stages.go:269
- **Against:** roadmap.toml, library task (the 1,823-page PDF)
- **Finding:** With `Text: true`, each page is one gs run for its text and one for its image,
  after one for the count: 3,647 processes for the cited PDF, each opening the file afresh.
  `stages` sets no `Concurrency`, so they run serially, as the audio windows do. The bake-off's
  0.126 s a page was measured on fixtures of six and seven pages; nothing measured a page's
  cost in a file of 1,823.
- **Proposed design:** `stages.Build` takes the concurrency for pages and windows from its
  caller and passes it to both stages. The acceptance run times the cited PDF per page, and
  again with gs rendering runs of pages to `page-%d.png` in one process with a per-page
  fallback when a run fails; the library adopts ranges only if the measurement shows the
  per-process cost matters at that size.
- **Carried by:** library (the concurrency); acceptance (the measurement)

## For the synthesis

- **The warning shape fits.** `Warning{Tool, Where, What}` carries PS-2's unit failures without
  change if `Where` may name a unit inside a part ("page 7"), not only a place in a tool's
  input. The dispatcher's rule for what is recorded (a tool's refusal) against what isn't (a
  killed or cancelled run) belongs in `dispatch`, once, not in each stage.
- **Stage identities restate package defaults.** The document stage writes `150`; the audio
  stage re-derives `Below`'s default and omits four fields (PS-11); the video stage rebuilds an
  `audio.Request` to validate. A format package that exported one `Identity()`-ready value of
  its checked request (the video stage's `VideoPlan` is the model) would end the drift.
- **`dispatch.Limits` counts parts, not outputs.** PS-5's output cap needs either a limit the
  dispatcher enforces per part or one each stage enforces; one place is better.
- **office batches metafiles; the picture stage can't.** `ConvertBatch` renders every EMF and
  WMF of a document in one soffice run. If layers.md's "embedded and OLE media become child
  parts" ever extends to pictures, each would become a soffice run of its own through the
  picture stage. Keep pictures inside office's batch.
- **Three stream shapes.** `document` buffers finished pages in memory, `audio` in temporary
  files, `media` hands over on an unbuffered channel. After PS-3 and PS-4 the rule is one: a
  stage holds at most its concurrency of finished units.
- **Build constraints.** Every file of the four packages is `//go:build linux`, as `tool` is.
  docproc's module then has packages that are empty elsewhere; `go vet ./...` on another
  platform and the README should say so once.
- **`tool` additions this group wants:** `Tool.AtLeast` (PS-13), `tool/tooltest` (PS-14), and a
  memory bound (backlog). A per-run timeout needs nothing from `tool`: it is a context.
- **The roadmap's library summary** misnames PS-1's cause, and its two examples (the form, the
  windows) are instances of PS-2 and PS-3, which are wider than it says.
- **Not promoted:** PS-7 and PS-8 together leave about 450 lines of production code and about
  4,000 lines of tests in the spike. The findings documents cite them by commit, so nothing is
  lost by leaving them at v0.9.0.
