using System.Runtime.InteropServices;

namespace Docproc.Ooxml;

internal static partial class Native
{
    private const int Interrupted = 4;
    private const int NoSuchFile = 2;
    private const int PermissionDenied = 13;
    private const int NotADirectory = 20;
    private const int CurrentDirectory = -100;
    private const uint StatxType = 0x1;
    private const int FileTypeMask = 0xF000;
    private const int RegularFile = 0x8000;
    private const int Directory = 0x4000;
    private const int StatxSize = 256;
    private const int StatxModeOffset = 28;

    /// <summary>A path's kind, as <see cref="KindOf"/> finds it.</summary>
    public enum PathKind
    {
        /// <summary>A regular file, after any symbolic links.</summary>
        RegularFile,

        /// <summary>Nothing at all, a dangling link among them, or a path one of whose
        /// directories is a file.</summary>
        Missing,

        /// <summary>A directory, after any symbolic links.</summary>
        Directory,

        /// <summary>Anything else that is no regular file: a device, a pipe, a socket.</summary>
        NotAFile,
    }

    /// <summary>What the path names, following symbolic links.</summary>
    /// <exception cref="UnauthorizedAccessException">A directory on the path can't be
    /// searched.</exception>
    /// <exception cref="IOException">The path can't be looked up, for any other reason than its
    /// absence.</exception>
    public static unsafe PathKind KindOf(string path)
    {
        var buffer = stackalloc byte[StatxSize];
        if (Statx(CurrentDirectory, path, 0, StatxType, buffer) != 0)
        {
            var error = Marshal.GetLastPInvokeError();
            return error switch
            {
                NoSuchFile or NotADirectory => PathKind.Missing,
                PermissionDenied => throw new UnauthorizedAccessException(Marshal.GetPInvokeErrorMessage(error)),
                _ => throw new IOException(Marshal.GetPInvokeErrorMessage(error), error),
            };
        }
        var mode = *(ushort*)(buffer + StatxModeOffset);
        return (mode & FileTypeMask) switch
        {
            RegularFile => PathKind.RegularFile,
            Directory => PathKind.Directory,
            _ => PathKind.NotAFile,
        };
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
