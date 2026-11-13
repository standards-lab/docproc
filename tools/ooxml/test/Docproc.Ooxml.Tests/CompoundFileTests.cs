using System.Buffers.Binary;
using System.Text;
using Docproc.Ooxml.Reading;

namespace Docproc.Ooxml.Tests;

// The compound-file directory reader's seam, independent of the CLI: compound files built here
// byte by byte from [MS-CFB], never one the reader wrote, valid ones of both versions and
// malformed ones, each refused as malformed.
public sealed class CompoundFileTests
{
    private const uint EndOfChain = 0xFFFFFFFE;
    private const uint FatSector = 0xFFFFFFFD;
    private const uint NoStream = 0xFFFFFFFF;

    // An encrypted package's root, as [MS-OFFCRYPTO] has it: the children come back in the
    // directory tree's order, through left and right siblings.
    [Theory]
    [InlineData(3)]
    [InlineData(4)]
    public void TheRootsChildrenAreNamed(int version)
    {
        var image = new Image(version, sectors: 2);
        image.Fat(index: 0, sector: 0);
        image.Chain(1);
        image.Directory(1);
        image.Entry(0, "Root Entry", 5, child: 2);
        image.Entry(1, "EncryptionInfo", 2);
        image.Entry(2, "EncryptedPackage", 2, left: 1, right: 3);
        image.Entry(3, "\u0006DataSpaces", 1);

        Assert.Equal(["EncryptionInfo", "EncryptedPackage", "\u0006DataSpaces"], image.RootChildren());
    }

    // Only the root's own children: a storage's children are its own.
    [Fact]
    public void AStoragesChildrenAreNotTheRoots()
    {
        var image = new Image(3, sectors: 2);
        image.Fat(0, 0);
        image.Chain(1);
        image.Directory(1);
        image.Entry(0, "Root Entry", 5, child: 1);
        image.Entry(1, "Storage", 1, child: 2);
        image.Entry(2, "EncryptedPackage", 2);

        Assert.Equal(["Storage"], image.RootChildren());
    }

    [Fact]
    public void AnEmptyRootHasNoChildren()
    {
        var image = new Image(3, sectors: 2);
        image.Fat(0, 0);
        image.Chain(1);
        image.Directory(1);
        image.Entry(0, "Root Entry", 5);

        Assert.Empty(image.RootChildren());
    }

    // A directory of two sectors, the second found through the FAT, and the FAT sector for it
    // found through the DIFAT's chain past the header's 109: version 3's sectors of 512 bytes,
    // each FAT sector covering 128 sectors, so the second directory sector sits past sector
    // 109 * 128.
    [Fact]
    public void TheDirectorysChainIsFollowedThroughTheDifat()
    {
        const uint second = 14_000;
        var image = new Image(3, sectors: (int)second + 1);
        image.DifatChain(0, next: EndOfChain);
        for (uint i = 0; i < 110; i++)
        {
            image.Fat(i, 1 + i, difatSector: i >= 109 ? 0u : null);
        }
        image.Chain(111, second);
        image.Directory(111, second);
        image.Entry(0, "Root Entry", 5, child: 6);
        for (uint id = 1; id <= 6; id++)
        {
            image.Entry(id, $"Stream{id}", 2, left: id > 1 ? id - 1 : NoStream);
        }

        Assert.Equal(["Stream1", "Stream2", "Stream3", "Stream4", "Stream5", "Stream6"], image.RootChildren());
    }

