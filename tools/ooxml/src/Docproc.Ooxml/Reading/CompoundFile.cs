using System.Buffers.Binary;
using System.Text;

namespace Docproc.Ooxml.Reading;

/// <summary>An OLE compound file whose structure breaks [MS-CFB], and how.</summary>
internal sealed class MalformedCompoundFileException(string message) : Exception(message);

/// <summary>A minimal reader of an OLE compound file's directory, by [MS-CFB]: the header, the
/// FAT sectors the DIFAT names, the directory's sector chain, and the names of the root storage's
/// children. It is the whole of what docproc-ooxml reads of a compound file, enough to tell an
/// encrypted OOXML package, whose root holds an <c>EncryptedPackage</c> stream ([MS-OFFCRYPTO]
/// 2.3.4.4), from a legacy binary document. It reads no stream's content and no mini stream.
/// Every step is bounded by the file's length: each sector is read at most once along the
/// directory's chain and each directory entry at most once in the tree, so a cycle or a reference
/// past the end is malformed, never a loop.</summary>
internal static class CompoundFile
{
    /// <summary>The eight bytes every compound file begins with.</summary>
    public static ReadOnlySpan<byte> Signature => [0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1];

    private const int HeaderSize = 512;
    private const int HeaderDifatEntries = 109;
    private const int EntrySize = 128;
    private const uint MaxRegularSector = 0xFFFFFFFA;
    private const uint EndOfChain = 0xFFFFFFFE;
    private const uint NoStream = 0xFFFFFFFF;
    private const byte StorageObject = 1;
    private const byte StreamObject = 2;
    private const byte RootStorageObject = 5;

    /// <summary>The names of the root storage's children, its storages and streams, in the
    /// order of its directory tree.</summary>
    /// <exception cref="MalformedCompoundFileException">The stream isn't a compound file [MS-CFB]
    /// can read that far.</exception>
    /// <exception cref="IOException">The stream can't be read.</exception>
    public static IReadOnlyList<string> RootChildren(Stream stream) => new Reader(stream).RootChildren();

    private sealed class Reader
    {
        private readonly Stream stream;
        private readonly byte[] header = new byte[HeaderSize];
        private readonly int sectorSize;
        private readonly long sectorCount;
        private readonly uint fatSectors;
        private readonly List<uint> difatSectors = [];
        private readonly HashSet<uint> difatSeen = [];
        private uint nextDifat;

        public Reader(Stream stream)
        {
            this.stream = stream;
            if (stream.Length < HeaderSize)
            {
                throw new MalformedCompoundFileException("shorter than its header");
            }
            stream.Position = 0;
            stream.ReadExactly(header);
            if (!header.AsSpan(0, 8).SequenceEqual(Signature))
            {
                throw new MalformedCompoundFileException("no compound file signature");
            }
            if (U16(header, 28) != 0xFFFE)
            {
                throw new MalformedCompoundFileException("a byte order other than little-endian");
            }
            // Version 3 has 512-byte sectors, version 4 4096-byte ones; nothing else is defined.
            var shift = (U16(header, 26), U16(header, 30)) switch
            {
                (3, 9) => 9,
                (4, 12) => 12,
                var (major, given) => throw new MalformedCompoundFileException($"version {major} with sector shift {given}"),
            };
            sectorSize = 1 << shift;
            // Sector n begins at (n + 1) * sectorSize, the header filling sector -1; a last sector
            // the file cuts short is read as if padded with zeros.
            sectorCount = (stream.Length + sectorSize - 1) / sectorSize - 1;
            fatSectors = U32(header, 44);
            if (fatSectors > sectorCount)
            {
                throw new MalformedCompoundFileException($"{fatSectors} FAT sectors in a file of {sectorCount} sectors");
            }
            nextDifat = U32(header, 68);
        }

        public List<string> RootChildren()
        {
            var directory = DirectoryChain();
            var entriesPerSector = sectorSize / EntrySize;
            var entryCount = (long)directory.Count * entriesPerSector;
            var entry = new byte[EntrySize];

            void Read(uint id)
            {
                if (id >= entryCount)
                {
                    throw new MalformedCompoundFileException($"directory entry {id} past the directory's {entryCount}");
                }
                var offset = Offset(directory[(int)(id / entriesPerSector)]) + id % entriesPerSector * EntrySize;
                ReadAt(offset, entry);
            }

            Read(0);
            if (entry[66] != RootStorageObject)
            {
                throw new MalformedCompoundFileException("a first directory entry that isn't the root storage");
            }
            // The root's children are a red-black tree through each entry's left and right
            // siblings, walked in order; an entry reached twice is a cycle.
            var names = new List<string>();
            var seen = new HashSet<uint> { 0 };
            var pending = new Stack<(uint Id, bool Visit)>();
            pending.Push((U32(entry, 76), false));
            while (pending.TryPop(out var item))
            {
                if (item.Id == NoStream)
                {
                    continue;
                }
                Read(item.Id);
                if (item.Visit)
                {
                    names.Add(Name(entry, item.Id));
                    continue;
                }
                if (!seen.Add(item.Id))
                {
                    throw new MalformedCompoundFileException($"directory entry {item.Id} reached twice");
                }
                if (entry[66] is not (StorageObject or StreamObject))
                {
                    throw new MalformedCompoundFileException($"directory entry {item.Id} of object type {entry[66]} in a storage");
                }
                pending.Push((U32(entry, 72), false));
                pending.Push((item.Id, true));
                pending.Push((U32(entry, 68), false));
            }
            return names;
        }

