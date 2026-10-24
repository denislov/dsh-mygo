package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	mgo "github.com/egoist/mygo/ui"
)

func TestApplicationThemeCarriesTheSemanticPalette(t *testing.T) {
	for _, dark := range []bool{false, true} {
		theme := applicationTheme(dark)
		want := paletteFor(dark)
		if theme.Dark != dark {
			t.Fatalf("dark=%v theme.Dark=%v", dark, theme.Dark)
		}
		for name, pair := range map[string][2]mgo.Color{
			"background": {theme.Background, want.Background},
			"surface":    {theme.Surface, want.Surface},
			"border":     {theme.Border, want.Border},
			"text":       {theme.Text, want.Text},
			"muted":      {theme.TextMuted, want.TextMuted},
			"accent":     {theme.Accent, want.Accent},
			"danger":     {theme.Danger, want.Danger},
			"warning":    {theme.Warning, want.Warning},
			"success":    {theme.Success, want.Success},
			"scrollbar":  {theme.Scrollbar, want.Scrollbar},
		} {
			if pair[0] != pair[1] {
				t.Errorf("dark=%v %s = %+v, want %+v", dark, name, pair[0], pair[1])
			}
		}
		if theme.ScrollbarWidth != 8 || theme.Radius != radiusMd || theme.Spacing != space4 || theme.FontSize != 14 {
			t.Fatalf("dark=%v metrics = %+v", dark, theme)
		}
		if theme.Selection.A == 0 || theme.Selection.A == 1 || theme.Focus.A == 0 || theme.Focus.A == 1 {
			t.Fatalf("dark=%v selection/focus must be translucent: %+v %+v", dark, theme.Selection, theme.Focus)
		}
	}
}

func TestPalettesSeparateSurfaces(t *testing.T) {
	for _, palette := range []appTokens{lightPalette, darkPalette} {
		if palette.Background == palette.Sidebar || palette.Sidebar == palette.Surface || palette.Surface == palette.SurfaceRaised {
			t.Fatal("background, sidebar, surface and raised surface must be distinct")
		}
		if palette.Text == palette.TextMuted || palette.TextMuted == palette.TextFaint {
			t.Fatal("text hierarchy must be distinct")
		}
		if palette.Accent == palette.UserBubble || palette.Accent == palette.Border {
			t.Fatal("accent must stand apart from surfaces")
		}
	}
}

func TestProductionViewCodeKeepsColorsInTokens(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "tokens.go" {
			continue
		}
		data, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{"mgo.Hex(", "ui.Hex(", "mgo.RGBA(", "ui.RGBA("} {
			if strings.Contains(string(data), needle) {
				t.Errorf("%s hardcodes a color with %s; add a token instead", name, needle)
			}
		}
	}
}