    public static TheoryData<string, Action<Image>> Malformed => new()
    {
        { "shorter than its header", image => image.Truncate(100) },
        { "no compound file signature", image => image.Header(0, 0) },
        { "a byte order other than little-endian", image => image.Header16(28, 0xFEFF) },
        { "version 3 with sector shift 12", image => image.Header16(30, 12) },
        { "version 5 with sector shift 9", image => image.Header16(26, 5) },
        { "9 FAT sectors in a file of 2 sectors", image => image.Header(44, 9) },
        { "directory sector 7 past the file's 2", image => image.Header(48, 7) },
        { "no directory", image => image.Header(48, EndOfChain) },
        { "directory sector 1 reached twice", image => image.Link(1, 1) },
        { "FAT sector 0 past the header's 0", image => image.Header(44, 0) },
        { "FAT sector 0 at sector 9, past the file's 2", image => image.Header(76, 9) },
        { "a first directory entry that isn't the root storage", image => image.Entry(0, "Root Entry", 1, child: 1) },
        { "directory entry 9 past the directory's 4", image => image.Entry(0, "Root Entry", 5, child: 9) },
        { "directory entry 1 reached twice", image => image.Entry(1, "A", 2, right: 1) },
        { "directory entry 0 reached twice", image => image.Entry(1, "A", 2, left: 0) },
        { "directory entry 1 of object type 0 in a storage", image => image.Entry(1, "A", 0) },
        { "directory entry 1 with a name of 66 bytes", image => image.EntryNameLength(1, 66) },
        { "directory entry 1 with a name of 3 bytes", image => image.EntryNameLength(1, 3) },
        { "directory entry 1 with a name of 0 bytes", image => image.EntryNameLength(1, 0) },
    };

    // Each malformed file is refused with what is wrong, never read past its end or around a
    // cycle.
    [Theory]
    [MemberData(nameof(Malformed))]
    public void AMalformedCompoundFileIsRefused(string reason, Action<Image> breakIt)
    {
        var image = new Image(3, sectors: 2);
        image.Fat(0, 0);
        image.Chain(1);
        image.Directory(1);
        image.Entry(0, "Root Entry", 5, child: 1);
        image.Entry(1, "A", 2);
        breakIt(image);

        var e = Assert.Throws<MalformedCompoundFileException>(() => image.RootChildren());
        Assert.Equal(reason, e.Message);
    }

    // A DIFAT chain that loops, or names a sector past the file, is malformed: the directory's
    // sector sits where the FAT sector that covers it is the DIFAT chain's second sector's to
    // name, and the first names itself, or a sector past the end, as the next.
    [Theory]
    [InlineData(110u, "DIFAT sector 110 reached twice")]
    [InlineData(40_000u, "DIFAT sector 40000 past the file's 30209")]
    public void ADifatChainThatLoopsOrLeavesTheFileIsRefused(uint next, string reason)
    {
        const uint far = 236 * 128;
        var image = new Image(3, sectors: (int)far + 1);
        for (uint i = 0; i < 109; i++)
        {
            image.Fat(i, 1 + i);
        }
        image.DifatChain(110, next);
        image.Header(44, 300);
        image.Header(48, far);

        var e = Assert.Throws<MalformedCompoundFileException>(() => image.RootChildren());
        Assert.Equal(reason, e.Message);
    }

    // A last sector the file cuts short reads as zeros past the end.
    [Fact]
    public void AShortLastSectorReads()
    {
        var image = new Image(3, sectors: 2);
        image.Fat(0, 0);
        image.Chain(1);
        image.Directory(1);
        image.Entry(0, "Root Entry", 5, child: 1);
        image.Entry(1, "A", 2);
        image.Truncate(512 + 512 + 256);

        Assert.Equal(["A"], image.RootChildren());
    }

    /// <summary>A compound file under construction: its header, sectors of the version's size,
    /// FAT sectors listed in the header's DIFAT or a DIFAT sector, and directory entries.</summary>
    public sealed class Image
    {
        private readonly int sectorSize;
        private readonly List<uint> fat = [];
        private readonly List<uint> directory = [];
        private byte[] bytes;

        public Image(int version, int sectors)
        {
            sectorSize = version == 3 ? 512 : 4096;
            bytes = new byte[(sectors + 1) * sectorSize];
            CompoundFile.Signature.CopyTo(bytes);
            Header16(24, 0x3E);
            Header16(26, (ushort)version);
            Header16(28, 0xFFFE);
            Header16(30, (ushort)(version == 3 ? 9 : 12));
            Header16(32, 6);
            Header(48, EndOfChain);
            Header(56, 4096);
            Header(60, EndOfChain);
            Header(68, EndOfChain);
            for (var i = 0; i < 109; i++)
            {
                Header(76 + i * 4, 0xFFFFFFFF);
            }
            // Every sector's FAT entry is free until something claims it.
            Array.Fill(bytes, (byte)0xFF, sectorSize, bytes.Length - sectorSize);
        }

