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
each artifact's first tag and the task that releases it.

## Capability map

- **Inspection and routing**: what a file is, whether it is safe to open, and which stage reads it
  (`layers.md`, "Inspection and routing").
- **System tools**: docproc-ooxml (docx, xlsx, pptx), docproc-mail (.pst, .msg, .eml, mbox), and
  docproc-onenote (sections, .onepkg, notebooks), each writing JSON Lines under schema "2" with
  Markdown inside as the text layer (`layers.md`, "The tool contract"). docproc-ooxml is
  [`tools/ooxml/`](../tools/ooxml/README.md), and the envelope's cases every tool reads are
  [`tools/envelope/cases.json`](../tools/envelope/cases.json).
- **Media**: audio windows and pictures prepared for transcription and vision.
- **Dispatch**: each part of an input walked through its stage, its outputs, warnings, and found
  parts yielded as events (`layers.md`, "Dispatch", "Event shapes").
- **Stores**: the content store and the content-addressed index, interfaces with filesystem and
  memory implementations, and their providers `docproc/storage` and `docproc/postgres`
  (`layers.md`, "Content", "Index").
- **Run directory**: `cmd/docproc`'s, read and written by `rundir` and `rundir/schema`
  (`layers.md`, "The nested modules").
- **Command**: `cmd/docproc`, a thin composition of the library (`layers.md`, "The nested
  modules"; `design.md`, "cmd/docproc").
- **Releases**: each artifact's first tag and the task that tags it, the tools on linux-x64 and
  linux-arm64 (`design.md`, "Releases").
