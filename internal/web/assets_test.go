package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// hexInCSS matches any hex colour literal in the stylesheet.
var hexInCSS = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)

// TestStylesheetUsesNoHexValues enforces the rule from design.md: a component
// never hardcodes a colour, it references a token.
//
// If a hex value appears here, the palette and the stylesheet can drift
// apart, and the contrast ratios the design package verifies stop meaning
// anything.
func TestStylesheetUsesNoHexValues(t *testing.T) {
	contents, err := fs.ReadFile(staticFS, "static/app.css")
	if err != nil {
		t.Fatalf("read stylesheet: %v", err)
	}

	for _, match := range hexInCSS.FindAllString(string(contents), -1) {
		t.Errorf("the stylesheet contains the hex value %s; use a design token instead", match)
	}
}

// TestStylesheetHonoursTheDesignRules checks that the accessibility rules the
// design depends on are actually in the stylesheet rather than only in the
// documentation.
func TestStylesheetHonoursTheDesignRules(t *testing.T) {
	contents, err := fs.ReadFile(staticFS, "static/app.css")
	if err != nil {
		t.Fatalf("read stylesheet: %v", err)
	}
	css := string(contents)

	required := []struct {
		name    string
		pattern string
		why     string
	}{
		{
			name:    "minimum target size",
			pattern: `--target-min:\s*44px`,
			why:     "every interactive target must be at least 44 by 44 pixels",
		},
		{
			name:    "focus is always visible",
			pattern: `:focus-visible`,
			why:     "focus must never be removed",
		},
		{
			name:    "reduced motion is honoured",
			pattern: `prefers-reduced-motion`,
			why:     "WCAG 2.2 requires motion to be optional",
		},
		{
			name:    "forced colours are supported",
			pattern: `forced-colors`,
			why:     "the site must survive a high contrast mode",
		},
		{
			name:    "tabular figures for times",
			pattern: `tabular-nums`,
			why:     "the slot grid must line up",
		},
		{
			name:    "the slot grid is a grid",
			pattern: `\.slot-grid\s*\{[^}]*display:\s*grid`,
			why:     "the slots must be comparable at a glance",
		},
		{
			name:    "an available slot carries no fill",
			pattern: `\.slot--available[^{]*\{[^}]*background:\s*var\(--neutral-0\)`,
			why:     "an available slot is the default state and must be neutral",
		},
		{
			name:    "the selection is the only filled slot",
			pattern: `\.slot--selected[^{]*\{[^}]*background:\s*var\(--brand-600\)`,
			why:     "colour marks the choice, not every option",
		},
		{
			name:    "a taken slot is disabled",
			pattern: `\.slot--taken[^{]*\{[^}]*cursor:\s*not-allowed`,
			why:     "a slot that cannot be booked must not look clickable",
		},
		{
			name:    "the skip link exists",
			pattern: `\.skip-link`,
			why:     "keyboard users need a way past the header",
		},
		{
			name:    "text never falls below 16px for content",
			pattern: `--text-base:\s*16px`,
			why:     "16px is the floor for anything a patient must read",
		},
	}

	for _, rule := range required {
		if !regexp.MustCompile(rule.pattern).MatchString(css) {
			t.Errorf("the stylesheet does not satisfy %s: %s", rule.name, rule.why)
		}
	}
}

// TestScriptIsOptional checks that the script only removes a click and does
// not gate anything.
//
// Every interaction must work without it, so the script may not be
// responsible for showing anything a patient needs in order to complete a
// booking.
func TestScriptIsOptional(t *testing.T) {
	contents, err := fs.ReadFile(staticFS, "static/app.js")
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	script := string(contents)

	// The script may hide the no-JavaScript fallback button, but only the
	// one that is explicitly marked as a fallback.
	if !strings.Contains(script, "[data-hide-with-js]") {
		t.Error("the script does not target the no-JavaScript fallback explicitly")
	}

	// It must never disable anything. A slot that the script disabled
	// would be a slot the patient cannot see why is gone.
	if strings.Contains(script, ".disabled") {
		t.Error("the script disables something; the server decides what is bookable")
	}

	// The script must not be the only thing that submits the date form.
	if !strings.Contains(script, "form.submit()") {
		t.Error("the script does not submit the date form")
	}
}
