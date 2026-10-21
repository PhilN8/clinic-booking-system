// Package design holds the design tokens for the clinic booking site.
//
// The tokens live in tokens.json so that the stylesheet, the components and
// the test suite all read from one source of truth. The test enforces the
// WCAG 2.2 contrast requirements for every documented colour pairing, which
// means a palette change that breaks accessibility fails the build rather
// than being discovered by a screen reader user.
//
// Run `go test ./design/ -v` to print the contrast table with the computed
// ratios. The numbers in design.md come from that output.
package design

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

// TokenFile is the path to the token definitions.
const TokenFile = "tokens.json"

//go:embed tokens.json
var embeddedTokens []byte

// Embedded returns the tokens compiled into the binary.
//
// The site needs the palette at runtime, and the container has no working
// directory file to read, so the tokens travel with the code.
func Embedded() (Tokens, error) {
	var tokens Tokens
	if err := json.Unmarshal(embeddedTokens, &tokens); err != nil {
		return Tokens{}, fmt.Errorf("design: parse embedded tokens: %w", err)
	}
	return tokens, nil
}

// Color is a scale of shades for one hue.
type Color map[string]string

// Pair is a foreground and background combination with the contrast
// requirement it has to meet.
type Pair struct {
	Foreground string  `json:"fg"`
	Background string  `json:"bg"`
	Use        string  `json:"use"`
	Minimum    float64 `json:"min"`
}

// Tokens is the whole token file.
type Tokens struct {
	Name    string           `json:"name"`
	Version string           `json:"version"`
	Colors  map[string]Color `json:"colors"`
	Pairs   []Pair           `json:"pairs"`
}

// Load reads the tokens from disk.
func Load(path string) (Tokens, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Tokens{}, fmt.Errorf("design: read %s: %w", path, err)
	}

	var tokens Tokens
	if err := json.Unmarshal(raw, &tokens); err != nil {
		return Tokens{}, fmt.Errorf("design: parse %s: %w", path, err)
	}
	return tokens, nil
}

// Lookup resolves a dotted token reference such as "brand.700".
func (tok Tokens) Lookup(reference string) (string, error) {
	parts := strings.SplitN(reference, ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("design: token reference %q must be in the form scale.shade", reference)
	}

	scale, ok := tok.Colors[parts[0]]
	if !ok {
		return "", fmt.Errorf("design: token reference %q names an unknown scale %q", reference, parts[0])
	}
	shade, ok := scale[parts[1]]
	if !ok {
		return "", fmt.Errorf("design: token reference %q names an unknown shade %q", reference, parts[1])
	}
	return shade, nil
}

// RelativeLuminance returns the WCAG relative luminance of a hex colour.
func RelativeLuminance(hex string) (float64, error) {
	r, g, b, err := parseHex(hex)
	if err != nil {
		return 0, err
	}
	return 0.2126*linearise(r) + 0.7152*linearise(g) + 0.0722*linearise(b), nil
}

// ContrastRatio returns the WCAG contrast ratio between two hex colours.
func ContrastRatio(foreground, background string) (float64, error) {
	lumFg, err := RelativeLuminance(foreground)
	if err != nil {
		return 0, err
	}
	lumBg, err := RelativeLuminance(background)
	if err != nil {
		return 0, err
	}

	lighter, darker := lumFg, lumBg
	if lighter < darker {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05), nil
}

// CSSVariables renders the palette as CSS custom properties, so the
// stylesheet references tokens rather than repeating hex values.
//
// The scales and their shades are emitted in a stable order, which keeps the
// output diffable.
func CSSVariables(tok Tokens) (string, error) {
	var out strings.Builder

	names := make([]string, 0, len(tok.Colors))
	for name := range tok.Colors {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		shades := make([]string, 0, len(tok.Colors[name]))
		for shade := range tok.Colors[name] {
			shades = append(shades, shade)
		}
		sort.Slice(shades, func(i, j int) bool {
			return shadeOrder(shades[i]) < shadeOrder(shades[j])
		})

		for _, shade := range shades {
			value, err := parseHexValue(tok.Colors[name][shade])
			if err != nil {
				return "", fmt.Errorf("design: %s.%s: %w", name, shade, err)
			}
			fmt.Fprintf(&out, "  --%s-%s: %s;\n", name, shade, value)
		}
	}

	return out.String(), nil
}

// shadeOrder sorts shade names numerically so 50 comes before 100 and 950
// comes last.
func shadeOrder(shade string) int {
	value, err := strconv.Atoi(shade)
	if err != nil {
		return 1 << 30
	}
	return value
}

// linearise converts an 8-bit sRGB channel to linear light.
func linearise(channel float64) float64 {
	if channel <= 0.03928 {
		return channel / 12.92
	}
	return math.Pow((channel+0.055)/1.055, 2.4)
}

// parseHex splits a six digit hex colour into its channels.
func parseHex(hex string) (r, g, b float64, err error) {
	value, err := parseHexValue(hex)
	if err != nil {
		return 0, 0, 0, err
	}
	number, err := strconv.ParseUint(strings.TrimPrefix(value, "#"), 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("design: colour %q is not valid hex: %w", hex, err)
	}
	return float64((number>>16)&0xFF) / 255, float64((number>>8)&0xFF) / 255, float64(number&0xFF) / 255, nil
}

// parseHexValue normalises a hex colour to its upper case, hash prefixed form.
func parseHexValue(hex string) (string, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(trimmed) != 6 {
		return "", fmt.Errorf("colour %q is not a six digit hex value", hex)
	}
	if _, err := strconv.ParseUint(trimmed, 16, 32); err != nil {
		return "", fmt.Errorf("colour %q is not valid hex: %w", hex, err)
	}
	return "#" + strings.ToUpper(trimmed), nil
}
