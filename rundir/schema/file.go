package schema

// File is an input's file metadata, as the filesystem holds it, and its sniffed type:
//
//   - "name": its file name
//   - "arg": the path argument it came from, a file or a directory, absolute, cleaned
//   - "path": its path relative to that argument, with slashes; "." when the argument is the
//     file itself, so joining "arg" and "path" always names the file
//   - "size": its size in bytes
//   - "mode": its mode and permissions, as ls writes them, such as "-rw-r--r--"
//   - "modified": its modified time, a [Time]
//   - "created": its created time, statx's birth time, a [Time]; optional, absent where the
//     filesystem records none, and on any system but Linux
//   - "detected": the MIME type net/http.DetectContentType names for its first 512 bytes, such
//     as "text/plain; charset=utf-8"; optional, absent when they couldn't be read. It is the
//     host's sniffing, apart from what routing read of the part, its part's content_type.
type File struct {
	Name     string `json:"name"`
	Arg      string `json:"arg"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Mode     string `json:"mode"`
	Modified Time   `json:"modified"`
	Created  Time   `json:"created,omitzero"`
	Detected string `json:"detected,omitzero"`
}
