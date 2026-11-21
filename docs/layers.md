# Layers

These pages state, as signatures and schemas, the boundaries between the five layers of Cerebro's
stack: the system tools (docproc-ooxml, docproc-mail, docproc-onenote), docproc (the Go extraction
library), docinfer (the Go inference library), go-ai (the model client), and Cerebro (the
data-intelligence capstone's processing service). Each arrow points from a layer to a layer that
uses it:

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
  index, and the event shapes.
- [Changes per tool and per package](changes.md): what each promotion changes from its spike.
- [The design review](design-review.md): each finding's settled design and disposition.

## Assumptions

- Assumes docproc's dispatch keeps asking each tool for its Markdown alone as the text it hands to
  inference, as processing's stages do.
- Assumes Athena builds `sparse_rows` from typed cells, not from Markdown (`athena-mapping`).
- Assumes docproc keeps processing's package names `office`, `email`, and `onenote`, not the
  tools' names: each does more than decode its tool (LibreOffice's legacy conversion, threading).
- Assumes `cmd/docinfer`, in docinfer's repository, reads a `cmd/docproc` run directory through
  docproc's public `rundir` and `rundir/schema`.
- Assumes inference needs nothing of docproc beyond `dispatch` (its dispatcher, events, shapes,
  and Meta keys) and, for `cmd/docinfer`, `rundir`, so docproc depends on no model concept and
  imports no docinfer package.
- Assumes every stream input is spooled to scratch before its stages run, since every stage and
  tool reads a path; no stage reads a stream directly.
- Assumes the typed metadata shapes wait for the capstone's `metadata` task; until then,
  `Output.Meta` and the tools' lines carry metadata.
