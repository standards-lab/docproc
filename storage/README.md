# docproc/storage

`github.com/standards-lab/docproc/storage` is docproc's content store in object storage: a
`Content`, the `dispatch.Content` that keeps each output's content by its SHA-256 in the
container a go-storage `Client` names, so the outputs a dispatcher stores live in an object store,
such as S3 or Azure Blob, rather than on a local disk. It is a nested module of
[docproc](../README.md), released on its own `storage/vX.Y.Z` tags, and requires the base module
and go-storage; a provider, such as go-storage's `s3`, is the host's. The module's go.mod also
requires `s3`, for its live tier alone.

## Install

```sh
go get github.com/standards-lab/docproc/storage@v0.1.1
```

[`CHANGELOG.md`](CHANGELOG.md) states what each release changes. The package's documentation,
`go doc github.com/standards-lab/docproc/storage`, is the reference for its exported names, and
its doc comment lists them: `Config`, `New`, and `Content`.

## Configuration

```go
store, err := storage.New(storage.Config{
	Client: client,             // a go-storage Client, such as a Store over s3; its container must exist
	Prefix: "content/",         // joined as given: end it with a slash for a directory of its own
	Renew:  7 * 24 * time.Hour, // a content put again once older than this is rewritten; zero never rewrites
})
```

`Client` is required, and a nil one panics naming the field. `Renew` has no default, and a
negative one is refused, matching `dispatch.ErrRequest`. A host hands the `Content` to its
`dispatch.Dispatcher` as it would a `dispatch.FileContent`. The `Client`'s own bounds,
go-storage's `MaxObjectSize` among them, are the host's: a put past it fails matching
`dispatch.ErrTooLarge`, so the part fails recorded.

## The contracts

- **Key layout.** A content's key is the prefix, the content's SHA-256's first two hex digits, a
  slash, then the whole SHA-256: `<prefix>ab/ab12…`, `dispatch.ContentFile`'s layout under the
  prefix, with a slash on every platform. The layout is stable, for a reader that isn't Go to
  follow.
- **Renewal.** A content's age is its object's `ModifiedAt`, as the `Client`'s `Stat` reports it.
  A put of a content present and no older than the renew age writes nothing; a put of one older
  than it writes it again, all or nothing, its age restarting.
- **Errors.** go-storage's `ErrTooLarge` matches `dispatch.ErrTooLarge`; an open's `ErrNotFound`
  matches `fs.ErrNotExist`, and a missing container never does; a name that isn't a SHA-256 as
  lowercase hex matches `dispatch.ErrRequest`.
- **No sweeper.** The module deletes nothing. The host sweeps content by age, never by
  reference, with its object store's own lifecycle rule on age: an expiration on the prefix, as
  an S3 bucket's lifecycle configuration sets. The rule's age is at least the index entries'
  retention plus the renew age plus the job stream's retention, so no live entry names a swept
  content.

## Testing

Tasks run from the repository root, `mise run <task>`:

- `check`: the unit tier, with every other module. It runs dispatchtest's `Content` suite over a
  go-storage `Store` wrapping storagetest's `Fake`, with the renew age on the Fake's clock, a
  `Store`'s `MaxObjectSize`, and a missing container; it reads no network.
- `integration:storage`: the live tier, under the `integration` build tag. It starts the compose
  harness's SeaweedFS alone, runs the `Content` suite over go-storage's `s3` provider against its
  S3 gateway, each test in a bucket of its own, and stops the service whether the suite passes or
  fails, printing its log on a failure. `integration` runs it after the postgres module's, and
  `test:integration` after the base module's integration tier.

SeaweedFS listens on port 8334; `DOCPROC_SEAWEEDFS_PORT` moves it, and `DOCPROC_S3_ENDPOINT` in
the root `mise.toml` follows. The tier reads `DOCPROC_S3_ENDPOINT`, `DOCPROC_S3_ACCESS_KEY`, and
`DOCPROC_S3_SECRET_KEY`, which `mise.toml` sets for the harness, and fails, rather than skips,
when one is unset.

## Limits

- A put trusts the caller's SHA-256: the bytes aren't hashed against the name.
- Every put costs a `Stat` first, and a renewal writes the whole content again.
- The renew age is compared on the host's clock against the store's `ModifiedAt`, so skew
  between them moves a renewal by as much.
- A content is read whole, from its start: no range read, and no resumed put.

Apache-2.0.
