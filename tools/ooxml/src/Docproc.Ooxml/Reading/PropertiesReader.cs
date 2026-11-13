using System.Globalization;
using System.Xml;
using System.Xml.Linq;
using DocumentFormat.OpenXml.Packaging;
using Docproc.Ooxml.Model;
using Docproc.Ooxml.Writing;

namespace Docproc.Ooxml.Reading;

/// <summary>Reads a package's core, extended, and custom properties by the reading rule
/// (context/layers.md, "docproc-ooxml"; context/design.md, OX-13): each part as XML, never
/// through the SDK's typed properties, which parse core dates and hide the stored text. A core
/// value is a string as stored, dates included; an extended value is a number when it is all
/// digits, else a string; a custom value takes its variant's type, a file time as its stored
/// ISO 8601 text. Keys keep their stored order, and a repeated key keeps its first value with a
/// warning. A part that can't be parsed is a warning, never a refusal. The thumbnail is not
/// media.</summary>
/// <remarks>A value is an element's text: one with no text, as an unset core property is
/// written, has no value and is left out, by the envelope's rule of absence, and so is one that
/// holds elements rather than text, such as the extended properties' heading pairs and titles of
/// parts, vectors that are no single value.</remarks>
internal static class PropertiesReader
{
    private const string VariantTypes = "http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes";

    // The places a properties warning names, one for each part: the map it would have filled.
    private const string CorePlace = "core properties";
    private const string AppPlace = "app properties";
    private const string CustomPlace = "custom properties";

    /// <summary>The package's properties, or null when it has none; what was skipped in them is
    /// added to the warnings.</summary>
    public static Properties? Read(OpenXmlPackage package, List<Warning> warnings)
    {
        var core = Map(package.GetPartsOfType<CoreFilePropertiesPart>().FirstOrDefault(), CorePlace, warnings, CoreValue);
        var app = Map(package.GetPartsOfType<ExtendedFilePropertiesPart>().FirstOrDefault(), AppPlace, warnings, AppValue);
        var custom = Map(package.GetPartsOfType<CustomFilePropertiesPart>().FirstOrDefault(), CustomPlace, warnings, CustomValue);
        return core is null && app is null && custom is null ? null : new Properties(core, app, custom);
    }

    // A part's properties in stored order, or null when it is absent, can't be parsed, or holds
    // none; reading a value yields its key and value, or null for an element that has none.
    private static List<Property>? Map(OpenXmlPart? part, string place, List<Warning> warnings, Func<XElement, (string Key, PropertyValue Value)?> read)
    {
        if (part is null)
        {
            return null;
        }
        XDocument document;
        try
        {
            using var stream = part.GetStream(FileMode.Open, FileAccess.Read);
            document = XDocument.Load(stream);
        }
        catch (XmlException e)
        {
            warnings.Add(new Warning(place, $"not read: {e.Message}"));
            return null;
        }
        var properties = new List<Property>();
        var keys = new HashSet<string>(StringComparer.Ordinal);
        foreach (var element in document.Root?.Elements() ?? [])
        {
            if (read(element) is not var (key, value))
            {
                continue;
            }
            if (!keys.Add(key))
            {
                warnings.Add(new Warning(place, $"\"{key}\" is repeated: its first value is kept"));
                continue;
            }
            properties.Add(new Property(key, value));
        }
        return properties.Count > 0 ? properties : null;
    }

    // A core property: its element's name, and its text as stored.
    private static (string, PropertyValue)? CoreValue(XElement element) =>
        Text(element) is { } text ? (element.Name.LocalName, new PropertyValue(PropertyValueType.Text, text)) : null;

    // An extended property: its element's name, and its text, a number when it is all digits.
    private static (string, PropertyValue)? AppValue(XElement element) =>
        Text(element) is { } text
            ? (element.Name.LocalName, text.All(char.IsAsciiDigit) ? new PropertyValue(PropertyValueType.Number, Integer(text)) : new PropertyValue(PropertyValueType.Text, text))
            : null;

    // A custom property: its name, and its variant's value by the variant's type. A property with
    // no name, or whose variant is a vector, an array, or empty, has no value.
    private static (string, PropertyValue)? CustomValue(XElement property)
    {
        if (property.Attribute("name")?.Value is not { Length: > 0 } name
            || property.Elements().FirstOrDefault() is not { } variant
            || variant.Name.Namespace != VariantTypes
            || Text(variant) is not { } text)
        {
            return null;
        }
        return (name, Variant(variant.Name.LocalName, text));
    }

    // A variant's value: its integer and real types a number, its boolean true or false, and any
    // other, strings, dates, file times, currency, and errors among them, a string as stored. A
    // number or boolean whose text is no such value stays the string it is stored as.
    private static PropertyValue Variant(string type, string text)
    {
        switch (type)
        {
            case "i1" or "i2" or "i4" or "i8" or "int" or "ui1" or "ui2" or "ui4" or "ui8" or "uint":
                if (decimal.TryParse(text, NumberStyles.AllowLeadingSign, CultureInfo.InvariantCulture, out var integer))
                {
                    return new PropertyValue(PropertyValueType.Number, integer.ToString(CultureInfo.InvariantCulture));
                }
                break;
            case "r4" or "r8" or "decimal":
                if (double.TryParse(text, NumberStyles.Float, CultureInfo.InvariantCulture, out var real) && double.IsFinite(real))
                {
                    return new PropertyValue(PropertyValueType.Number, real.ToString("R", CultureInfo.InvariantCulture));
                }
                break;
            case "bool":
                if (text is "true" or "1")
                {
                    return new PropertyValue(PropertyValueType.Boolean, "true");
                }
                if (text is "false" or "0")
                {
                    return new PropertyValue(PropertyValueType.Boolean, "false");
                }
                break;
            default:
                break;
        }
        return new PropertyValue(PropertyValueType.Text, text);
    }

    // All digits as a JSON number: its leading zeros dropped, which a JSON number can't have.
    private static string Integer(string digits)
    {
        var trimmed = digits.TrimStart('0');
        return trimmed.Length > 0 ? trimmed : "0";
    }

    // An element's text, or null when it has none or holds elements rather than text.
    private static string? Text(XElement element) =>
        !element.HasElements && element.Value.Length > 0 ? element.Value : null;
}
