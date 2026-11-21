# Layers

These pages state, as signatures and schemas, docproc's boundaries, with the system tools below it
and its callers above, in Cerebro's five-layer stack: the system tools (docproc-ooxml,
docproc-mail, docproc-onenote), docproc (the Go extraction library), docinfer (the Go inference
library), go-ai (the model client), and Cerebro (the data-intelligence capstone's processing
service); Cerebro's planned boundary is in [`context/cerebro.md`](../context/cerebro.md). Each
arrow points from a layer to a layer that uses it:

```text
                    ┌────────────────────────┐
                    │                        ▼
system tools ──► docproc ──► docinfer ──► Cerebro
                               ▲             ▲
go-ai ─────────────────────────┴─────────────┘
```

docproc extracts and knows no model; docinfer infers over what docproc extracts, calling go-ai
directly; Cerebro runs docinfer's jobs over docproc's dispatcher, stages, and stores, which it
wires, and builds the go-ai clients docinfer calls. Each signature or schema traces to a capstone
task in `standards-lab/context/data-intelligence.md` ("Design") or to spike evidence.

Spike paths are relative to `~/experiments/`. "processing" is spike-ai-pipeline-processing, whose
`office`, `email`, `onenote`, and `dispatch` packages are the consumers the tools are measured
against. "the root" is spike-ai-pipeline, and "the spike's docproc" and "the spike's docinfer" are
the commands under its `internal/docproc` and `internal/docinfer`; docproc and docinfer alone are
the libraries, and `cmd/docproc` and `cmd/docinfer` their commands.

The boundaries' pages:

- [The tool contract](tool-contract.md): the common envelope, each tool's schema "2", and
  versioning.
- [Payload measurement](payload-measurement.md): what each structured field costs over `~/docs`.
- [Consumers of structured text](structured-text.md): the rule that every piece of text exists
  once.
- [docproc's API](api.md): inspection and routing, running the tools, dispatch, content, the
  index, the event shapes, and the run directory.
- [The design review](design-review.md): each finding's settled design and disposition.

## Assumptions

- Assumes Athena builds `sparse_rows` from typed cells, not from Markdown (`athena-mapping`).
- Assumes `cmd/docinfer`, in docinfer's repository, reads a `cmd/docproc` run directory through
  docproc's public `rundir` and `rundir/schema`.
- Assumes the typed metadata shapes wait for the capstone's `metadata` task; until then,
  `Output.Meta` and the tools' lines carry metadata.
