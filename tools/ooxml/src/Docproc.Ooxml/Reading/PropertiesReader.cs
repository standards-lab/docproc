using System.Globalization;
using System.Xml;
using System.Xml.Linq;
using DocumentFormat.OpenXml.Packaging;
using Docproc.Ooxml.Model;
using Docproc.Ooxml.Writing;

namespace Docproc.Ooxml.Reading;

// See docs/tool-contract.md, "docproc-ooxml".
internal static class PropertiesReader
{
    private const string VariantTypes = "http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes";

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

    private static (string, PropertyValue)? CoreValue(XElement element) =>
        Text(element) is { } text ? (element.Name.LocalName, new PropertyValue(PropertyValueType.Text, text)) : null;

    private static (string, PropertyValue)? AppValue(XElement element) =>
        Text(element) is { } text
            ? (element.Name.LocalName, text.All(char.IsAsciiDigit) ? new PropertyValue(PropertyValueType.Number, Integer(text)) : new PropertyValue(PropertyValueType.Text, text))
            : null;

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

    private static string Integer(string digits)
    {
        var trimmed = digits.TrimStart('0');
        return trimmed.Length > 0 ? trimmed : "0";
    }

    private static string? Text(XElement element) =>
        !element.HasElements && element.Value.Length > 0 ? element.Value : null;
}
