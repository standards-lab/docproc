// Package storage is docproc's content store over go-storage: a [Content], the [dispatch.Content]
// that keeps each content by its SHA-256 in the container a go-storage Client names, so the
// outputs a dispatcher stores live in an object store, such as S3, rather than on a local disk.
// It exports [Config], the store's wiring, [New], which makes one, and [Content], the store.
//
// # Layout
//
// A content's key is the host's prefix, the content's SHA-256's first two hex digits, a slash,
// then the whole SHA-256, as "<prefix>ab/ab12…": [dispatch.ContentFile]'s layout under the prefix,
// with a slash on every platform. The layout is stable, for a reader that isn't Go to follow. The
// prefix is joined as given, so a host that wants its contents under a directory of their own ends
// it with a slash.
//
// # Renewal and retention
//
// A content's age is its object's ModifiedAt, as the Client's Stat reports it: a put of a content
// present and no older than the renew age writes nothing, and a put of one older than it writes it
// again, its age restarting, as [dispatch.Content] says.
//
// The package ships no sweeper. The host sweeps content by age, never by reference, with its
// object store's own lifecycle rule on age, an expiration on the prefix, as an S3 bucket's
// lifecycle configuration sets. The rule's age is at least the index entries' retention plus the
// renew age plus the job stream's retention, so no live entry names a swept content.
package storage
