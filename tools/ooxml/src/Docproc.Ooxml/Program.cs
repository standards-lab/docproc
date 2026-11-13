// docproc-ooxml: the Open XML system tool of docproc. Cli holds the process contract; stdout and
// stderr are the raw descriptors, so the locale can't change a byte and a broken pipe is an error.
using Docproc.Ooxml;

return Cli.Run(args, DescriptorStream.Stdout, DescriptorStream.Stderr);
