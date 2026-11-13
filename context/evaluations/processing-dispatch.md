# Evaluation · processing, dispatch, content, and index (spike-ai-pipeline-processing v0.9.0)

The spike's dispatcher is sound to promote as the walk it is: one goroutine per dispatch,
depth-first, constant memory per part, a small index contract proven against both
implementations, and tests that measure what they claim (heap, disk, crash and resume). It is
also the package `layers.md` changes most, and the changes are larger than its change list reads:
the spike never replays a seen part's children, never renews a claim, and never does work off the
walk's goroutine, so replay, leases, and bounded puts are each new control flow, not edits. The
three most consequential findings: a replay miss can't be "prepared in place", because a replayed
child has no file and only its parent's stage can produce one (PD-1); the resumed replay's
"`After` at most `Input.After`" skips messages in a `.pst`, whose ids are identities read in
folder order, never ordinals (PD-2); and puts before events, plus leases that must outlast the
caller's loop body, turn the single-goroutine walk into a pipeline whose ordering, failure
classes, and lost-claim path `layers.md` leaves unstated (PD-3, PD-4). The largest
simplifications: `Event.Keep` has no caller (PD-5), flags can leave the `Index` (PD-6), and
`FileIndex`'s cross-process claims can go (PD-7).

Evidence was read from the spike at tag v0.9.0, its one caller in `spike-ai-pipeline` at
`6a631f1` (`root:` paths), and `spike-ai-pipeline-mail` v0.3.1 (`mail:` paths). Paths are
relative to the spike's root. Nothing was run; every claim below is from reading.

## Findings

### PD-1 · A replay miss can't be prepared in place: a replayed child has no file
- **Kind:** issue
- **Weight:** high
- **Where:** dispatch/dispatch.go:686-708 (`seen`), 474-496 (`releasedPart`), 500-526;
  dispatch/stage.go:139-146
