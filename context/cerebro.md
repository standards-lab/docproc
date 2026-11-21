# cerebro

Cerebro, the data-intelligence capstone's processing service, is planned. It runs docinfer's jobs
over docproc's dispatcher, stages, and stores, which it wires, and builds the go-ai clients docinfer
calls. This note holds the boundary its design tasks start from: `protocol` settles the Queries and
Commands and the upload protocol, `state` the stores, streams, and the fold, and `events` the event
protocol (`standards-lab/context/data-intelligence.md`, "Design"). Its nested `client` module,
tagged `client/vX`, holds these types and a Go client; Cerebro imports none of its consumers
(data-intelligence.md, "Cerebro and Athena", "API").

## Queries and Commands

Go Elemental Queries and Commands. Every Command that changes a job appends one event to the job's
subject under the expected-last-subject-sequence check, which is its transaction.

```go
package client // capstone-cerebro/client

// Commands
type Submit struct { // a small document, whole
    ID         string // the caller's idempotency ID
    Profile    string // a versioned processing profile in Cerebro's configuration, never a model
    Name, MIME string
    Content    []byte // at most Cerebro's configured small-document size
    Extraction *docinfer.Extract
}
type OpenUpload struct { Name, MIME string; Size int64; SHA256 string }
type AppendChunk struct { Upload string; Offset int64; SHA256 string; Bytes []byte }
type CompleteUpload struct { Upload, Profile string; Extraction *docinfer.Extract }
type AbortUpload struct { Upload string }
type CancelJob struct { Job string }

// Queries
type GetJob struct { Job string }       // -> Job
type GetUpload struct { Upload string } // -> Upload: the offset to resume from
type GetOutput struct { SHA256 string } // -> the content a job's update names, streamed
type ListProfiles struct{}              // -> []Profile

type Upload struct { ID string; Size, Received int64; ChunkSize int; Expires time.Time } // UTC
type Profile struct { Name string; Resolved docinfer.Profile }
type Job struct {
    ID, Profile, Name string; Size int64; SHA256 string
    State    State  // queued, running, completed, failed, cancelled
    Attempt  int    // the work queue's delivery, from 1
    Sequence uint64 // the job's last event
    Manifest *Manifest
    Error    string
}
```

The `Client` has one method per Command and Query, each taking a `context.Context`.

- **Input.** `Submit` stages a small document in blob storage at `input/<sha256>` before it returns
  the job queued; a larger one goes through the upload, any replica taking any chunk, a mismatched
  chunk refused, `GetUpload` giving the offset to resume from, and `CompleteUpload` queuing the job
  once all `Size` bytes are received. Either way the input is durable past the work queue's
  `MaxDeliver` deliveries: `dispatch.Input`'s `Reader` is blob storage's and its `SHA256` the
  declared one, checked once by the dispatcher's spool ([`docs/api.md`](../docs/api.md), "Input from
  a stream, or a path"); a mismatch fails the job at once. `Submit`'s `ID` makes a retried Submit
  return the first one's job. The upload's shape graduates to go-web-sdk
  (`chunked-upload-graduation`).
