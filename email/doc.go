// Package email reads mail stores, Outlook .pst stores and .msg messages, and MIME messages, .eml
// and mbox files, for a model through docproc-mail: an input in, its records streamed out as typed
// Go values while docproc-mail runs. It provides pre-inference preparation only; a layer above
// sends what it returns to a model. The package builds on every 64-bit platform, on the standard
// library and this module's tool and internal/cli packages; the program it runs is a tool.Tool the
// caller built through the exec runner.
//
// A Reader is built at the composition root over a docproc-mail Tool, MailSpec declaring it.
// NewReader refuses one older than MailMinimum, 0.1.0, its first schema "2" release, and one the
// container runner built, since docproc-mail reads the input's path in place; the Tool's own
// construction refuses a missing one, or one whose --version doesn't name schema "2". The contract
// is docproc-mail's README, tools/mail/README.md: its JSON Lines, schema "2", in the common
// envelope, whose media names, warnings, and exit codes internal/cli holds.
//
// Reader.Read returns the sequence of one input's records. Ranging over it runs docproc-mail once,
// through internal/cli's envelope reader, which checks that the first line is a schema "2" store
// line, and yields each line docproc-mail writes as it writes it, decoded through
// encoding/json/v2 into the typed value of its kind, Store, Folder, Message, or Skipped, each time
// UTC: the store first, then, for a .pst, each folder before its messages and they before its
// subfolders, with a skipped record wherever docproc-mail couldn't read one; for a .msg or an
// .eml, its message, with any embedded message nested under its attachment record; for an mbox,
// each message in the file's order. A line of a kind the package doesn't know is an
// UnknownRecord, never an error. Each warning docproc-mail writes to stderr is a Warning record
// among them, parsed into where and what, a .pst folder's node id in where rewritten to the
// folder's path once its line has been read. A Request's After resumes a .pst's or an mbox's read
// after the message whose id it is, through docproc-mail's --after: the store, every folder, and
// every folder's skipped record, then only the messages after it, the whole read's tail byte for
// byte.
//
// A message's body is its Markdown, docproc-mail's. Its attachments, and the data: images its
// Markdown links, its Media, name their files by the envelope's media name rule; given a media
// directory in its Request, docproc-mail writes each distinct content there once, and each
// Attachment's and Medium's Path names its file, its SHA256 the hash the name claims. The package
// routes nothing: the caller hands each file onward, and an embedded message is nested under its
// attachment record, where docproc-mail could read it. A sender whose SMTP address the line leaves
// out, an Exchange sender's whose input doesn't hold it among them, takes the address of its
// message's transport headers' From field, when they have one that parses.
//
// A message of a conversation class, IPM.Note.Microsoft.Conversation or IPM.Note.Microsoft.Missed,
// the classes a Skype for Business client saves its instant-message history under in a store's
// Conversation History folder, is a Message like any other; Message.Conversation is its view as
// one conversation: its participants, the sender then the recipients; its start and end, the
// submit and delivery times; its topic, the stored conversation topic, else the subject; whether
// it was missed, by its class; and its transcript, the Markdown. The class decides, never the
// folder. A Conversation is one per stored item: the package parses no turn from the transcript.
//
// docproc-mail's exit codes 1 to 4, and a death by a signal, are a *cli.ExitError, which matches
// its code's error through errors.Is, ErrNotWritten, ErrUsage (a fault in this package, or After
// with a single message), ErrRefused (with docproc-mail's reason, an .ost's among them), or
// ErrFailed, each internal/cli's shared error, and names a refusal's class by its Refusal method. A
// read whose context ends returns an error matching the context's, as every Tool run does. A loop
// that stops early stops docproc-mail.
//
// Threads come from a store's messages: a Threader, fed each record while a read is ranged over,
// keeps their threading keys alone, so its memory follows the number of messages, never their
// bodies. The spike measured it, at spike-ai-pipeline-processing v0.9.0, over 200,000 messages: 362
// bytes a message held, 435 while Threads runs. Its keys are unchanged since. A
// caller resuming a read rebuilds a Threader from the message records it persisted; no Threader
// state is saved. Threader.Threads builds the threads by RFC 5256's REFERENCES algorithm over
// Message-ID, In-Reply-To, and References, with one addition: a message whose headers name no
// parent takes the one its conversation index names, its own less the last child block. A
// Message-ID or index parent no message has is a placeholder, pruned as RFC 5256 says: one with no
// children is removed, and one with children gives them to its own parent, unless it is a root
// holding several, which it keeps together. Roots with the same base subject are then gathered, a
// reply under the message it replies to, so a store without threading fields threads by subject
// alone. Each set of children is ordered by sent date. A message of a conversation class isn't
// threaded.
//
// Known limits, docproc-mail's: in a .pst, an embedded message is listed without its content; an
// .ost is refused; a conversation is a record per item, its turns unparsed; an attachment by
// reference has no content file; the stores docproc-mail's generator writes carry no threading
// fields, so they thread by subject alone; every mbox is read as an mboxrd, so an mboxcl's
// Content-Length is ignored and an mboxo body line that was ">From " loses its ">"; and an .eml's
// or an mbox's message has no class, no creation or modification time, and SMTP addresses alone.
//
// Its exported identifiers:
//
//   - [MailSpec] declares docproc-mail for the exec runner's constructor.
//   - [MailMinimum] is the oldest docproc-mail the Reader accepts, 0.1.0.
//   - [Config] is the Tool a Reader is built over.
//   - [Reader] reads mail stores and MIME messages through docproc-mail.
//   - [NewReader] builds a Reader over a Config.
//   - [Reader.Read] returns the sequence of one input's records.
//   - [Request] is one input's read: its path, the media directory docproc-mail writes files
//     into, if any, and the message its read resumes after, if any.
//   - [ErrRequest] is the cause of a refused request, and of a container-built Mail.
//   - [ErrNotWritten], [ErrUsage], [ErrRefused], and [ErrFailed] are docproc-mail's exit codes 1
//     to 4, internal/cli's.
//   - [ErrSchema] is the cause of a first line that isn't a schema "2" store line.
//   - [Record] is one item of a read: [Store], [Folder], [Message], [Skipped], [Warning], or
//     [UnknownRecord].
//   - [Address], [Attachment], and [Medium] are a message's addresses, attachment records, and
//     media; an Attachment's or Medium's Path names its file in the media directory.
//   - [Conversation] is a message of a conversation class, [ConversationClass] or [MissedClass],
//     read as one conversation by [Message.Conversation]; [IsConversation] reports whether a class
//     is one.
//   - [Threader] builds a store's threads from the threading keys of the messages [Threader.Add]
//     is fed while a read is ranged over; [Threader.Threads] returns them.
//   - [Threads] builds the threads of a read's records whole.
//   - [Thread] is one message of a thread, or a placeholder, with its replies.
package email
