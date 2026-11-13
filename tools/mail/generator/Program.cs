// The fixtures generator of docproc-mail: hand-written .eml and item sources in, .pst and .msg
// fixtures out. Its one mode, `fixtures`, builds the committed set.

using System.Text;
using Docproc.Mail.Generator;

// .NET has only the Unicode and Latin-1 encodings built in. MimeKit registers the code pages
// itself; MsgKit doesn't, and fails on a windows-1251 or GB2312 source without them.
Encoding.RegisterProvider(CodePagesEncodingProvider.Instance);

return args switch
{
    ["fixtures", var dir] => Fixtures.Run(dir),
    _ => Usage(),
};

static int Usage()
{
    Console.Error.WriteLine("usage: generator fixtures <tests/fixtures/generated>");
    return 2;
}
