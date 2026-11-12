# quarry standards

The judgement calls the standards-reviewer applies to quarry, beyond what `mise run check`
enforces: each one line, with a pointer to the architecture page that applies. The design task of
v1.ai.processing writes them; until then the full Go Elemental standard applies.

- `architecture/principles/tool-beside-library.md`: `cmd/docproc` and `cmd/docinfer` are thin
  consumers of the library's packages.
