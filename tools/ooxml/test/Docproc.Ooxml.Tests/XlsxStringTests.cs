using Docproc.Ooxml.Reading;

namespace Docproc.Ooxml.Tests;

// The xlsx reader's text and number formats, independent of a package: SpreadsheetML's _xHHHH_
// escapes and the elapsed-time formats (context/design.md, OX-17 and OX-18).
public sealed class XlsxStringTests
{
    [Theory]
    [InlineData("plain", "plain")]
    [InlineData("Tab_x0009_here", "Tab\there")]
    [InlineData("_x000D__x000A_", "\r\n")]
    [InlineData("lower _x00e9_ and upper _x00E9_", "lower é and upper é")]
    [InlineData("_x005F_x0041_", "_x0041_")]
    [InlineData("_x005F_", "_")]
    [InlineData("_x41_ _xZZZZ_ _x0041 x0041_", "_x41_ _xZZZZ_ _x0041 x0041_")]
    [InlineData("_x0041_x0042_", "Ax0042_")]
    public void EscapesAreDecodedLeftToRight(string stored, string text) =>
        Assert.Equal(text, XlsxReader.Unescape(stored));

    [Theory]
    [InlineData("[h]:mm")]
    [InlineData("[hh]:mm:ss")]
    [InlineData("[m]:ss")]
    [InlineData("[ss].000")]
    [InlineData("[Red][h]:mm")]
    public void AnElapsedFormatIsNoDateOrTime(string code) =>
        Assert.Equal((false, false), DateFormat.Parts(code));

    [Theory]
    [InlineData("h:mm", false, true)]
    [InlineData("[Red]yyyy-mm-dd", true, false)]
    [InlineData("[$-409]m/d/yy h:mm", true, true)]
    public void OtherBracketsLeaveTheFormatsParts(string code, bool date, bool time) =>
        Assert.Equal((date, time), DateFormat.Parts(code));
}