        // The directory's sectors, in order, through the FAT.
        private List<uint> DirectoryChain()
        {
            var chain = new List<uint>();
            var seen = new HashSet<uint>();
            for (var sector = U32(header, 48); sector != EndOfChain; sector = Fat(sector))
            {
                if (sector > MaxRegularSector || sector >= sectorCount)
                {
                    throw new MalformedCompoundFileException($"directory sector {sector} past the file's {sectorCount}");
                }
                if (!seen.Add(sector))
                {
                    throw new MalformedCompoundFileException($"directory sector {sector} reached twice");
                }
                chain.Add(sector);
            }
            if (chain.Count == 0)
            {
                throw new MalformedCompoundFileException("no directory");
            }
            return chain;
        }

        // The FAT's entry for a sector: the next sector of its chain.
        private uint Fat(uint sector)
        {
            var perSector = (uint)(sectorSize / 4);
            var buffer = new byte[4];
            ReadAt(Offset(FatSector(sector / perSector)) + sector % perSector * 4, buffer);
            return U32(buffer, 0);
        }

        // The sector holding the given FAT sector: the header's DIFAT names the first 109, and a
        // chain of DIFAT sectors the rest, each ending in the next one's number.
        private uint FatSector(uint index)
        {
            if (index >= fatSectors)
            {
                throw new MalformedCompoundFileException($"FAT sector {index} past the header's {fatSectors}");
            }
            uint sector;
            if (index < HeaderDifatEntries)
            {
                sector = U32(header, 76 + (int)index * 4);
            }
            else
            {
                var perDifat = (uint)(sectorSize / 4 - 1);
                var difat = (int)((index - HeaderDifatEntries) / perDifat);
                while (difatSectors.Count <= difat)
                {
                    if (nextDifat > MaxRegularSector || nextDifat >= sectorCount)
                    {
                        throw new MalformedCompoundFileException($"DIFAT sector {nextDifat} past the file's {sectorCount}");
                    }
                    if (!difatSeen.Add(nextDifat))
                    {
                        throw new MalformedCompoundFileException($"DIFAT sector {nextDifat} reached twice");
                    }
                    difatSectors.Add(nextDifat);
                    var next = new byte[4];
                    ReadAt(Offset(nextDifat) + perDifat * 4, next);
                    nextDifat = U32(next, 0);
                }
                var buffer = new byte[4];
                ReadAt(Offset(difatSectors[difat]) + (index - HeaderDifatEntries) % perDifat * 4, buffer);
                sector = U32(buffer, 0);
            }
            if (sector > MaxRegularSector || sector >= sectorCount)
            {
                throw new MalformedCompoundFileException($"FAT sector {index} at sector {sector}, past the file's {sectorCount}");
            }
            return sector;
        }

        private long Offset(uint sector) => ((long)sector + 1) * sectorSize;

        private void ReadAt(long offset, byte[] buffer)
        {
            Array.Clear(buffer);
            stream.Position = offset;
            stream.ReadAtLeast(buffer, buffer.Length, throwOnEndOfStream: false);
        }

        // An entry's name: its length in bytes counts the terminating null, at most 32 UTF-16
        // code units in all.
        private static string Name(byte[] entry, uint id)
        {
            var length = U16(entry, 64);
            if (length is < 2 or > 64 || length % 2 != 0 || U16(entry, length - 2) != 0)
            {
                throw new MalformedCompoundFileException($"directory entry {id} with a name of {length} bytes");
            }
            return Encoding.Unicode.GetString(entry, 0, length - 2);
        }
    }

    private static ushort U16(byte[] bytes, int offset) => BinaryPrimitives.ReadUInt16LittleEndian(bytes.AsSpan(offset));

    private static uint U32(byte[] bytes, int offset) => BinaryPrimitives.ReadUInt32LittleEndian(bytes.AsSpan(offset));
}
