// Package design holds the design tokens for the clinic booking site.
//
// The tokens live in tokens.json so that the stylesheet, the component
// library and this test all read from one source of truth. The test
// enforces the WCAG 2.2 contrast requirements for every documented colour
// pairing, which means a palette change that breaks accessibility fails
// `go test ./...` rather than being discovered by a screen reader user.
//
// Run `go test ./design/ -v` to print the contrast table with the computed
// ratios. The numbers in design.md come from that output.
package design

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TokenFile is the path to the token definitions.
const TokenFile = "tokens.json"

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
func Load(t *testing.T) Tokens {
	t.Helper()

	raw, err := os.ReadFile(TokenFile)
	if err != nil {
		t.Fatalf("read %s: %v", TokenFile, err)
	}

	var tokens Tokens
	if err := json.Unmarshal(raw, &tokens); err != nil {
		t.Fatalf("parse %s: %v", TokenFile, err)
	}
	return tokens
}

// Lookup resolves a dotted token reference such as "brand.700".
func (tok Tokens) Lookup(t *testing.T, reference string) string {
	t.Helper()

	parts := strings.SplitN(reference, ".", 2)
	if len(parts) != 2 {
		t.Fatalf("token reference %q must be in the form scale.shade", reference)
	}

	scale, ok := tok.Colors[parts[0]]
	if !ok {
		t.Fatalf("token reference %q names an unknown scale %q", reference, parts[0])
	}
	shade, ok := scale[parts[1]]
	if !ok {
		t.Fatalf("token reference %q names an unknown shade %q", reference, parts[1])
	}
	return shade
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

// linearise converts an 8-bit sRGB channel to linear light.
func linearise(channel float64) float64 {
	if channel <= 0.03928 {
		return channel / 12.92
	}
	return math.Pow((channel+0.055)/1.055, 2.4)
}

// parseHex splits a six digit hex colour into its channels.
func parseHex(hex string) (r, g, b float64, err error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(trimmed) != 6 {
		return 0, 0, 0, fmt.Errorf("colour %q is not a six digit hex value", hex)
	}

	value, err := strconv.ParseUint(trimmed, 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("colour %q is not valid hex: %w", hex, err)
	}
	return float64((value>>16)&0xFF) / 255, float64((value>>8)&0xFF) / 255, float64(value&0xFF) / 255, nil
}

// TestContrastRatios is the point of this package: every documented colour
// pairing must meet the contrast requirement recorded next to it.
func TestContrastRatios(t *testing.T) {
	tokens := Load(t)

	var table strings.Builder
	table.WriteString("\n| Foreground | Background | Contrast | Required | Use |\n")
	table.WriteString("|---|---|---|---|---|\n")

	for _, pair := range tokens.Pairs {
		fg := tokens.Lookup(t, pair.Foreground)
		bg := tokens.Lookup(t, pair.Background)

		ratio, err := ContrastRatio(fg, bg)
		if err != nil {
			t.Fatalf("%s on %s: %v", pair.Foreground, pair.Background, err)
		}

		if ratio+0.005 < pair.Minimum {
			t.Errorf("%s (%s) on %s (%s) has a contrast ratio of %.2f:1, which is below the required %.1f:1 — %s",
				pair.Foreground, fg, pair.Background, bg, ratio, pair.Minimum, pair.Use)
		}

		table.WriteString(fmt.Sprintf("| `%s` | `%s` | **%.2f:1** | %.1f:1 | %s |\n",
			pair.Foreground, pair.Background, ratio, pair.Minimum, pair.Use))
	}

	t.Logf("\nVerified contrast ratios\n%s", table.String())
}

// TestEveryColourIsValidHex catches a typo in a hex value.
func TestEveryColourIsValidHex(t *testing.T) {
	tokens := Load(t)

	for scale, shades := range tokens.Colors {
		for shade, value := range shades {
			if _, _, _, err := parseHex(value); err != nil {
				t.Errorf("%s.%s: %v", scale, shade, err)
			}
		}
	}
}

// TestNoDuplicateHexValues catches two different token names pointing at the
// same colour, which usually means a scale was pasted and edited by hand.
func TestNoDuplicateHexValues(t *testing.T) {
	tokens := Load(t)

	seen := map[string]string{}
	for scale, shades := range tokens.Colors {
		for shade, value := range shades {
			key := strings.ToUpper(value)
			if previous, exists := seen[key]; exists {
				t.Errorf("%s.%s and %s are both %s", scale, shade, previous, value)
				continue
			}
			seen[key] = scale + "." + shade
		}
	}
}

// TestScalesAreOrdered guards against a scale whose shades run the wrong way
// round, which is an easy mistake to make when hand-editing a ramp.
func TestScalesAreOrdered(t *testing.T) {
	tokens := Load(t)

	for _, scale := range []string{"brand", "neutral", "success", "warning", "danger", "info"} {
		shades, ok := tokens.Colors[scale]
		if !ok {
			t.Errorf("scale %q is missing", scale)
			continue
		}

		previous := 2.0
		for _, shade := range []string{"50", "100", "200", "300", "400", "500", "600", "700", "800", "900", "950"} {
			value, ok := shades[shade]
			if !ok {
				t.Errorf("scale %q is missing shade %q", scale, shade)
				continue
			}
			luminance, err := RelativeLuminance(value)
			if err != nil {
				t.Errorf("%s.%s: %v", scale, shade, err)
				continue
			}
			if luminance > previous {
				t.Errorf("scale %q shade %q (luminance %.4f) is lighter than the one before it (%.4f); scales run from lightest to darkest",
					scale, shade, luminance, previous)
			}
			previous = luminance
		}
	}
}
