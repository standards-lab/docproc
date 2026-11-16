# Changelog

All notable changes to `github.com/standards-lab/docproc`, docproc's Go base module, are documented
here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each release's section is
headed by the version its `vX.Y.Z` tag carries, and is the release's notes. Its exported API is its
contract: a change to it is a change here. This changelog covers the base module only; each system
tool under `tools/` keeps its own.

## [Unreleased]

The base module was copied, in one commit, from spike-ai-pipeline-processing v0.9.0 (4402f7c), its
packages, and from spike-ai-pipeline at 6a631f1, its `internal/docproc/stages` package, which
becomes `stages`. The module path and the import paths changed and nothing else. The spike's
findings and bake-off tiers, its corpus, vision, and audio test helpers, every test that imports
spike-ai-pipeline-client, and its fixtures scripts stay in the spike.

### Added

- The packages `audio`, `dispatch`, `document`, `email`, `inspect`, `media`, `office`, `onenote`,
  `picture`, `route`, `sevenzip`, `stages`, and `tool`, with `internal/cli`, `internal/goldentest`,
  `internal/mediatest`, and `internal/textscore`, as the spike left them.

### Changed

- The module requires golang.org/x/text alone, at v0.44.0, and no longer spike-ai-pipeline-client;
  its go directive is 1.27.4.
- The office, email, and onenote readers' tests that run their real binaries, inspect's and
  dispatch's tests over those binaries, and the tests that run the real 7zz are in the
  integration tier, under the `integration` build tag.

[Unreleased]: https://github.com/standards-lab/docproc/commits/HEAD
