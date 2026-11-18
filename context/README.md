# docproc

docproc is the extraction library: it extracts text, metadata, and media from binary documents,
messages, and streams so native model capabilities can use them, and docinfer
(`github.com/standards-lab/docinfer`) runs inference over what it extracts. Its callers are Cerebro,
the data-intelligence capstone's processing service, and any other caller. It is a Go library
adjacent to the organization's Go Elemental standard, beside sqlate. The system tools it runs where
a mature implementation lives in another language sit beside it under `tools/`, each on its own
toolchain and tagged `tools/<format>/v<version>`, so a tool's contract changes with its decoder in
one pull request.

Its goals live in the coordinator's roadmap (`standards-lab/context/roadmap.toml`,
v1.ai.processing). It is built from spike-ai-pipeline-processing, spike-ai-pipeline's docproc,
and spike-ai-pipeline-office, -mail, and -onenote. [`layers.md`](layers.md) states the
boundaries of Cerebro's stack (system tools, docproc, docinfer, go-ai, Cerebro) as signatures and
schemas, and what each promotion changes from its spike; [`design.md`](design.md) holds the design
review of all five promoted artifacts: how it changes the goal's tasks, the settled design and
disposition of every finding, the one home of each shape and vocabulary the artifacts share, and
each artifact's tags and the task that releases each. The built base module is stated by the
repository's [README](../README.md) and each package's documentation.

## Capability map

- **Inspection and routing**: what a file is, whether it is safe to open, and which stage reads it
  (`layers.md`, "Inspection and routing").
- **System tools**: docproc-ooxml (docx, xlsx, pptx), docproc-mail (.pst, .msg, .eml, mbox), and
  docproc-onenote (sections, .onepkg, notebooks), each writing JSON Lines under schema "2" with
  Markdown inside as the text layer (`layers.md`, "The tool contract"). docproc-ooxml is
  [`tools/ooxml/`](../tools/ooxml/README.md), docproc-mail
  [`tools/mail/`](../tools/mail/README.md), and docproc-onenote
  [`tools/onenote/`](../tools/onenote/README.md). The Rust tools share the docproc-envelope crate,
  [`tools/envelope/`](../tools/envelope/src/lib.rs), and every tool reads the envelope's cases
  from [`tools/envelope/cases.json`](../tools/envelope/cases.json).
- **Media**: audio windows and pictures prepared for transcription and vision.
- **Dispatch**: each part of an input walked through its stage, its outputs, warnings, and found
  parts yielded as events (`layers.md`, "Dispatch", "Event shapes").
- **Stores**: the content store and the content-addressed index, interfaces with filesystem and
  memory implementations (`layers.md`, "Content", "Index"). Two nested modules provide them over
  stack libraries: `docproc/storage`, the content store over go-storage
  ([`storage/`](../storage/README.md)), and `docproc/postgres`, the index over sqlate on
  PostgreSQL ([`postgres/`](../postgres/README.md)). Their live tiers run against the compose
  harness through `mise run integration` (the root [README](../README.md), "Development").
- **Run directory**: `cmd/docproc`'s, read and written by the `rundir` and `rundir/schema`
  packages, whose package docs state its layout, a run's report directory among it, and schema
  "2" (`layers.md`, "The nested modules").
- **Command**: `cmd/docproc`, a thin composition of the library, with three commands: `version`,
  `run`, and `report`. The root [README](../README.md#the-command), "The command", states its
  commands, exit statuses, and configuration (`layers.md`, "The nested modules"; `design.md`,
  "cmd/docproc").
- **Releases**: each artifact's tags and the task that releases each, the tools on linux-x64 and
  linux-arm64 (`design.md`, "Releases").
