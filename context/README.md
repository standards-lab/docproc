# docproc

docproc is the extraction library: it extracts text, metadata, and media from binary documents,
messages, and streams so native model capabilities can use them, and docinfer
(`github.com/standards-lab/docinfer`) runs inference over what it extracts. Its callers are Cerebro,
the data-intelligence capstone's processing service, and any other caller. It is a Go library
adjacent to the organization's Go Elemental standard, beside sqlate. The system tools it runs where
a mature implementation lives in another language sit beside it under `tools/`, each on its own
toolchain and tagged `tools/<format>/v<version>`, so a tool's contract changes with its decoder in
one pull request.

Its goals live in the coordinator's roadmap (`standards-lab/roadmap/index.toml`, v1.ai.processing).
It is built from spike-ai-pipeline-processing, spike-ai-pipeline's docproc, and
spike-ai-pipeline-office, -mail, and -onenote. The repository's [README](../README.md) and each
package's documentation state the built base module. [`docs/layers.md`](../docs/layers.md) and the
pages it lists state docproc's boundaries with the system tools and its callers as signatures and
schemas. [`docs/design-review.md`](../docs/design-review.md) holds the design review of the five
promoted artifacts, and [`docs/acceptance.md`](../docs/acceptance.md) the measurements of what the
goal delivers, by its acceptance and final tasks.
[`cerebro.md`](cerebro.md) holds Cerebro's planned boundary over docproc and docinfer.

## Capability map

- **Inspection and routing** (built): [route/doc.go](../route/doc.go),
  [inspect/doc.go](../inspect/doc.go).
- **System tools** (built): [tool/doc.go](../tool/doc.go),
  [tools/ooxml/README.md](../tools/ooxml/README.md),
  [tools/mail/README.md](../tools/mail/README.md),
  [tools/onenote/README.md](../tools/onenote/README.md),
  [tools/envelope/src/lib.rs](../tools/envelope/src/lib.rs).
- **Readers** (built): [office/doc.go](../office/doc.go), [email/doc.go](../email/doc.go),
  [onenote/doc.go](../onenote/doc.go), [sevenzip/doc.go](../sevenzip/doc.go).
- **Media** (built): [audio/doc.go](../audio/doc.go), [media/doc.go](../media/doc.go),
  [picture/doc.go](../picture/doc.go), [document/doc.go](../document/doc.go).
- **Dispatch** (built): [dispatch/doc.go](../dispatch/doc.go), [stages/doc.go](../stages/doc.go).
- **Stores** (built): [dispatch/dispatchtest/doc.go](../dispatch/dispatchtest/doc.go),
  [storage/doc.go](../storage/doc.go), [postgres/doc.go](../postgres/doc.go).
- **Run directory** (built): [rundir/doc.go](../rundir/doc.go),
  [rundir/schema/doc.go](../rundir/schema/doc.go).
- **Command** (built): [cmd/docproc/main.go](../cmd/docproc/main.go).
- **Tool coverage** (planned): [tool-gaps.md](tool-gaps.md).
- **Cerebro** (planned): [cerebro.md](cerebro.md).
- **Releases** (built): [.github/workflows/release.yml](../.github/workflows/release.yml).
