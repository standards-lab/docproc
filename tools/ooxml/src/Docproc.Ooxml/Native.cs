using System.Runtime.InteropServices;

namespace Docproc.Ooxml;

/// <summary>The two calls into libc .NET has no API for: a write to a file descriptor that
/// reports every error, and a file's type. The csproj binds them directly into the NativeAOT
/// binary (<c>DirectPInvoke</c>), which links libc anyway; in process, .NET resolves
/// <c>libc</c> itself.</summary>
internal static partial class Native
{
    private const int Interrupted = 4; // EINTR
    private const int NoSuchFile = 2; // ENOENT
    private const int NotADirectory = 20; // ENOTDIR
    private const int CurrentDirectory = -100; // AT_FDCWD
    private const uint StatxType = 0x1; // STATX_TYPE
    private const int FileTypeMask = 0xF000; // S_IFMT
    private const int RegularFile = 0x8000; // S_IFREG

    /// <summary>A path's kind, as <see cref="KindOf"/> finds it.</summary>
    public enum PathKind
    {
        /// <summary>A regular file, after any symbolic links.</summary>
        RegularFile,

        /// <summary>Nothing at all, or anything but a regular file: a directory, a device, a
        /// pipe, a socket.</summary>
        NotAFile,
    }

    /// <summary>What the path names, following symbolic links.</summary>
    /// <exception cref="IOException">The path can't be looked up, for a reason other than its
    /// absence, such as a directory on it that can't be searched.</exception>
    public static unsafe PathKind KindOf(string path)
    {
        // struct statx is the same on every architecture: 256 bytes, its u16 stx_mode at 28.
        var buffer = stackalloc byte[256];
        if (Statx(CurrentDirectory, path, 0, StatxType, buffer) != 0)
        {
            var error = Marshal.GetLastPInvokeError();
            if (error is NoSuchFile or NotADirectory)
            {
                return PathKind.NotAFile;
            }
            throw new IOException(Marshal.GetPInvokeErrorMessage(error), error);
        }
        var mode = *(ushort*)(buffer + 28);
        return (mode & FileTypeMask) == RegularFile ? PathKind.RegularFile : PathKind.NotAFile;
    }

    /// <summary>Writes all of buffer to the file descriptor, retrying an interrupted write.</summary>
    /// <exception cref="IOException">A write failed: a broken pipe, a full disk, a closed
    /// descriptor. .NET's console streams report none of these, a broken pipe passing for
    /// success.</exception>
    public static unsafe void WriteAll(int descriptor, ReadOnlySpan<byte> buffer)
    {
        while (!buffer.IsEmpty)
        {
            nint written;
            fixed (byte* bytes = buffer)
            {
                written = Write(descriptor, bytes, (nuint)buffer.Length);
            }
            if (written < 0)
            {
                var error = Marshal.GetLastPInvokeError();
                if (error == Interrupted)
                {
                    continue;
                }
                throw new IOException(Marshal.GetPInvokeErrorMessage(error), error);
            }
            buffer = buffer[(int)written..];
        }
    }

    [LibraryImport("libc", EntryPoint = "write", SetLastError = true)]
    private static unsafe partial nint Write(int descriptor, byte* buffer, nuint count);

    [LibraryImport("libc", EntryPoint = "statx", SetLastError = true, StringMarshalling = StringMarshalling.Utf8)]
    private static unsafe partial int Statx(int directory, string path, int flags, uint mask, byte* buffer);
}

/// <summary>stdout or stderr as the raw file descriptor, written as given with no encoding, no
/// buffer, and every error reported, so the locale can't change a byte and a broken pipe is an
/// I/O error.</summary>
internal sealed class DescriptorStream(int descriptor) : Stream
{
    /// <summary>The process's stdout.</summary>
    public static DescriptorStream Stdout { get; } = new(1);

    /// <summary>The process's stderr.</summary>
    public static DescriptorStream Stderr { get; } = new(2);

    public override bool CanRead => false;

    public override bool CanSeek => false;

    public override bool CanWrite => true;

    public override long Length => throw new NotSupportedException();

    public override long Position
    {
        get => throw new NotSupportedException();
        set => throw new NotSupportedException();
    }

    public override void Write(ReadOnlySpan<byte> buffer) => Native.WriteAll(descriptor, buffer);

    public override void Write(byte[] buffer, int offset, int count) => Write(buffer.AsSpan(offset, count));

    public override void Flush()
    {
    }

    public override int Read(byte[] buffer, int offset, int count) => throw new NotSupportedException();

    public override long Seek(long offset, SeekOrigin origin) => throw new NotSupportedException();

    public override void SetLength(long value) => throw new NotSupportedException();
}
