package inspect

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

func (in *inspection) svg(ctx context.Context) error {
	d := xmlDecoder(io.NewSectionReader(in.file, 0, in.size))
	d.Strict = false
	for n := 0; ; n++ {
		if n%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			in.flag(SVGUnreadable, fmt.Sprintf("ended at line %d: %s", line(d), clip(err.Error())))
			return nil
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		at := fmt.Sprintf("<%s> at line %d", start.Name.Local, line(d))
		switch strings.ToLower(start.Name.Local) {
		case "script":
			in.flag(SVGScript, at)
		case "foreignobject":
			in.flag(SVGForeignObject, at)
		}
		for _, a := range start.Attr {
			local := strings.ToLower(a.Name.Local)
			if strings.HasPrefix(local, "on") {
				in.flag(SVGEventHandler, fmt.Sprintf("%s on %s", a.Name.Local, at))
			}
			if local == "href" {
				v := strings.TrimSpace(a.Value)
				if !strings.HasPrefix(v, "#") && !strings.HasPrefix(strings.ToLower(v), "data:") {
					in.flag(SVGExternalHref, fmt.Sprintf("%s=%q on %s", attrName(a.Name), clip(v), at))
				}
			}
		}
	}
}

const xlinkNS = "http://www.w3.org/1999/xlink"

func attrName(n xml.Name) string {
	if n.Space == xlinkNS || n.Space == "xlink" {
		return "xlink:" + n.Local
	}
	return n.Local
}

func line(d *xml.Decoder) int {
	l, _ := d.InputPos()
	return l
}

const maxEvidence = 120

func clip(s string) string {
	if len(s) <= maxEvidence {
		return s
	}
	cut := maxEvidence
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