- **Against:** layers.md, "Dispatch" ("A miss while replaying … prepares that child in place,
  within the same dispatch, as a part not seen is prepared"); "One job, and its cancellation"
  (the `MissingError` drop: "the redelivery's replay finds that entry gone and prepares the part
  again in place")
- **Finding:** A part is prepared from `Part.Path`. A found part's file exists only because its
  parent's stage wrote it (`Found.Path`) while that stage ran. A replay runs no stage, so a
  replayed child has no file, and nothing stores a found part's bytes: `Content` holds outputs
  only. The spike shows the same wall today: `seen` yields the entry's outputs and never reads
  its children ("counts the entry's children, never reading them"), and the one file-less path,
  `releasedPart`, fails the part when "the index no longer holds its content's entry"
  (dispatch.go:493). So the recursion over children, the per-child lookup, and the miss handling
  are all new code, and the miss as written has nothing to prepare from. Misses are not rare:
  content shared with an older input has an older entry, so the host's sweep by age drops a
  child's entry while its newer parents' entries live; docinfer's `MissingError` drop names the
  missing output's part, usually a child. Two more consequences follow from the text as written.
  The replay does one `Lookup` per replayed child, so freshness is O(routes) only for the
  `Entry.Stages` check, and a replay is O(parts) lookups. And "drops the entries along its replay
  path" drops entries this dispatch holds no claim on and never records again, so the next
  dispatch of the same input prepares the whole input, a whole `.pst` for one missing attachment.
- **Proposed design:** A miss is resolved by the nearest part that has a file, one level at a
  time. `seen` first resolves the entry without yielding: it reads the log and looks up each
  child that needs it (not recursing into them). If every child resolves, it replays, and each
  child resolves its own children the same way when its turn comes. If a child misses and the
  replaying part has a file (the input, or a part found by a stage that ran), the part drops its
  own entry, claims it, and is prepared as not seen: its stage finds the children again with
  files, the live ones replay, and the missing one prepares. If the replaying part has no file,
  the miss passes up to its parent's resolve, which is why resolve precedes the first event: no
  event of a subtree is out before the subtree is known whole one level down. A miss met deeper,
  after events are out, is the same rule applied at the nearest ancestor with a file, and its
  already-replayed events repeat as prepared events; `layers.md` states that a dispatch may
  repeat a part's events, so docinfer's `job` and Cerebro count by part ID, never by event.
  `layers.md` replaces "prepares that child in place" and "drops the entries along its replay
  path" with this, and states the O(parts) lookups. The rejected alternative: putting every
  found part's bytes in `Content` so a child prepares from there, which doubles blob storage for
  every store and archive.
- **Carried by:** library (and the docinfer goal, for `job`'s counting)

### PD-2 · The resumed replay's "at most `Input.After`" skips messages in a .pst
- **Kind:** issue
- **Weight:** high
- **Where:** dispatch/dispatch.go:164-169 (`Input.After`); dispatch/email.go:92, 209-212;
  mail: src/pst.rs:433 (`if id == after`), src/mbox.rs:130, README.md:39-50,
  docs/findings/mail-cli.md:412
- **Against:** layers.md, "Dispatch" ("every item through the last item whose `Recorded.After`
  is nonzero and at most `Input.After`"); the goal record's decision "a resumed replay skips the
  log's prefix through the last item whose `After` is at most `Input.After`"
- **Finding:** mail's `--after` is an identity, matched by equality: a `.pst`'s message is named
  by its node id, and the read's order "is the folders' depth-first order", each folder's
  contents table in turn. Node ids don't ascend in that order: a message in a later folder can
  hold a smaller id than one in an earlier folder. A replay that skips through the *last* log
  item whose `After` is numerically at most the cursor then skips every later message with a
  smaller id, silently. Only an mbox's ordinals ascend. A second difference: a resumed
  preparation yields the store line and every folder line again before the tail (mail's
  README), while a resumed replay, skipping a prefix, yields neither.
- **Proposed design:** The replay skips through the item whose `After` equals `Input.After`.
  When no item's does, the dispatch ends with an error, as mail refuses "an id that names no
  message". `layers.md` states that `After` is opaque and compared by equality only, in
  `Item.After`, `Event.After`, `Recorded.After`, and `job`'s checkpoint ("the last message in
  read order", never "the highest"). The skipped prefix keeps the store's and folders' record
  outputs (items before the first nonzero `After`), so a resumed replay and a resumed
  preparation yield the same events.
- **Carried by:** library (layers.md's rule; the docinfer goal for `job`'s wording)

### PD-3 · Puts before events make the walk a pipeline; its order, cost, and failures are unstated
- **Kind:** issue
- **Weight:** high
- **Where:** dispatch/dispatch.go:564-588, 657-681 (`output`, `yieldOutput`), 712-725
  (`hashFile`); dispatch/pool.go:32-37, 118-131; dispatch/text.go:139; dispatch/vision.go:58
- **Against:** layers.md, "Dispatch" ("Preparation puts concurrently, at most `Config.Puts` in
  flight per dispatch, while the appends and the events keep the order the stage yielded");
  "One job" ("stores each output's content there before its event"); go-storage `store.go:164`
  (`ErrTooLarge`)
- **Finding:** Today every step is on one goroutine: the stage's iterator yields, the dispatcher
  yields the event, the body returns, the output is appended. Nothing is in flight, which is what
  the stream tests measure and what `Pool` promises ("a worker holds at most one event").
  The target needs four things the code has no place for. (1) An ordered queue: an output's
  event waits for its put, so the walk keeps pulling the stage while puts run and yields events
  from the queue's head, on its own goroutine, since a range-over-func `yield` can't be called
  from a put's goroutine. (2) A drain rule: a found part's events follow the outputs before it,
  so the queue drains before the walk recurses. Concurrency therefore exists only across runs of
  consecutive outputs (a PDF's pages, a video's frames) and is 2 for a store's
  record-and-Markdown pairs between attachments. (3) Two reads of every file output: `Put` takes
  the SHA-256 as its key, so the file is hashed, then read again by the put. A text or vision
  output that is the part's own file (text.go:139, vision.go:58) would be read a third time
  though its hash is the part's. (4) A failure class: a put that fails is neither a part's
  failure nor, always, a reason to stop. go-storage refuses an object over its store's
  `MaxObjectSize` with `ErrTooLarge`, and the text stage passes a part of any size through as its output; treated as
  the index's failure is (fatal, redelivered), that input is redelivered until its deliveries
  run out.
- **Proposed design:** State the pipeline in `layers.md`: per part being prepared, a queue of at
  most `Config.Puts` outputs; an output is hashed on the walk's goroutine (its part's SHA-256
  reused when `Output.Path` is the part's path), put on a put goroutine, then appended and
  yielded in order from the walk's goroutine; the queue drains before a found part is
  dispatched and before the part's own event; a stopped loop or ended context waits for the
  puts in flight before `Dispatch` returns. A `Put` error matching a new `dispatch.ErrTooLarge`
  (the provider maps go-storage's) fails the part, recorded as PD-11 proposes; any other stops
  the dispatch, as an index failure does. The stream tests gain the bound: heap and disk hold
  `Puts` outputs, not one.
- **Carried by:** library

### PD-4 · A lease must outlast the caller's loop body, and a lost claim has no way out of `staged`
- **Kind:** issue
- **Weight:** high
- **Where:** dispatch/dispatch.go:500-625 (claim at 513, held through `emit` at 624), 590-599;
  dispatch/pool.go:118-131; dispatch/file.go:33-37
- **Against:** layers.md, "Index" ("the dispatcher renews it on a timer, at a third of the
  lease, for as long as the part's stage runs … Its dispatch then stops the part's stage,
  records nothing, and waits for the new claimant's entry")
- **Finding:** A claim is held from `Claim` until `Record` runs inside `emit`, after the body
  returns for the part's own event. In between sit every yield of the part's subtree, so the
  claim's lifetime includes the caller's loop body: docinfer's admission, a slow consumer, and in
  `Pool` the wait on `it.done`. Every ancestor's claim is held the same way, so a `.pst`'s claim
  lasts the whole store. Renewal "for as long as the part's stage runs" is therefore too short,
  and a timer on the walk's goroutine never fires while the body blocks it. A lost claim is
  harder still. `staged` has three exits (loop stopped, context ended, outcome known), and it
  treats any context error after the stage as the dispatch's own, fatal (dispatch.go:594-599).
  Stopping one part's stage needs a context per part; an ancestor's lost claim must stop the
  descendants being prepared under it; and by then the part's events are out, so "waits for the
  new claimant's entry" replays them: the same part's outputs twice in one dispatch.
- **Proposed design:** One renewer goroutine per dispatch holds the claims in flight (the
  ancestor chain) and renews each at a third of its lease from `Claim` to `Record` or `Release`,
  whatever the walk's goroutine is doing; it ends with the dispatch. A renewal the index refuses
  ends the dispatch with an error matching a new `dispatch.ErrClaimLost`, which a host treats as
  transient: the redelivery waits on the new claimant and replays. A claim is lost only when a
  process stalls past its lease, so unwinding in place buys nothing a redelivery doesn't, and it
  costs a context tree and duplicate events. `layers.md`'s "stops the part's stage, records
  nothing, and waits" becomes "ends the dispatch with `ErrClaimLost`".
- **Carried by:** library

### PD-5 · `Event.Keep` has no caller, and nothing the loop body does reaches the index any more
- **Kind:** simplification
- **Weight:** medium
- **Where:** dispatch/dispatch.go:81-93, 199-200, 243-329 (`emit`, `remove`, `clean`), 662-681;
  dispatch/adapter.go:131-136; dispatch/archive.go:295-301; dispatch/doc.go:238-262;
  root: internal/docproc/rundir/rundir.go:68-106, internal/docproc/run/execute.go:149-157
- **Against:** none: simplification (it removes `func (e Event) Keep()` from layers.md,
  "Dispatch", and "keeps a quarantined part's file through `Keep`" from "The nested modules")
- **Finding:** `Keep` is called only from tests (`grep '\.Keep()'`: six sites, all tests). The one
  shipped caller copies: `Dir.Store` copies an output's file and `Dir.Quarantine` copies a
  quarantined part's, both inside the loop body, while the file is there. The dispatcher already
  tolerates a file the body moved ("A file the caller moved away is not missed",
  dispatch.go:279), so a caller that wants a file renames or links it in the body. `Keep` costs
  the `keep *bool` in `Event`, the `kept` map, `clean`'s walk around kept paths (40 lines), the
  `!keep` clause of `settled`, the stash's and the archive stage's kept-file cases, and three
  tests. With `Ref` gone and `Path` never marshalled, the other reason the dispatcher waits for
  the body is gone too: the index records nothing the body can change, so "as the body left it"
  leaves the contract, and the body's return gates only the files' removal.
- **Proposed design:** Drop `Keep`. An event's files are the caller's to read, link, or move
  until its body returns, and the dispatcher removes what is left; a quarantined part's file is
  removed like any other. `cmd/docproc` keeps copying a quarantined file in the body, as it
  does. The dispatcher appends an output before it yields its event (after its put), and records
  a part's entry before it yields the part's own event, so an attempt stopped at that event
  still leaves the entry; `Pool`'s hand-back stays, for the files alone.
- **Carried by:** library (and docproc, for `run`)

### PD-6 · Flags can leave the `Index`: an entry carries its part's flags
- **Kind:** simplification
- **Weight:** medium
- **Where:** dispatch/dispatch.go:415-425, 479, 637, 688; dispatch/index.go:58-62;
  dispatch/memory.go:194-207; dispatch/file.go:536-555; inspect/inspect.go:280
- **Against:** layers.md, "Index" (`Flags`, `RecordFlags`) and "Dispatch" ("Whether a changed
  policy re-inspects seen content is the `design` task's")
- **Finding:** `Index.Flags` has two readers: `seen`, for the replaying part, and `releasedPart`,
  for a file-less part whose route has no stage. `RecordFlags` is written for every inspected
  part, including parts with no stage and quarantined parts, which record no entry and are
  inspected again by every dispatch, so those writes are never read. In the target a replayed
  child's flags arrive in `Child.Part.Flags`; only the replaying part's own flags still need the
  index. Carrying them in its `Entry` removes two interface methods, a table from
  `docproc/postgres`, `flags/` from `FileIndex`, one write per part with no stage, and one read
  per replayed part. It also answers the open question cheaply: `inspect`'s verdict is a pure
  function of flags and options (`Options.verdict`, unexported), so a seen part's recorded flags
  can be weighed under the current policy without reading its file.
- **Proposed design:** `Entry` gains `Flags []inspect.Flag`; `Flags` and `RecordFlags` leave the
  `Index`; a file-less part's flags ride in the dispatch's `released` record. `inspect` exports
  the verdict (`Options.Verdict(flags)`). On a seen part, recorded flags that now come to
  quarantine make the part Quarantined, with no replay; a policy relaxed since changes nothing
  until the entry ages out. A new check applies to content prepared after it ships. `layers.md`
  states this in place of the open question.
- **Carried by:** library

### PD-7 · `FileIndex`'s cross-process claims are the part of it that can't meet the lease contract
- **Kind:** simplification
- **Weight:** medium
- **Where:** dispatch/file.go:23-66, 238-379 (claim files, `readClaim`, `reclaim`, `Wait`'s
  poll), 393-416 (`appendLine` checks no token), 445-507; dispatch/doc.go:319-323;
  root: internal/docproc/rundir/rundir.go:50
- **Against:** layers.md, "Index" ("every implementation honors one contract … from then on the
  first token's `Renew`, `Append`, and `Record` fail"; "`FileIndex` … stays for tests and
  `cmd/docproc`")
- **Finding:** `FileIndex` serves processes sharing a directory: a claim is an `O_EXCL` file
  with a time, a waiter polls it, and a claim older than `Stale` (30 minutes by default) is taken
  over by rename. Its own doc says a takeover "is a read then a rename or remove, not one atomic
  step: two claimants may both come to hold the key", and that the later record replaces the
  earlier. `Append` never checks the token, and a stale claimant that appended nothing before
  the takeover records successfully after it. So the fencing `layers.md` requires of every
  implementation is not something files give; adding `Renew` (rewriting the claim file) widens
  the same race. Its one user is `cmd/docproc`, one process per run directory, which pays for
  the design instead: after a killed run, the next run waits out each leftover claim's `Stale`
  ("the resumed dispatch waits that out").
- **Proposed design:** `FileIndex` is single-process: its claims are `MemoryIndex`'s in-memory
  table (a mutex and a channel per claim), and only entries and logs are files, a log written
  beside its entry under a temporary name and renamed at `Record`. Opening it removes leftover
  temporary logs. A claim never expires while its process lives, so `Claim` returns a lease the
  dispatcher needn't renew (zero meaning none) and `Renew` succeeds. The claim files, `reclaim`,
  the poll, `DefaultStale`, `DefaultPoll`, and `FileOptions` go (about 150 lines and
  `TestFileIndexStale`). Its doc says two processes sharing a directory may each prepare the
  same content, the later record replacing the earlier whole, which is what it gives today
  around a takeover. Entries, logs, and `FileContent` shard their directories by the name's
  first two hex digits; today each is one flat directory, two or three files per part.
- **Carried by:** library

### PD-8 · A recorded entry that is no longer fresh can't be claimed
- **Kind:** issue
- **Weight:** medium
- **Where:** dispatch/memory.go:108-110; dispatch/file.go:254-256, 271-274;
  dispatch/dispatch.go:505-526
- **Against:** layers.md, "Dispatch" ("A part is seen when its entry's `Stages` each equal the
  current identity of the stage of that name") and "Index" (`Claim`, `Drop`)
- **Finding:** In the spike an entry found is an entry seen: the key holds the part's own stage
  identity, so a changed stage is a different key, and `Claim` refuses a recorded key in both
  implementations. `Entry.Stages` adds a third state, recorded but stale (a descendant's stage
  changed), under the *same* key. The claim loop has no path for it: `Lookup` finds it, and
  `Claim` returns false. Two smaller gaps sit beside it. "The stage of that name" needs a map
  from `Identity.Name` to the wired stage, which `Stages`, keyed by route, doesn't give, and
  nothing stops two routes wiring different stages under one name. And replicas on two tool
  versions (a rolling deploy) each find the other's entry stale under the shared parent key and
  replace it in turn.
- **Proposed design:** A stale entry is dropped, then claimed: `Lookup` → not fresh → `Drop` →
  the claim loop. `New` builds the name-to-identity map and refuses two stages that share a name
  with different identities. `layers.md` states the sequence and accepts the rolling-deploy
  cost, bounded by the deploy.
- **Carried by:** library

### PD-9 · An append needs no durability before `Record`, so a provider may buffer the log
- **Kind:** simplification
- **Weight:** medium
- **Where:** dispatch/dispatch.go:556-559, 678; dispatch/file.go:393-416, 421-443;
  dispatch/index.go:46-51
- **Against:** layers.md, "Index" ("one row per log item … A claim, a renewal, an append, and a
  record are each one conditional statement")
- **Finding:** A claim's log is read only after `Record`; a `Release` or a takeover discards
  it. `FileIndex` already relies on that: `appendLine` writes through a `bufio.Writer` and
  nothing is flushed or synced until `Record`. Read literally, the provider's "one conditional
  statement" per append is one round trip to PostgreSQL per output, warning, and child: about
  three per message of a store, millions for a large `.pst`, each buying durability nothing
  reads.
- **Proposed design:** The `Index` contract says an append may be buffered and that `Record`
  commits the log whole or fails; a provider flushes in batches and at `Record`, and a lost
  claim surfaces at a flush, a `Renew`, or `Record`. `docproc/postgres` inserts log rows in
  multi-row statements. The dispatcher appends from the walk's goroutine only, so an
  implementation's pending log needs no lock of its own.
- **Carried by:** library

### PD-10 · Any failure on content flagged encrypted is recorded Encrypted, for good
- **Kind:** issue
- **Weight:** medium
- **Where:** dispatch/dispatch.go:608-611, 647-652; inspect/scan.go:22; dispatch/document.go:66-71
- **Against:** data-intelligence.md (at-least-once redelivery retries what failed);
  design.md, ON-16 (a tool's death by signal is recorded failed)
- **Finding:** `case errors.Is(stageErr, ErrEncrypted), encrypted(p.Flags)` turns *every* stage
  error into Encrypted when the part carries `pdf-encrypt`, `zip-encrypted`, or
  `ooxml-encrypted`, and Encrypted is complete, so the entry is recorded. `pdf-encrypt` flags any
  `/Encrypt` name, and the document stage renders a PDF "restricted only by its permissions" as
  any other. A full disk, an OOM kill, or a container error while rendering such a PDF is
  therefore recorded Encrypted and never retried until the stage's identity changes.
- **Proposed design:** A part is Encrypted only when its stage ends with an error matching
  `ErrEncrypted`. The archive and office stages already end with it, and the document stage
  does when qpdf is wired; without qpdf it relies on the fallback today, so it maps
  Ghostscript's password refusal itself. The fallback goes, with a test: a failing stage over a
  flagged part is Failed and unrecorded.
- **Carried by:** library

### PD-11 · A refusal the content decides is never recorded, so its ancestors never are
- **Kind:** issue
- **Weight:** medium
- **Where:** dispatch/dispatch.go:610-616 (`complete = false`), 575-577; internal/cli/cli.go:17-21
- **Against:** data-intelligence.md, "Caching" ("content seen before is replayed, not
  recomputed"); roadmap.toml, `library` summary (the PDF whose form read fails)
- **Finding:** Failed is never recorded, so a later dispatch retries it, and one Failed
  descendant keeps every ancestor unrecorded. That is right for a tool that died and wrong for
  a tool that refused the content: a corrupt attachment is refused identically on every run, and
  the `.pst` holding it is read whole by every later dispatch. The tools' contract already
  separates the two (`cli.ErrRefused`, exit 3, against `cli.ErrFailed`), and the dispatcher
  collapses them. PD-3's too-large output is the same class.
- **Proposed design:** A stage error matching a new `dispatch.ErrRefused` (the adapters wrap a
  tool's refusal with it) gives the part Failed with its reason, recorded, its subtree
  complete, so its ancestors record. Every other failure stays unrecorded and retried. A seen
  part recorded Failed replays as recorded.
- **Carried by:** library

### PD-12 · `Where` comes from untrusted names: IDs can collide, grow, or be refused
- **Kind:** issue
- **Weight:** medium
- **Where:** dispatch/zip.go:87, 118; dispatch/tar.go:90, 132; dispatch/sevenzip.go:149, 231;
  dispatch/document.go:250, 271
- **Against:** layers.md, "Dispatch" (`Part.ID`: "unique among siblings and stable across
  runs"; "`%` and `/` percent-encoded")
- **Finding:** An archive entry's and a PDF attachment's `Where` is a prefix plus the stored
  name, raw: `"zip entry " + f.Name`. A stored name may be invalid UTF-8, hold NUL and control
  bytes, and run to 64 KiB in a ZIP and longer in a tar. Three things break once `Where` chains
  into `Part.ID`. Uniqueness is checked on Go strings, but the ID travels as JSON, where
  `encoding/json` replaces each invalid byte with U+FFFD, so `a\xff` and `a\xfe` are distinct
  siblings with one ID on the wire. PostgreSQL refuses NUL in `text` and `\u0000` in `jsonb`,
  so `docproc/postgres` fails the append of such a child, which stops the dispatch. And an ID is
  every ancestor's `Where` joined, repeated in each event of the part.
- **Proposed design:** The dispatcher normalises a `Where` before its uniqueness rule: invalid
  UTF-8 and C0/C1 controls percent-encoded byte by byte, and a `Where` past a cap (say 255
  bytes) cut to the cap with the hex of its SHA-256's first 8 bytes appended. `Name` keeps the
  stored name but is made valid UTF-8 the same way. Tests: two names differing in one invalid
  byte, a NUL, a 64 KiB name.
- **Carried by:** library

### PD-13 · Three per-dispatch maps grow with a store, against "the memory of one at a time"
- **Kind:** issue
- **Weight:** medium
- **Where:** dispatch/dispatch.go:205-218, 340-342 (`run.released`); dispatch/adapter.go:74-75,
  104-112 (`stash.released`); dispatch/doc.go:26-29
- **Against:** layers.md, "Dispatch" (`Where` unique among siblings); the spike's own claim
  (doc.go:26-29); roadmap.toml's backlog (the Threader's 60 bytes a message)
- **Finding:** The package doc promises a store's stage "holds the memory of one at a time".
  Two maps already break it: `run.released` holds a record per distinct content the dispatch
  released, and `stash.released` holds each released original's path and SHA-256 (the path
  already ends in that SHA-256, `nameSum`), both for the whole store. `layers.md` adds a third:
  uniqueness among siblings needs every sibling's `Where`, and a store's messages'
  attachments are all siblings under the store. At a million attachments that is three maps of
  a million strings in each dispatch.
- **Proposed design:** Uniqueness is each stage's to give, stated in `Stage`'s contract; the
  mail, onenote, and office stages give it by construction (message id and attachment number,
  page id and medium, media name). The dispatcher keeps the sibling set only for a stage that
  declares its names untrusted (archive, and the document stage's attachments), whose siblings
  `Limits.Parts` already bounds. `stash.released` becomes a set of SHA-256s, or goes: a missing
  file whose name is a SHA-256 is found file-less, and the dispatcher's `released` decides. The
  stream tests state the remaining growth (one `released` record per distinct content) as the
  bound it is.
- **Carried by:** library

### PD-14 · Release and Record run under `context.WithoutCancel` with no bound
- **Kind:** issue
- **Weight:** medium
- **Where:** dispatch/dispatch.go:530-534, 618
- **Against:** Go Elemental, lifecycle-and-context ("every layer runs against a fresh
  timeout-bounded context, so cleanup is not pre-cancelled"); timeouts (each bound has one owner)
- **Finding:** A dispatch whose context ended still releases its claims, rightly, under
  `context.WithoutCancel(r.ctx)`, and records under the same. Against a file or a map that
  returns at once. Against PostgreSQL it is a statement with no deadline issued while the
  process drains, once per claim in the ancestor chain: a database that stalls holds the drain
  until the host kills it.
- **Proposed design:** Cleanup is best effort and bounded: `Release` after a cancelled context
  runs under a timeout the dispatcher owns (a `Config` field, defaulting to a few seconds), and
  its failure is dropped, since the lease expiring releases the claim anyway. `Record` runs
  under the dispatch's own context: an ended context records nothing and releases.
- **Carried by:** library

### PD-15 · A replay can't tell a truncated log, and `Limits` on a replay are unstated
- **Kind:** issue
- **Weight:** low
- **Where:** dispatch/dispatch.go:693-706 (`Outputs: n`, never compared with `e.Outputs`),
  97-104, 387-388; dispatch/file.go:177-198, 499-504
- **Against:** layers.md, "Index" (`Items`, `Drop`) and "Dispatch" (`Limits`)
- **Finding:** `seen` counts the outputs it replayed and reports that count; it never compares
  it with the entry's. `FileIndex` survives a concurrent replacement because a reader keeps its
  open file. A provider paging rows doesn't: a `Drop` or a re-record under a new token
  mid-replay ends `Items` early with no error, and the replay reports a short part as whole.
  Separately, a seen part counts once against `Limits` today, since its children aren't walked;
  once a replay recurses, whether replayed descendants count toward `Parts`, `Bytes`, and
  `Depth` (a subtree replayed deeper than it was prepared) is undefined.
- **Proposed design:** `Items` fails when its entry is replaced or dropped while it reads (the
  provider reads under the entry's token), and the dispatcher checks the replayed outputs and
  children against `Entry.Outputs` and `Entry.Children`, a mismatch ending the dispatch as an
  index failure. Replayed parts count toward `Limits` as prepared ones do; a replayed part past
  a cap is Limited, and its entry stays.
- **Carried by:** library

### PD-16 · The adapters are a second package inside `dispatch`
- **Kind:** simplification
- **Weight:** medium
- **Where:** dispatch/{adapter,archive,zip,tar,sevenzip,audio,video,document,email,office,
  onenote,picture,vision,text}.go (3,009 lines; eight files `//go:build linux`);
  dispatch/doc.go:1-7; root: internal/docproc/stages/stages.go
- **Against:** none: simplification (it changes layers.md's package table: `dispatch` and
  `stages`)
- **Finding:** `dispatch` holds two things with one-way dependence: the walk, index, content,
  and shapes (2,347 lines, standard library plus `route` and `inspect`, any platform), and
  fourteen files of stage adapters that between them import every format package and `tool`,
  eight building on Linux only. No format package imports `dispatch`, and the adapters' only
  caller outside tests is the spike root's `stages`, which calls each constructor once. Of the
  71 identifiers `go doc -short` lists for the package, 38 are the adapters' (`XStage`,
  `XConfig`, `NewXStage`, `QPDF`, `PDFDetach`, `SevenZip`, `Versions`, the plan types).
  docinfer, `docproc/storage`, and `docproc/postgres` import `dispatch` for its shapes and interfaces and compile all of it.
- **Proposed design:** The adapters move into `stages`, beside `Build`, which is their one
  caller: `dispatch` becomes the walk, the stores, and the shapes, with no build tag. In
  `stages` the adapter types and constructors can go unexported behind `Build`, its tests
  beside them. The audio and video plan records, which docinfer decodes, move to `audio` and
  `media`, beside `Seam`. `HashConfig` stays in `dispatch` for a host's own stages.
- **Carried by:** library

### PD-17 · The providers need the index contract as an importable suite
- **Kind:** issue
- **Weight:** medium
- **Where:** dispatch/index_test.go:19-33, 116-352 (`TestIndexContract`), 354-407;
  dispatch/stage_test.go:33-309
- **Against:** layers.md, "Index" ("every implementation honors one contract"); go-storage's
  `storagetest/suite.go` and go-messaging's `messagingtest`
- **Finding:** The contract is proven by `TestIndexContract`, a table over `indexes()` in
  package `dispatch_test`, and the concurrent-claim proof by `TestConcurrentDispatch` over the
  same table and the test fixture's stages. `docproc/postgres` and `docproc/storage` are other
  modules: they can import neither, so each would restate the contract, and the lease rules
  (takeover, fencing, discarded items) would be proven per provider or not at all.
- **Proposed design:** A public `dispatch/dispatchtest` holds `Index(t, new)` and
  `Content(t, new)` conformance suites, as go-storage's `storagetest` does, with the fixture
  stage the dispatch tests share (`container`), and each takes a constructor. `MemoryIndex`,
  `FileIndex`, `MemoryContent`, `FileContent`, and both providers run them; docinfer's `job`
  tests use the fixture stage.
- **Carried by:** library

### PD-18 · The test tiers don't divide where layers.md divides them
- **Kind:** issue
- **Weight:** medium
- **Where:** dispatch/tier_integration_test.go:18-34; dispatch/findings_test.go (1,353 lines),
  findings_formats_test.go (837), findings_ink_test.go (198); internal/corpustest/doc.go:1;
  internal/textscore/speech.go; document/ghostscript_integration_test.go
- **Against:** layers.md, "docproc's packages [library]" (`internal/goldentest`, `mediatest`,
  `textscore`, `corpustest` "kept: test helpers that call no model"; the model tests "moved to
  docinfer"); the goal record's decision "no measurement scripts committed"
- **Finding:** Three things don't fit the table. (1) `TestTierRoutes` is the library's one
  whole-fixture test: every committed fixture through the real stages, held to the hand-written
  `testdata/tier-routes.tsv`. It imports the client and `visiontest` only to send a sample of
  its images to a model, so "moved with the tests that call a model" takes the route truth out
  of docproc. (2) The tests that move to docinfer import `textscore` and `corpustest`, which an
  `internal/` path bars from another module. (3) Most of what the two helpers serve isn't
  library testing. `corpustest` builds under `findings` alone, and the findings and bake-off
  tiers (6,553 and 2,551 lines repository-wide, 2,388 in `dispatch`) are the spike's measurement
  harnesses over ~/docs and uncommitted stores, whose results are already written up in
  `docs/findings/`. Without the model tests and those tiers, `textscore`'s speech half
  (`speech.go`, 239 lines, with 311 of tests) has no caller, and its text half has one, the
  text-layer check in `document/ghostscript_integration_test.go`.
- **Proposed design:** `TestTierRoutes` splits: the route and outcome truth stays in docproc
  with no model, and the image sampling moves to docinfer. The findings and bake-off tiers stay
  in the spike as its evidence; `acceptance` reruns what it needs through `cmd/docproc`.
  `corpustest` and `textscore`'s speech half don't move to docproc: docinfer takes a copy of
  what its model tests score with. `textscore`'s three text functions move beside their one
  caller in `document`'s tests. `goldentest` and `mediatest` stay.
- **Carried by:** library (and the docinfer goal)

### PD-19 · Warnings a stage makes itself, and a store's warnings, have no place in `Warning`
- **Kind:** issue
- **Weight:** low
- **Where:** dispatch/text.go:176-181; dispatch/office.go:107-109; dispatch/email.go:110-114;
  dispatch/onenote.go:92-96; dispatch/archive.go:218-232
- **Against:** layers.md, "Dispatch" (`Warning{Tool, Where, What}`, "parsed from the envelope's
  stderr line") and the change list ("in place of a warnings record output")
- **Finding:** The change list names the `warnings` record of mail and onenote. Two stages
  carry warnings another way, as `Meta["warnings"]`, lines joined by a newline: the text stage's
  own (the windows-1252 fallback, the U+FFFD count), which no tool wrote, and office's. And mail
  and onenote yield their warnings once, after the last record, since stderr and stdout are two
  pipes: they follow the store's last `After`, so a resumed preparation carries only what the
  tail's read warned of (mail writes no warnings for the messages it passes over), while a
  resumed replay carries them all.
- **Proposed design:** Every warning is an `Item.Warning`; `Meta["warnings"]` goes. A warning a
  stage makes itself names the stage in `Tool` (`text`). `layers.md` states that a tool's
  warnings follow its outputs, and that a resumed store's warnings are its tail's.
- **Carried by:** library

### PD-20 · The archive stage takes its own copy of the inspection options
- **Kind:** simplification
- **Weight:** low
- **Where:** dispatch/archive.go:40-52, 150-155, 165-183; dispatch/document.go:108, 252 (the
  pdfdetach bomb rule); dispatch/dispatch.go:118-119
- **Against:** roadmap.toml, `library` backlog ("dispatch's bombRule validates its options
  through `inspect.CheckListing(route.Zstd, 0, nil, opts)`"); layers.md, "Dispatch"
  ("`inspect.Options` is in no stage's identity")
- **Finding:** `ArchiveConfig.Inspect` "should be the dispatcher's", and nothing makes it so: a
  host can wire one policy into `Config.Inspect` and another into the stage. `bombRule` checks
  the options by calling `CheckListing` over an empty listing, the backlog's indirect call, and
  restates inspect's defaulting. The stage also hashes the bomb thresholds into its identity,
  which the sentence in `layers.md` denies.
- **Proposed design:** `inspect` exports the options check and the resolved thresholds
  (`Options.Check() (Options, error)`, defaults filled). `stages.Build` takes the options once
  and gives them to the archive and document stages and to the caller for `Config.Inspect`.
  `layers.md` says the bomb thresholds are in the archive and document stages' identities, and
  the policy in none.
- **Carried by:** library

### PD-21 · `FileOptions.Now` is a clock abstraction, and a claim's time is written in local time
- **Kind:** issue
- **Weight:** low
- **Where:** dispatch/file.go:40-41, 109-111, 260, 314-316
- **Against:** Go Elemental, utc-times ("The standard has no clock abstraction"; a time that
  leaves the code is converted with `.UTC()`)
- **Finding:** `FileOptions.Now` exists so the tests can age a claim, and `claimFile.Time` is
  `f.now()` marshalled as it comes, in the host's zone. PD-7 removes both. `FileContent`'s renew
  age, new, reads a file's modification time and needs neither a clock field nor a stored time.
- **Proposed design:** No store takes a clock. `FileContent`'s tests age a content with
  `os.Chtimes`; any time a store returns or writes is `.UTC()`.
- **Carried by:** library

### PD-22 · dispatch's shapes become a wire contract with no committed output
- **Kind:** issue
- **Weight:** low
- **Where:** dispatch/stage.go:44-48, 175-183; dispatch/index.go:67-93;
  internal/goldentest/goldentest.go:61-102
- **Against:** the goal record's decision "contracts proven by committed golden outputs";
  layers.md, "Dispatch" (`Version = "1"`, "grown by addition only")
- **Finding:** `Event`, `Part`, `Output`, `Warning`, `Entry`, and `Recorded` become what Cerebro
  publishes, what `docproc/postgres` stores, and what `rundir` writes, yet the spike has no test
  of their JSON: the shapes were the spike root's `schema`. `goldentest` can't serve: it commits
  SHA-256 sums, so a failure names a key and shows no difference.
- **Proposed design:** `dispatch` commits one golden JSON Lines file of a dispatch over the
  fixture stage (each event kind, each outcome, a replay) and one of an entry's log, compared as
  text. `goldentest` stays for what it is used for, PNG bytes.
- **Carried by:** library

## For the synthesis

- **A dispatch may repeat events.** PD-1's miss and any redelivery make a part's events
  at-least-once. docinfer's `Done` counts, Cerebro's manifest counts, and `job`'s watermark
  should all be defined over part IDs and message cursors, never over events seen.
- **A stopped attempt discards the parts in flight.** A loop stopped mid-part releases the
  claims of the whole ancestor chain and their logs. A 1,823-page PDF or a 4.4 h audio file
  stopped by a 429 is prepared again from its first page on the redelivery; only a store has a
  cursor. This is the spike's rule kept, and worth a sentence in data-intelligence.md's
  redelivery cost.
- **Refused against failed** (PD-11) starts in group B's readers: the adapters can wrap a
  refusal only if each reader keeps `cli.ErrRefused` distinct through its own error type.
- **The inspection options have three owners** (PD-6, PD-20): the dispatcher's `Config`, the
  archive and document stages, and group A's `inspect`. One exported check and one verdict
  function in `inspect` serve all three.
- **Fixtures in place.** The dispatch tier reads the format packages' fixtures "in place"
  (`dispatch/testdata/README.md`), and `email/testdata` is a copy of mail's refreshed from its
  tag. With the tools nested under `tools/<format>`, the copies and the refresh task can go, the
  Go tests reading `tools/<format>/fixtures`.
- **`encoding/json/v2`** marshals every record output (`dispatch/stage.go:188`) while
  `HashConfig` and the index use v1. The library should pick one for its wire shapes, since
  PD-22's goldens pin the bytes.
- **The package doc's "Resuming a store"** (`dispatch/doc.go:278-325`) tells a caller to save an
  `email.Threader` with its cursor. `layers.md` moves threading to the consumer, so the section
  is rewritten around `Event.After`, and the Threader's memory (the roadmap's backlog) is the
  consumer's question.
- **Constructors.** `dispatch.New` and every `NewXStage` return an error for a nil required
  field, where construction-and-execution has a wiring mistake panic by name; the goal record
  leaves the choice to `library`, and it should be made once for every package of the module.
