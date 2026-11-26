using System.Text;
using Docproc.Mail.Generator;

Encoding.RegisterProvider(CodePagesEncodingProvider.Instance);

return args switch
{
    ["fixtures", var dir] => Fixtures.Run(dir),
    ["tnef", var dir] => Tnef.Run(dir),
    _ => Usage(),
};

static int Usage()
{
    Console.Error.WriteLine("usage: generator fixtures <tests/fixtures/generated>\n       generator tnef <tests/fixtures/tnef>");
    return 2;
}
