using System.Text;
using Docproc.Mail.Generator;

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
