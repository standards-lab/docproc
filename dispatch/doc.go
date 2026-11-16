// Package dispatch walks an input and every part found in it to the stages that prepare them, on
// the standard library and this module's route and inspect packages and sevenzip's types,
// golang.org/x/text for the text stage's decoding, and, for the format packages' adapters, on
// document, office, picture, email, onenote, audio, media, and tool. It
// never calls a model: it reports what each part is and what its stage prepared, for a layer above
// to send on. The stages are the caller's, registered by route; the format packages' adapters are
// stages.
//
// [Dispatcher.Dispatch] takes an [Input], a file the caller owns and the dispatcher never
// removes, or a stream it spools, and yields [Event]s, depth-first, in each stage's own order:
// each output a stage prepares is an output event, its Output set, and each warning a warning
// event, yielded in the stage's order, and each part a stage finds is dispatched as it finds it,
// its outputs' events and every part found in it, then its own event, before the stage's next
// item. A part's own event follows all of that, once its outcome is known, so the input's event is
// last. A part is a file: its SHA-256, streamed, and size are taken before any stage runs, a
// claimed SHA-256 checked against it, its route by route.File, its content's flags by
// inspect.Content and its name's by inspect.Name, and it names its occurrence by its ID, its
// parent's ID, its depth, and where its parent's stage found it.
//
// # Events
//
// An output event carries the part it belongs to, the identity of its stage, and the [Output],
// its SHA256 filled and its content already in the content store, and a warning event its
// [Warning], what of the part read badly. A part's own event carries its outcome and why, the error a failed part came to, its stage's identity, how
// many outputs it yielded, in Outputs, and how many parts it found, in Children; never the outputs
// or the parts themselves, whose events the caller has had. Once the body returns for an output
// event, the dispatcher holds nothing of the output but what it appended to the index, and once
// a found part's event is done, nothing of the part but the [Child] reference it appended, so a
// stage that yields millions of outputs and parts, a store's messages and attachments, holds the
// memory of one at a time, the caller's loop owning each as it comes.
//
// # Outcomes
//
// A part comes to one [Outcome]:
//
//   - [Prepared]: its stage prepared it; its outputs' events came before its own, which counts
//     them and references the parts it found.
//   - [NoProcessor]: its route has no stage registered, or its stage ended with
//     [ErrNoProcessor], as office's does for a kind it has no processor for yet.
//   - [Seen]: the index already holds its content's fresh entry under its stage's identity,
//     recorded by this dispatch, an earlier one, or a concurrent one this dispatch waited on. Its
//     log is replayed, streamed one item at a time, with no file: each output and warning an event
//     with the Outcome Seen and the entry's outcome in Recorded, an output carrying its SHA256 alone,
//     and each child dispatched as its recorded part at its place in this input; then its own event
//     carries the entry's outcome, in Recorded, its counts, and its flags. The part is neither
//     inspected nor prepared again.
//   - [Cycle]: its SHA-256 is an ancestor's; it is neither inspected, prepared, nor recursed
//     into.
//   - [Quarantined]: its flags' verdict under the dispatcher's inspect.Options quarantines it,
//     its flags holding the evidence: its content's and its name's before its stage runs, and no
//     stage runs; or a flag its stage raised before its first output, which stops the stage.
//   - [Encrypted]: its stage ended with [ErrEncrypted]; Err holds the stage's error. A flag decides
//     no outcome: content flagged encrypted whose stage fails is Failed.
//   - [ReadElsewhere]: its stage ended with [ErrReadElsewhere], its content read as other parts.
//   - [Failed]: the part couldn't be read, routed, inspected, or prepared, its claimed SHA-256
//     wasn't its content's, or an output was past the content store's size; Err says why. A stage
//     that fails after yielding outputs and finding parts has had those outputs yielded and those
//     parts dispatched.
//   - [Limited]: the part is past a cap the caller set in [Limits], on the parts, the bytes, or
//     the depth one dispatch admits; it is neither inspected nor prepared, and its siblings go
//     on. No cap applies by default.
//
// An error that stops the whole dispatch is yielded alone, last: the input unreadable, or its
// SHA-256 not the one claimed, matching [ErrSHA256]; a failure of the index or the content store,
// a [*StoreError], the store's rather than the input's, so a host stops its run on it; a claim
// the index no longer holds, matching [ErrClaimLost], transient; a file the dispatcher can't
// remove; or the context ending.
//
// # Stages
//
// A [Stage] names its [Identity], its name, its tool's version, and its configuration's hash,
// and prepares one part: Prepare yields [Item]s, each an [Output] it prepared, a [Warning], a flag
// it raises about the part, or a [Found] part, a file it wrote into the directory the dispatcher
// made for the part, or an unread child with no file and an outcome. An Output is text, Markdown,
// an image, a record, or audio, held in a file or inline, with the stage's metadata; a store's
// stage sets [Item.After] on a message's last output, the message's cursor. A stage runs on a
// goroutine of its own, each item handed to the walk and taken before the stage goes on, as a
// yield is; an output's file and bytes are the dispatcher's from its yield until the part's own
// event.
//
// Each stage names where it found a part, [Found.Where], stably across runs. The dispatcher
// normalises it, as every wire string, and keeps it unique among its siblings for a stage that
// implements [UntrustedWhere], whose names come from the content; any other stage gives that
// uniqueness itself.
//
// # Adapters
//
// Each format package has an adapter here, a Stage built over the reader or converter the
// caller already built, its Tools the caller's, and the version of those Tools, which [Versions]
// names: no adapter constructs a tool. An adapter's identity is its name, that version, and a
// hash of the options that change what it prepares, with the adapters' own version, so a change
// to an adapter prepares content again.
//
//   - [DocumentStage], for route.Document: a PDF's pages through document's Preparer, each
//     page's text layer a text output and its image a PNG, under the request the caller gives.
//     Given qpdf, through its [QPDF], a PDF that needs a password ends with [ErrEncrypted],
//     nothing rendered, one restricted only by its permissions rendering as any other, and a PDF
//     whose AcroForm has fields yields a "forms" record of them, a document.Form, before its
//     pages. Given pdfdetach, through its [PDFDetach], each embedded file, the name tree's and
//     the file attachment annotations', is written under its SHA-256, never its stored name, and
//     found "attachment <name>", the bomb rule enforced on the bytes written, as the archive
//     stage enforces it, its check inspect.ArchiveBomb; the media behind a Screen or
//     Rendition annotation isn't extracted.
//   - [OfficeStage], for route.Office: office's Markdown, and each medium office wrote, a
//     converted picture as its PNG, found as a part, an RTF converted to docx first, as a legacy
//     document is. ODF, WordPerfect, an Excel binary workbook, and a legacy document or an RTF
//     without LibreOffice have no processor yet; an encrypted OOXML package, which office
//     refuses, ends with [ErrEncrypted].
//   - [PictureStage], for route.VisionConverted: a TIFF to a PNG per page, "page N", its Meta
//     naming the page and the count, and an EMF, WMF, SVG, HEIC, WebP, or AVIF to one PNG,
//     through picture's Converter. A picture whose tool the Converter lacks, or a HEIC, WebP, or
//     AVIF whose reader its magick lacks, has no processor yet.
//   - [VisionStage], for route.Vision: a PNG, JPEG, GIF, or BMP passed through as an image
//     output, so every picture reaches a layer above as one.
//   - [EmailStage], for route.Nested: mail's records, each a record output, and each
//     attachment's file found as a part, where its message and place say. A message's
//     attachments come first, each found part dispatched whole, then its record, then its
//     Markdown body, which the record's Meta "markdown" says follows; an embedded message mail
//     gives no file for is prepared inline, in its place among the attachments, its own
//     attachments found below it, at any depth. A message of a conversation class is a
//     conversation record, email's Conversation, then its transcript's Markdown, as a message is.
//   - [OneNoteStage], for route.OneNote: onenote's records, a page's record followed by its
//     Markdown, and each of its media found as a part, "page <id>, <where>"; a medium onenote
//     wrote no content for is a record.
//   - [AudioStage], for route.Audio: a WAV, FLAC, MP3, Ogg, or M4A through audio's Splitter.
//     The part's plan comes first, a record holding an [AudioPlan], then each window in plan
//     order, an audio output holding a 16 kHz mono WAV, its Meta its index, span, cut, and seams.
//     The stage transcribes nothing: a layer above transcribes each window and joins the
//     transcripts with audio.Join. A source with no audio stream fails, its error wrapping
//     audio.ErrNoAudio, and a failed window fails the part once the other windows are yielded. A
//     caller with no ffmpeg and ffprobe has no Splitter and registers no audio stage, so its
//     audio parts have no processor yet.
//   - [VideoStage], for route.Video: an MP4, Matroska, WebM, or AVI through media's Streamer, one
//     unpaced ffmpeg run per part, over a hard link to the part's file in the part's directory,
//     so media's directory of frames is made there and never beside the caller's input. The
//     part's plan comes first, a record holding a [VideoPlan], the settings its windows are
//     planned and its frames sampled under; then each frame, an image output holding a PNG, its
//     Meta its index, time, reason, and size, and each window, an audio output holding a 16 kHz
//     mono WAV, its Meta the audio stage's but its count, with the threshold it was cut under,
//     interleaved as the run completes them. A video with no audio stream yields frames only, and
//     one with no video stream windows only; a source with neither fails, its error wrapping
//     media.ErrNoMedia. The stage runs no model. A caller with no ffmpeg and ffprobe has no
//     Streamer and registers no video stage, so its video parts have no processor yet, as is
//     every part whose route has no stage registered.
//
// Where a stage yields a record and its Markdown, per message or page, the record comes first,
// then the Markdown of the same name.
//
// [ArchiveStage], for route.Archive, is no format package's adapter: it reads a ZIP, a tar, and
// a gzip stream on the standard library, on any platform, and, given 7-Zip through its
// [ArchiveConfig]'s [SevenZip], a sevenzip.Reader, a zstd stream, a 7z archive, a cabinet not
// named .onepkg, and a ZIP entry of a method archive/zip doesn't read. A ZIP is read through
// archive/zip, one entry at a time, each file entry written into the part's directory under its
// content's SHA-256 and found as a part, named by its stored name, "zip entry <name>"; the stored
// name is never a path. A directory's entry is no part. A stored, deflated, or bzip2 entry is
// decompressed by the standard library, bzip2 through compress/bzip2; an entry of another method,
// such as Deflate64, implode, LZMA, xz, zstd, or PPMd, is extracted alone through 7zz's standard
// output, under the same rules. An encrypted entry is a record, recorded encrypted, and the ZIP,
// once its other entries are dispatched, [Encrypted]. A tar is read through archive/tar the same
// way, each regular file found "tar entry <name>", a directory no part, and a link, a device, or a
// FIFO a record, no link or node made. A gzip stream is decompressed through compress/gzip into
// one part, named by its header's name, else by the stream's own name without its .gz, or with a
// .tgz's .tar, found "gzip content <name>" and routed again, so a .tar.gz is a gzip part holding a
// tar part; a zstd stream, through 7zz, the same way, named by its own name without its .zst, or
// with a .tzst's .tar, found "zstd content <name>". A 7z's and a cabinet's files are extracted one
// at a time through 7zz, found "7z entry <name>" and "cabinet entry <name>"; 7zz writes only to
// its standard output, never to a path.
//
// Before anything of a 7z, a cabinet, or a zstd stream is extracted, 7-Zip's listing is checked
// by inspect.CheckListing against the rules inspect holds a tar to, a traversing, absolute, or
// drive letter name, a link, a device, and an entry past the bomb rule by its declared size, and a
// listing's flags are yielded as the part's own, which the dispatcher weighs under its
// inspect.Options: a verdict to quarantine stops the stage, and the part is [Quarantined], the
// listing's flags its evidence, as inspect would have quarantined it. An encrypted 7z entry is a record and
// the 7z [Encrypted] once its other entries are dispatched, and a 7z whose headers are encrypted
// is Encrypted at once.
//
// The stage enforces inspect's bomb rule again on the bytes it writes, or reads from 7zz, the rule
// of those inspect.Options, which should be the dispatcher's, none when they set none: an entry
// or stream past it is stopped, its partial file removed, and recorded quarantined, with its evidence,
// whatever its header or listing declared; the ratio is a ZIP entry's against its compressed
// size, a tar, 7z, or cabinet entry's against the whole archive's, and a gzip or zstd stream's
// against the stream's. With no 7-Zip, a zstd stream, a 7z archive, or a cabinet has no processor
// yet, and a ZIP entry 7-Zip would read is a record. Its identity is "archive", this package's
// version of it, and a hash of its bomb rule and its 7-Zip's version. A ZIP or tar with a
// traversing, absolute, or drive letter name, a ZIP's symbolic link, or a tar's link or device,
// is flagged active by inspect, and proceeds unless a policy quarantines it; the stage writes
// each entry by its hash, inside the part's directory, as it does a 7z's or a cabinet's, and
// records a link or device, no link or node made.
//
// [TextStage], for route.Text, is no format package's adapter either, and runs no tool, on any
// platform: it yields one [TextOutput], "text", UTF-8, its line endings as stored. Its encoding
// is decided, never detected: a byte order mark, UTF-8's, UTF-32's, or UTF-16's, either byte
// order, which the output drops; else UTF-8, when the whole part is valid UTF-8; else
// windows-1252, as a fallback, with a warning. It decodes through golang.org/x/text. Its Meta
// "encoding" names the encoding and how it was decided, as "utf-8 (byte order mark)", "utf-8", or
// "windows-1252 (fallback)"; bytes the encoding can't decode become U+FFFD, which its Meta
// "warnings" counts. Valid UTF-8 without a mark is passed through, the output's Path the part's
// own file; other text is decoded, streamed, into a file in the part's directory. It sets no cap
// of its own: a large part is held to the dispatcher's [Limits].
//
// A skipped record, a medium or attachment with no content, an archive's entry or a gzip or zstd
// stream's content the archive stage can't write, a PDF's embedded file the document stage can't
// write, and a CLI's warnings are each a record output, never dropped.
// The email and onenote stages place each file they find in a directory of its own, a hard link to
// the file the CLI wrote, since the CLI writes each distinct content once, named by its SHA-256,
// for every record that names it, and the dispatcher removes a found part's file once its event is
// done. Each such part carries that SHA-256 and a release, as [Found] says, which removes the CLI's
// original once the dispatcher has settled its content; a later record naming it is then found with
// no file, and seen from the index.
//
// # The index
//
// An [Index] is content-addressed. It records a stage's [Entry] for content by a [Key], the
// content's SHA-256 and the stage's identity, so a new tool version or configuration prepares
// it again. A dispatch looks the key up, and on a miss claims it: one claimant prepares the
// content, and a concurrent dispatch that meets the claim waits until the claimant records the
// entry, then reports the part [Seen], or releases it, then claims it in turn. A part that fails
// releases its claim, so another dispatch may try again.
//
// An entry's outputs, warnings, and children are one log of [Recorded] items, appended as the
// part's stage yields them and read back by Index.Items in that order. The claimant appends each
// output and warning before its event is yielded, and each part the stage finds, as a [Child],
// the found part whole and its outcome, once the found part's own event is done; before the part's
// own event, Index.Record commits the log with the entry: its outcome and reason, its counts, the
// distinct stages its subtree ran, and its content flags. Index.Release discards the log when the
// part fails, is limited, or the loop stops, under [Config].Release's bound once the context has
// ended. A claim is a lease, which Claim returns: one renewer per dispatch renews every claim in
// flight at a third of its lease, the caller's loop body included, and a renewal the index
// refuses ends the dispatch with an error matching [ErrClaimLost]. Index.Drop removes an entry and
// its log; package dispatchtest holds the contract as conformance suites.
//
// An entry is recorded only for a part whose stage ran and whose whole subtree came to an outcome
// the content decides: a part with a [Failed] or [Limited] descendant, or that failed itself as
// the run's fault, is not recorded, so a later dispatch prepares it again and retries what failed;
// a stage's [ErrRefused] and an output past the content store's size are the content's, recorded.
// A part whose route has no stage records nothing, so a stage registered later prepares it in a
// later dispatch. A quarantine is recorded when the content's flags alone come to it.
//
// An entry is fresh when each of its Stages is the identity of the wired stage of that name; one
// that isn't is dropped and prepared again. A [Seen] part replays its log, and resolves its
// children one level down before its first event: each child recorded prepared, encrypted, no
// processor yet, read elsewhere, or seen is looked up under its route's current stage, and replays
// its own log. A child whose entry is gone or not fresh, or whose route has gained a stage, is a
// miss, which the nearest part with a file resolves: the input, or a part a stage found in this
// dispatch, drops its own entry and is prepared again, its stage finding its children again, the
// live ones replayed. A miss met after events are out is resolved the same way, so events are at
// least once: a consumer counts by a part's ID and a store's cursor, never by events. A replay is
// checked against its entry's counts, a mismatch the index's failure, and counts toward [Limits].
//
// A part's verdict is taken over its content's flags, recorded in its entry, and its name's, taken
// at every occurrence, under the dispatcher's current inspect.Options: a seen part whose recorded
// flags now come to quarantine is [Quarantined], replaying nothing, and a policy relaxed since
// changes nothing until its entry is dropped.
//
// [MemoryIndex] holds the index in memory, its claims lasting the lease it is given, none by
// default, its waiters woken the moment a claim ends. [FileIndex] holds it in a directory the
// caller names, on the standard library alone, for one process: its claims are MemoryIndex's,
// never expiring, and its entries and logs are files, sharded by the first two hex digits of a
// key's name, a claim's log written under a temporary name as it comes and renamed at Record, so
// its memory holds none of it. Two dispatches each holding a part the other contains wait on each
// other, as only content that contains itself can arrange; their contexts end the wait.
//
// # The content store
//
// A [Content] store holds content by its SHA-256: a put is all or nothing, skipped while the
// content is present and younger than the store's renew age, and an open of an absent content
// misses with fs.ErrNotExist. [FileContent] holds it in a directory, each content in its
// [ContentFile], the stable layout of its name's first two hex digits, then the whole name;
// [MemoryContent] holds it in memory.
//
// # Shapes
//
// [Event], [Part], [ContentType], [Identity], [Output], and [Warning] are the JSON shapes a
// dispatch yields, the index's [Key], [Entry], [Recorded], and [Child] the shapes it stores, and
// [Tool], [Row], and [State] the stage table's, all on encoding/json and versioned by [Version],
// with the Meta keys a consumer reads and the record outputs' media types. A field local to a
// process, such as a part's or an output's Path, never marshals.
//
// # Outputs and the caller's loop
//
// Preparation is a pipeline per part being prepared: a queue of at most [Config].Puts outputs,
// each hashed on the walk's goroutine, its content put on a goroutine of its own, then appended to
// the part's log and yielded in order from the walk's goroutine, so everything the index records
// is stored. The queue drains before a found part is dispatched and before the part's own event;
// a stopped loop or an ended context waits for the puts in flight before Dispatch returns.
//
// The caller's loop owns an event until its body returns: its files are the caller's to read,
// link, or move until then. Once the body returns for the part's own event, the dispatcher
// removes the part's file, but for the input's, and the part's working directory, the outputs'
// files and anything else its stage wrote. An output's file is there until its part's own event
// is done.
//
// The disk, then, holds the files of the parts in flight, never an input's: each part being
// dispatched, its ancestors' among them, has its file and its working directory, and each found
// part's file goes once its event is done. A store's stage holds more: the content its CLI wrote
// for records still to come. The email and onenote stages release each original as soon as the
// dispatcher settles its content, the found part's event done, its entry in the index, the
// caller keeping nothing of it. Content whose route has no stage is settled too, once the
// caller's loop has moved past its [NoProcessor] event: the caller had its chance, and a later
// record naming it in the same dispatch is no processor yet again, with no file, its flags, route,
// and reason the first's.
// So a store's working directory holds the attachments of the messages in flight, those mail has
// written ahead of the dispatch among them, rather than every attachment until the store's own
// event, whether or not a stage is registered for them. What the dispatcher doesn't settle stays
// until then, so no record loses its content: an original whose part failed, was limited,
// quarantined, or a cycle is there for the next record naming it, which is dispatched from it
// again. The index records no path. A file the caller moved away is not missed.
//
// # The pool
//
// [Dispatcher.Pool] dispatches an iterator of inputs over a number of workers, each a Dispatch of
// its own, all sharing the dispatcher's index, so content the inputs share is prepared once and
// seen by the rest, and yields every input's events as one sequence, each a [PoolEvent], its input
// and its event. An input's events keep their order; the inputs' interleave. The caller's loop body
// runs on its own goroutine, and each worker waits for it to return before its dispatch goes on, so
// the caller's loop owns each event as it owns one dispatch's, output events among them, and the
// files are removed once the body returns. An error that stops one input's dispatch is yielded
// with that input: a [*StoreError] is the store's, on which a host stops its run. A worker holds one
// event at most, so a slow loop holds the workers back, and the disk holds what that many
// dispatches have in flight. An error that stops one input's dispatch is yielded with that input,
// and the others go on; ctx ending stops them all, its error yielded alone, last.
//
// # Resuming a store
//
// [Input.After] resumes the input's read after a message, the id email.Request's After takes, a
// .pst's node id or an mbox's ordinal: [EmailStage] passes it to mail as --after, mail 0.3.0 or
// later, which writes the whole read's store and folders, then only the messages after it. Only
// the input's own part carries it. A resumed input's entry is never recorded, its outputs being
// the read's tail, so a later whole dispatch prepares it whole; the parts found below are
// recorded as ever. A resumed input whose content the index holds replays its log past the item
// whose After is the cursor, compared by equality alone, since a .pst's node ids don't ascend in
// read order; no such item ends the dispatch with an error matching [ErrRequest], as mail
// refuses an id that names no message. Each message's last output's event carries the cursor in
// [Event.After].
//
// A caller resuming a large store after a crash combines three things it keeps as it goes: a
// [FileIndex] in a directory that outlives the process, which holds the entries of the parts
// recorded before the crash, so the resumed dispatch sees each attachment already prepared;
// the cursor, the id of the last message whose outputs it has persisted; and an email.Threader,
// fed every message up to the cursor, saved by its MarshalBinary together with it: a checkpoint.
//
// The store's outputs stream as the email stage reads them, so the cursor advances while the
// store is dispatched. For each message, in mail's order, the caller has, as events: each of its
// attachments, a found part's outputs, every part found in it, and its own event, or an inline
// message's own attachments, record, and Markdown, or an attachment's record, in the attachment
// table's order; then the message's record, its Meta "id" the message's store id; then its
// Markdown, when the record's Meta "markdown" says "follows". The message's last output, its
// Markdown or, with none, its record, is the last of everything the message holds, so the caller
// persists the message's outputs as they come and feeds the Threader the message's record,
// decoded into an email.Message; at the message's last output the message is done, and its id is
// the cursor. A message record whose Meta has "embedded_in" is an inline message's, part of its
// parent's, and is fed to no Threader. A conversation record, "conversation <id>", is a message's
// for the cursor, its Meta "id" the message's store id, which mail's --after takes as any other;
// it is fed to no Threader, since a conversation has no threading fields and the Threader takes
// messages alone.
//
// The caller saves the cursor and the Threader together every N messages, or on a timer, not at
// every message: each save marshals every message's keys so far, so saving at every message grows
// with the square of the store. Task onenote's finding, docs/findings/onenote.md, measured one
// save at 3.5 ms at 25,000 messages, about 3 minutes of saving over a 50,000-message store whose
// dispatch took 16 s. The cursor rule stays exact: a checkpoint is saved only at a message's last
// output, its cursor that message's id and its Threader fed exactly the messages up to it. Every
// message past the checkpoint's cursor comes again on resuming, those persisted after the
// checkpoint and the one a crash interrupted alike, so the caller discards or overwrites what it
// had of them and feeds the restored Threader each of their records once.
//
// To resume, the caller restores the Threader by UnmarshalBinary and dispatches the store, over
// the same index directory, with After set to the checkpoint's cursor: every part prepared before
// the crash is seen, the parts of the messages past the cursor included, and once the store's
// event is done, the Threader's threads are the whole store's. A process that died leaves its
// claims' temporary logs in the index's directory, which the next [NewFileIndex] over it removes,
// its claims having died with it; a dispatch whose loop stopped, rather than died, released them.
//
// Its exported identifiers:
//
//   - [New] builds a [Dispatcher] over a [Config]: its [Stages], its [Index], its [Content], its
//     working directory, its [Limits], its inspect.Options, its puts in flight, and its release
//     bound. [Dispatcher.Dispatch] dispatches an [Input], a path or a spooled Reader, its SHA-256
//     checked and its After resuming a store, yielding [Event]s, each a [Part] and an [Output], a
//     [Warning], or the part's [Outcome]. [Dispatcher.Pool] dispatches many over workers, yielding
//     [PoolEvent]s; [Dispatcher.Content] and [Dispatcher.Index] return the stores. [ErrRequest]
//     is a refused request's cause, [ErrSHA256] a content's SHA-256 not the one claimed,
//     [ErrClaimLost] a claim lost, and [StoreError] a store's failure.
//   - [Stage], its [Identity], and [HashConfig] for an identity's configuration hash; a stage
//     yields [Item]s, made by [OutputItem], [PartItem], [WarningItem], and [FlagItem], each an
//     [Output] of an [OutputKind], [TextOutput], [MarkdownOutput], [ImageOutput], [RecordOutput],
//     or [AudioOutput], a [Warning], a flag, or a [Found] part, and ends with [ErrNoProcessor],
//     [ErrEncrypted], [ErrReadElsewhere], or [ErrRefused] to name its outcome; [ErrTooLarge] is a
//     content store's. A stage whose found parts' names are untrusted implements
//     [UntrustedWhere].
//   - The adapters: [DocumentStage], [OfficeStage], [PictureStage], [EmailStage],
//     [OneNoteStage], [AudioStage], and [VideoStage], made by [NewDocumentStage], [NewOfficeStage],
//     [NewPictureStage], [NewEmailStage], [NewOneNoteStage], [NewAudioStage], and [NewVideoStage]
//     over a [DocumentConfig], [OfficeConfig], [PictureConfig], [EmailConfig], [OneNoteConfig],
//     [AudioConfig], and [VideoConfig]; the document stage reading a PDF's structure through its
//     [QPDF] and its embedded files through its [PDFDetach]; the audio stage's plan record an
//     [AudioPlan] of [AudioWindow]s and [AudioSpan]s, and the video stage's a [VideoPlan] of its
//     [VideoWindowing] and [VideoSampling]; [VisionStage], made by [NewVisionStage]; and
//     [Versions], a stage's tools' version.
//   - [ArchiveStage], made by [NewArchiveStage] over an [ArchiveConfig], on any platform, reading
//     7-Zip's kinds through its [SevenZip].
//   - [TextStage], made by [NewTextStage], on any platform.
//   - [Index], keyed by a [Key], holding [Entry]s, each with its log of [Recorded] items, an
//     output, a [Warning], or a [Child], appended under a claim and read back streamed; the shipped
//     ones are [MemoryIndex], made by [NewMemoryIndex], and [FileIndex], made by [NewFileIndex].
//     A [Found] part may claim its SHA-256, carry a release, for content a stage holds for later
//     records, or be an unread child, with no file and an outcome.
//   - [Content], the content store, its layout [ContentFile]; the shipped ones are [FileContent],
//     made by [NewFileContent], and [MemoryContent], made by [NewMemoryContent].
//   - The shapes, versioned by [Version]: [Event], its [Part] and the part's [ContentType], an
//     [Output], a [Warning], and the stage table's [Tool], [Row], and [State], [Wired],
//     [Missing], or [Excluded]; the Meta keys [MetaPage], [MetaCount], [MetaWindow],
//     [MetaFrame], and [MetaSheet]; and the record media types [MediaAudioPlan],
//     [MediaVideoPlan], [MediaOOXML], [MediaMail], and [MediaOneNote].
package dispatch