        public void Header(int offset, uint value) => BinaryPrimitives.WriteUInt32LittleEndian(bytes.AsSpan(offset), value);

        public void Header16(int offset, ushort value) => BinaryPrimitives.WriteUInt16LittleEndian(bytes.AsSpan(offset), value);

        public void Truncate(int length) => bytes = bytes[..length];

        // The FAT's index-th sector is the given sector, listed in the header's DIFAT, or in the
        // given DIFAT sector past the header's 109.
        public void Fat(uint index, uint sector, uint? difatSector = null)
        {
            Clear(sector);
            if (difatSector is { } difat)
            {
                Put(Offset(difat) + (index - 109) * 4, sector);
            }
            else
            {
                Header(76 + (int)index * 4, sector);
            }
            fat.Add(sector);
            Header(44, (uint)fat.Count);
            Link(sector, FatSector);
        }

        public void DifatChain(uint sector, uint next)
        {
            Clear(sector);
            Header(68, sector);
            Header(72, 1);
            Put(Offset(sector) + (uint)(sectorSize - 4), next);
        }

        // The sectors' chain in the FAT, ending in the last.
        public void Chain(params uint[] sectors)
        {
            for (var i = 0; i < sectors.Length; i++)
            {
                Link(sectors[i], i + 1 < sectors.Length ? sectors[i + 1] : EndOfChain);
            }
        }

        public void Link(uint sector, uint next)
        {
            var perSector = (uint)(sectorSize / 4);
            Put(Offset(fat[(int)(sector / perSector)]) + sector % perSector * 4, next);
        }

        public void Directory(params uint[] sectors)
        {
            Header(48, sectors[0]);
            directory.Clear();
            foreach (var sector in sectors)
            {
                directory.Add(sector);
                Array.Fill(bytes, (byte)0, (int)Offset(sector), sectorSize);
                for (var slot = 0; slot < sectorSize / 128; slot++)
                {
                    var at = (int)Offset(sector) + slot * 128;
                    BinaryPrimitives.WriteUInt32LittleEndian(bytes.AsSpan(at + 68), NoStream);
                    BinaryPrimitives.WriteUInt32LittleEndian(bytes.AsSpan(at + 72), NoStream);
                    BinaryPrimitives.WriteUInt32LittleEndian(bytes.AsSpan(at + 76), NoStream);
                }
            }
        }

        public void Entry(uint id, string name, byte type, uint left = NoStream, uint right = NoStream, uint child = NoStream)
        {
            var at = EntryOffset(id);
            Array.Fill(bytes, (byte)0, at, 64);
            var encoded = Encoding.Unicode.GetBytes(name);
            encoded.CopyTo(bytes, at);
            BinaryPrimitives.WriteUInt16LittleEndian(bytes.AsSpan(at + 64), (ushort)(encoded.Length + 2));
            bytes[at + 66] = type;
            BinaryPrimitives.WriteUInt32LittleEndian(bytes.AsSpan(at + 68), left);
            BinaryPrimitives.WriteUInt32LittleEndian(bytes.AsSpan(at + 72), right);
            BinaryPrimitives.WriteUInt32LittleEndian(bytes.AsSpan(at + 76), child);
        }

        public void EntryNameLength(uint id, ushort length) =>
            BinaryPrimitives.WriteUInt16LittleEndian(bytes.AsSpan(EntryOffset(id) + 64), length);

        public List<string> RootChildren()
        {
            using var stream = new MemoryStream(bytes);
            return [.. CompoundFile.RootChildren(stream)];
        }

        private int EntryOffset(uint id)
        {
            var perSector = (uint)(sectorSize / 128);
            return (int)(Offset(directory[(int)(id / perSector)]) + id % perSector * 128);
        }

        private void Clear(uint sector) => Array.Fill(bytes, (byte)0xFF, (int)Offset(sector), sectorSize);

        private long Offset(uint sector) => ((long)sector + 1) * sectorSize;

        private void Put(long offset, uint value) => BinaryPrimitives.WriteUInt32LittleEndian(bytes.AsSpan((int)offset), value);
    }
}
