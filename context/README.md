# quarry

quarry extracts text, metadata, and media from binary documents, messages, and streams so native
model capabilities can use them: Cerebro, the data-intelligence capstone's processing service, and
any other caller. It is a Go library adjacent to the organization's Go Elemental standard, beside
sqlate. The system tools it runs where a mature implementation lives in another language sit
beside it under `tools/`, each on its own toolchain and tagged `tools/<format>/v<version>`, so a
tool's contract changes with its decoder in one pull request.

Its goals live in the coordinator's roadmap (`standards-lab/context/roadmap.toml`,
v1.ai.processing). It is built from spike-ai-pipeline-processing, spike-ai-pipeline's docproc and
docinfer, and spike-ai-pipeline-office, -mail, and -onenote.

## Capability map

- **Inspection and routing**: what a file is, whether it is safe to open, and which stage reads it.
- **System tools**: quarry-ooxml (docx, xlsx, pptx), quarry-mail (.pst, .msg, .eml, mbox), and
  quarry-onenote (sections, .onepkg, notebooks), each with one JSON contract whose text layer is
  Markdown.
- **Media**: audio windows and pictures prepared for transcription and vision.
- **Inference**: the interfaces vision, transcription, chunking, and embedding run behind, free
  of any model client.
- **Run state**: the run directory, the content-addressed index and cache, and results in
  batches with their events.
- **Commands**: `cmd/docproc` and `cmd/docinfer`, thin compositions of the library.
