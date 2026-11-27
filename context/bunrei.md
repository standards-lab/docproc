# bunrei

Bunrei, the data-intelligence capstone's processing service, is planned. It runs docinfer's jobs
over docproc's dispatcher, stages, and stores, which it wires, and builds the go-ai clients docinfer
calls. The capstone root's notes hold its contracts; this note holds what Bunrei keeps to itself:
its input, its jobs and their fencing, and its state, which the `state` task settles
(`standards-lab/context/data-intelligence.md`, "Design").

## Queries and Commands

The capstone root's [`protocol`](https://github.com/JaimeStill/capstone-data-intelligence/blob/main/context/protocol.md) note holds Bunrei's Queries and Commands, the
`client` module's types, the profiles, and the extraction spec, and its [`upload`](https://github.com/JaimeStill/capstone-data-intelligence/blob/main/context/upload.md)
note the resumable upload, which go-web-sdk's handler, mounted by Bunrei, serves; CompleteUpload
is Bunrei's one upload Command. Every Command that changes a job appends one event to the job's
subject under the expected-last-subject-sequence check, which is its transaction.

- **Input.** A job's input is durable past the work queue's `MaxDeliver` deliveries, staged in
  blob storage at `input/<sha256>`: `dispatch.Input`'s `Reader` is blob storage's and its `SHA256`
  the declared one, checked once by the dispatcher's spool ([`docs/api.md`](../docs/api.md), "Input
  from a stream, or a path"); a mismatch fails the job at once.
- **A job** is one input, the unit of retry (data-intelligence.md, "Distribution"), its ID a UUIDv7.
  The replica running it extends its work-queue delivery while it runs, runs `job.Runner.Run` with
  `docinfer.Options.Final` set on the last delivery, and on a redelivery resumes a store from its
  last checkpoint (docinfer's `job` package doc).
- **A dead last delivery** gets no handler error, so nothing reports its exhaustion: Bunrei
  appends `failed` to a running job with no live delivery and no deliveries left, found by a
  `MaxDeliver` advisory or a sweep, the `state` task's choice.
- **Fencing.** `CancelJob` appends `cancel_requested`. The running replica watches its subject
  through a headers-only consumer and ends the job's context on a `cancel_requested`, after which
  only `cancelled` follows, or on a `started` newer than its own, after which it appends nothing. A
  refused append reads the subject's tail and acts on either alike.
- **The profile** resolves to a `docinfer.Pipeline`, one per profile.

## State and subjects

Bunrei holds only its jobs, its uploads, and each job's event stream. docproc's index and content
store and docinfer's cache are facets of its internal state, wired at its composition root.

```text
blob storage     input/<sha256>              a job's staged input
                 upload/<upload>/<offset>    an upload's staged chunks
                 content/<sha256>            docproc's Content, through docproc/storage
PostgreSQL       docproc's index             through docproc/postgres
                 docinfer's Cache            through docinfer/postgres
                 go-messaging's outbox and inbox, for Bunrei's events other than a job's
JetStream        BUNREI_WORK                bunrei.work: work-queue retention, one message
(file storage)                               per job, {"job"}
                 BUNREI_JOBS                bunrei.job.<job>: every event of one job, on its
                                             own subject; limits retention
                 BUNREI_UPLOADS             bunrei.upload.<upload>, if the upload keeps a
                                             stream
```

The host sweeps content by age ([`docs/api.md`](../docs/api.md), "Content"). Each job's events go on
one subject of its own, so the job has a gap-free sequence the sequence check guards; the subjects
share one stream, leaving no streams to reap. `standards-lab/context/messaging.md`'s conventions
govern committed mutations, not a job's progress stream. The browser never subscribes: Mikoshi relays
its own stream (data-intelligence.md, "Browser"). The names and retentions are the `state` task's.

## Events

The root's [`events`](https://github.com/JaimeStill/capstone-data-intelligence/blob/main/context/events.md) note holds Bunrei's events: the envelope, the lineage and
`layer` extensions, the gap-free `jobsequence`, the event table, the result batches with their cap,
a Bunrei setting, and the manifest.

## Requests to go-messaging

data-intelligence.md queues these asks of go-messaging's NATS provider, sized by the `state` and
`events` tasks, the `events` task assuming them met:

- A publish on a subject the caller names.
- A durable consumer filtered by subject.
- The CloudEvents `jobsequence` extension carried.
- Events delivered in order within a delivery group.
- A work-queue delivery its handler extends while it runs (an in-progress acknowledgement).

## Assumptions

- Assumes Bunrei wires docproc's `stages` as `cmd/docproc` does.
- Assumes one `docinfer.Bounds` per replica, shared by its pipelines, one per profile.
- Assumes Bunrei speaks Chat Completions to the gateway, its consumer key the bearer token.
- Assumes the consumer stores chunks by their identity, so a batch a redelivered job repeats changes
  nothing (the `chunks` task).