- **A job** is one input, the unit of retry (data-intelligence.md, "Distribution"), its ID a UUIDv7.
  The replica running it extends its work-queue delivery while it runs, runs `job.Runner.Run` with
  `docinfer.Options.Final` set on the last delivery, and on a redelivery resumes a store from its
  last checkpoint (docinfer's `job` package doc).
- **A dead last delivery** gets no handler error, so nothing reports its exhaustion: Cerebro
  appends `failed` to a running job with no live delivery and no deliveries left, found by a
  `MaxDeliver` advisory or a sweep, the `state` task's choice.
- **Fencing.** `CancelJob` appends `cancel_requested`. The running replica watches its subject
  through a headers-only consumer and ends the job's context on a `cancel_requested`, after which
  only `cancelled` follows, or on a `started` newer than its own, after which it appends nothing. A
  refused append reads the subject's tail and acts on either alike.
- **The profile** resolves to a `docinfer.Pipeline`. Each attempt's `started` carries the resolved
  `docinfer.Profile` before any batch, so the consumer registers the matching embedding space (the
  `chunks` task). The `vision` task's prompt and sampling live in a profile.
- **Extraction** is optional: a caller's `docinfer.Extract`. Cerebro refuses one that marshals past
  256 KiB, so `queued`, which carries it, stays under the 1 MB `max_payload`.

## State and subjects

Cerebro holds only its jobs, its uploads, and each job's event stream. docproc's index and content
store and docinfer's cache are facets of its internal state, wired at its composition root.

```text
blob storage     input/<sha256>              a job's staged input
                 upload/<upload>/<offset>    an upload's staged chunks
                 content/<sha256>            docproc's Content, through docproc/storage
PostgreSQL       docproc's index             through docproc/postgres
                 docinfer's Cache            through docinfer/postgres
                 go-messaging's outbox and inbox, for Cerebro's events other than a job's
JetStream        CEREBRO_WORK                cerebro.work: work-queue retention, one message
(file storage)                               per job, {"job"}
                 CEREBRO_JOBS                cerebro.job.<job>: every event of one job, on its
                                             own subject; limits retention
                 CEREBRO_UPLOADS             cerebro.upload.<upload>, if the upload keeps a
                                             stream
```

The host sweeps content by age ([`docs/api.md`](../docs/api.md), "Content"). Each job's events go on
one subject of its own, so the job has a gap-free sequence the sequence check guards; the subjects
share one stream, leaving no streams to reap. `standards-lab/context/messaging.md`'s conventions
govern committed mutations, not a job's progress stream. The browser never subscribes: Athena relays
its own stream (data-intelligence.md, "Browser"). The names and retentions are the `state` task's.

## Events

Each event is a CloudEvents 1.0 event in binary mode: `source` is `cerebro`, `id` a UUIDv7,
`subject` the job's ID, and `Nats-Msg-Id` de-duplicates a retried append; consumers de-duplicate on
`source` and `id`. Cerebro's `messaging.go` defines each type with go-core's `event.Define[T]`
([`docs/api.md`](../docs/api.md), "Event shapes"); `dataschema` names the payload's producing
package's `Version`, its exact form the `events` task's.

```text
type                            data                                          whose
cerebro.job.queued              {profile, name, mime, size, sha256, extraction} Cerebro's
cerebro.job.started             {attempt, replica, profile}                   Cerebro's
cerebro.job.part                dispatch.Event, a part's own                  docproc's
cerebro.job.output              dispatch.Event, an output's (SHA256, no path) docproc's
cerebro.job.warning             dispatch.Event, a warning's                   docproc's
cerebro.job.inference           docinfer.Inference                            docinfer's
cerebro.job.progress            docinfer.Progress                             docinfer's
cerebro.job.batch               docinfer.Batch; batchdigest, batchchunks      docinfer's
cerebro.job.checkpoint          job.Checkpoint                                docinfer's
cerebro.job.cancel_requested    {}                                            Cerebro's
cerebro.job.completed           Manifest                                      Cerebro's
cerebro.job.attempt.failed      {attempt, error}                              Cerebro's
cerebro.job.failed              {attempt, error}                              Cerebro's
cerebro.job.cancelled           {}                                            Cerebro's
```

- **docinfer's updates pass through**, their JSON unchanged; `done`'s `job.Done` goes into the
  manifest's entry for its attempt.
- **The fold.** A job is queued, then running from `started`, then completed, failed, or cancelled.
  An attempt that fails with deliveries left appends `attempt.failed`; `failed` is terminal, once
  the deliveries are spent or on an error `docinfer.Permanent` reports.
- **Size.** Every event fits NATS's 1 MB `max_payload`; an output or inference carries its content's
  SHA-256, never the content.

## Results and the manifest

A `cerebro.job.batch` event carries docinfer's `Batch` and, as the extension attributes
`batchdigest` and `batchchunks`, its `Digest()` and chunk count, so the headers-only fold yields the
manifest without reading a batch back. The consumer commits each batch in one transaction,
de-duplicated through its inbox. The job ends with `completed`, whose data is the manifest:

```go
type Manifest struct { // its range: every sequence from 1 through the completed event's own
    Batches  int       `json:"batches"`  // the batch events in the range, every attempt's
    Chunks   int       `json:"chunks"`   // the chunks those batches hold
    Digest   string    `json:"digest"`   // SHA-256 over those batches' digests, in sequence order
    Attempts []Attempt `json:"attempts"` // one per started, in order
}
type Attempt struct {
    Done *job.Done `json:"done,omitempty"` // nil for an attempt that ended without one
}
```

Each event carries the job's sequence, from 1 with no gap, in the CloudEvents `sequence` extension,
kept contiguous by the append check. A batch is identified by its job and its event's sequence,
which no attempt reuses, so docinfer numbers none. The consumer checks completeness by holding every
sequence through `completed`'s and recomputing the manifest over what it committed, every attempt's
batches, since a redelivery replays content. Counts are per attempt, never summed: a failed attempt
reports none. The digest covers the chunks' digests, so their text, not their vectors: it proves
which chunks arrived; a vector's length is checked against the profile's dimensions.

## Open questions

- **Live sources.** A URL source read as it arrives (srt, rtmp, http, udp) has no owner yet
  ([`docs/design-review.md`](../docs/design-review.md), decision 14). Whether one is a job, and
  through which Command, is the `protocol` task's.
- **The small-document size**, `Submit`'s limit and the upload's threshold, is a setting whose
  default the `protocol` task picks.

## Requests to go-messaging

data-intelligence.md queues these asks of go-messaging's NATS provider, sized by the `state` and
`events` tasks:

- A publish on a subject the caller names.
- A durable consumer filtered by subject.
- The CloudEvents `sequence` extension carried.
- Events delivered in order within a delivery group.
- A work-queue delivery its handler extends while it runs (an in-progress acknowledgement).

## Assumptions

- Assumes Cerebro wires docproc's `stages` as `cmd/docproc` does.
- Assumes one `docinfer.Bounds` per replica, shared by its pipelines, one per profile.
- Assumes a result batch's cap suits the consumer; the `events` task may tune it.
- Assumes Cerebro speaks Chat Completions to the gateway, its consumer key the bearer token.
- Assumes go-messaging gains what "Requests to go-messaging" asks before the `events` task builds on
  it.
- Assumes the consumer stores chunks by their identity, so a batch a redelivered job repeats changes
  nothing (the `chunks` task).
