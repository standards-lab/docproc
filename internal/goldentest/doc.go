// Package goldentest holds the golden-sum helpers the integration tier's test packages share, for
// outputs that must stay byte for byte what they were: a file of SHA-256 sums, one per output,
// recorded once and compared against on every run. dispatch's unit-tier goldens, compared as text,
// are recorded under its RecordEnv too. It builds untagged so the unit tier proves it, and no
// released file imports it.
//
// Its exported identifiers:
//
//   - [RecordEnv] names the variable that, set to 1, records the sums in place of comparing.
//   - [Sum] returns the lowercase hex SHA-256 of an output.
//   - [CanonicalPNG] returns a PNG without the chunks that carry the time it was written.
//   - [Check] compares a run's sums with a golden file's, or records them.
package goldentest
