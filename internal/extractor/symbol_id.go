package extractor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var whitespaceRe = regexp.MustCompile(`\s+`)

// BuildStableSymbolID creates a deterministic symbol ID.
// The ID is derived from semantic-ish identity fields and a canonical signature hash.
func BuildStableSymbolID(unit *CodeUnit) string {
	if unit == nil {
		return ""
	}

	lang := strings.TrimSpace(unit.Language)
	if lang == "" {
		lang = "unknown"
	}

	pkg := strings.TrimSpace(unit.Package)
	if pkg == "" {
		pkg = "_"
	}

	kind := strings.TrimSpace(unit.UnitType)
	if kind == "" {
		kind = "symbol"
	}

	name := strings.TrimSpace(unit.Name)
	if name == "" {
		name = "_"
	}

	receiver := canonicalize(extractReceiver(unit))
	signature := canonicalize(extractSignature(unit))
	if signature == "" {
		signature = canonicalize(unit.Content)
	}

	fingerprint := strings.Join([]string{
		lang,
		pkg,
		kind,
		receiver,
		name,
		signature,
	}, "|")

	sum := sha256.Sum256([]byte(fingerprint))
	short := hex.EncodeToString(sum[:8])
	return fmt.Sprintf("%s/%s:%s:%s:%s", lang, pkg, kind, name, short)
}

func extractReceiver(unit *CodeUnit) string {
	if unit == nil || unit.Details == nil {
		return ""
	}

	switch d := unit.Details.(type) {
	case GoFunctionDetails:
		return d.Receiver
	case *GoFunctionDetails:
		if d != nil {
			return d.Receiver
		}
	}
	return ""
}

func extractSignature(unit *CodeUnit) string {
	if unit == nil || unit.Details == nil {
		return ""
	}

	switch d := unit.Details.(type) {
	case GoFunctionDetails:
		return d.Signature
	case *GoFunctionDetails:
		if d != nil {
			return d.Signature
		}
	}
	return ""
}

func canonicalize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return whitespaceRe.ReplaceAllString(s, " ")
}
