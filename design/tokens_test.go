package design

import (
	"fmt"
	"strings"
	"testing"
)

// TestContrastRatios is the point of this package: every documented colour
// pairing must meet the contrast requirement recorded next to it.
func TestContrastRatios(t *testing.T) {
	tokens, err := Load(TokenFile)
	if err != nil {
		t.Fatalf("load tokens: %v", err)
	}

	var table strings.Builder
	table.WriteString("\n| Foreground | Background | Contrast | Required | Use |\n")
	table.WriteString("|---|---|---|---|---|\n")

	for _, pair := range tokens.Pairs {
		fg, err := tokens.Lookup(pair.Foreground)
		if err != nil {
			t.Fatalf("%v", err)
		}
		bg, err := tokens.Lookup(pair.Background)
		if err != nil {
			t.Fatalf("%v", err)
		}

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
	tokens, err := Load(TokenFile)
	if err != nil {
		t.Fatalf("load tokens: %v", err)
	}

	for scale, shades := range tokens.Colors {
		for shade, value := range shades {
			if _, err := parseHexValue(value); err != nil {
				t.Errorf("%s.%s: %v", scale, shade, err)
			}
		}
	}
}

// TestNoDuplicateHexValues catches two different token names pointing at the
// same colour, which usually means a scale was pasted and edited by hand.
func TestNoDuplicateHexValues(t *testing.T) {
	tokens, err := Load(TokenFile)
	if err != nil {
		t.Fatalf("load tokens: %v", err)
	}

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
	tokens, err := Load(TokenFile)
	if err != nil {
		t.Fatalf("load tokens: %v", err)
	}

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

// TestCSSVariablesAreComplete checks that every token reaches the
// stylesheet, so a component can reference any shade without a fallback.
func TestCSSVariablesAreComplete(t *testing.T) {
	tokens, err := Load(TokenFile)
	if err != nil {
		t.Fatalf("load tokens: %v", err)
	}

	variables, err := CSSVariables(tokens)
	if err != nil {
		t.Fatalf("CSSVariables: %v", err)
	}

	for scale, shades := range tokens.Colors {
		for shade := range shades {
			name := "--" + scale + "-" + shade
			if !strings.Contains(variables, name+":") {
				t.Errorf("CSS variable %s is missing from the generated stylesheet", name)
			}
		}
	}
}

// TestCSSVariablesAreStable checks that the generated output does not
// reorder between calls, which would make the stylesheet churn.
func TestCSSVariablesAreStable(t *testing.T) {
	tokens, err := Load(TokenFile)
	if err != nil {
		t.Fatalf("load tokens: %v", err)
	}

	first, err := CSSVariables(tokens)
	if err != nil {
		t.Fatalf("CSSVariables: %v", err)
	}
	second, err := CSSVariables(tokens)
	if err != nil {
		t.Fatalf("CSSVariables: %v", err)
	}
	if first != second {
		t.Error("CSSVariables is not deterministic")
	}
}

// TestLookupRejectsUnknownReferences catches typos in a token reference.
func TestLookupRejectsUnknownReferences(t *testing.T) {
	tokens, err := Load(TokenFile)
	if err != nil {
		t.Fatalf("load tokens: %v", err)
	}

	for _, reference := range []string{"brand", "brand.1200", "unknown.600", "brand.600.700"} {
		if _, err := tokens.Lookup(reference); err == nil {
			t.Errorf("Lookup(%q) = nil error, want an error", reference)
		}
	}
}
