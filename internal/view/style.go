package view

import (
	"runtime"
	"time"

	mgo "github.com/egoist/mygo/ui"
)

var quietTransition = mgo.ElementTransition{Colors: true, Duration: 140 * time.Millisecond}

// Keep native widget metrics and semantic colors shared across the whole app.
// Every field is set explicitly so MyGo defaults never leak into the palette.
func applicationTheme(dark bool) *mgo.Theme {
	t := mgo.LightTheme()
	if dark {
		t = mgo.DarkTheme()
	}
	p := paletteFor(dark)
	t.Background = p.Background
	t.Surface = p.Surface
	t.SurfaceHover = p.SurfaceHover
	t.SurfacePressed = p.SurfacePressed
	t.Border = p.Border
	t.Text = p.Text
	t.TextMuted = p.TextMuted
	t.Accent = p.Accent
	t.AccentHover = p.AccentHover
	t.AccentPressed = p.AccentPressed
	t.AccentText = p.AccentText
	t.Danger = p.Danger
	t.Warning = p.Warning
	t.Success = p.Success
	t.Selection = p.Selection
	t.Focus = p.Focus
	t.Inverse = p.Text
	t.InverseText = p.Background
	t.Scrollbar = p.Scrollbar
	t.ScrollbarWidth = 8
	t.Radius = radiusMd
	t.Spacing = space4
	t.FontSize = 14
	if runtime.GOOS == "windows" {
		t.Font = "Segoe UI"
	}
	return t
}

func chineseTheme(base *mgo.Theme) *mgo.Theme {
	t := *base
	if runtime.GOOS == "windows" {
		t.Font = "Microsoft YaHei UI"
	}
	return &t
}

// The app has a small set of line icons, rendered by MyGo's SVG component.
// Paths are static assets; parsing and GPU geometry are reused across frames.
func lineIcon(shapes string) *mgo.SVG {
	return mgo.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

var (
	iconPlus     = lineIcon(`<path d="M12 5v14M5 12h14"/>`)
	iconAgents   = lineIcon(`<circle cx="6" cy="5" r="2"/><circle cx="18" cy="12" r="2"/><circle cx="6" cy="19" r="2"/><path d="M6 7v10m0-8c0 3 10 0 10 3"/>`)
	iconChevron  = lineIcon(`<path d="m6 9 6 6 6-6"/>`)
	iconSessions = lineIcon(`<rect x="3" y="4" width="18" height="16" rx="3"/><path d="M9 4v16M5.5 8h1M5.5 12h1"/>`)
	iconMore     = lineIcon(`<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>`)
	iconSettings = lineIcon(`<path d="M4 7h16M4 17h16"/><circle cx="9" cy="7" r="3"/><circle cx="15" cy="17" r="3"/>`)
	iconInfo     = lineIcon(`<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7h.01"/>`)
	iconAttach   = lineIcon(`<path d="m8 13 7-7a3 3 0 0 1 4 4l-9 9a5 5 0 0 1-7-7l9-9M6 15l9-9"/>`)
	iconSend     = lineIcon(`<path d="M12 19V5m-6 6 6-6 6 6"/>`)
	iconStop     = lineIcon(`<rect x="6" y="6" width="12" height="12" rx="2"/>`)
	iconCopy     = lineIcon(`<rect x="8" y="8" width="12" height="13" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/>`)
	iconCheck    = lineIcon(`<path d="m5 12 4 4L19 6"/>`)
	iconClose    = lineIcon(`<path d="m6 6 12 12M6 18 18 6"/>`)
	iconSpark    = lineIcon(`<path d="m12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5Z"/>`)

	iconMenu         = lineIcon(`<path d="M4 6h16M4 12h16M4 18h16"/>`)
	iconPanelLeft    = lineIcon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/>`)
	iconPanelRight   = lineIcon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M15 4v16"/>`)
	iconKey          = lineIcon(`<circle cx="7.5" cy="15.5" r="3.5"/><path d="m10 13 8-8m-3 3 2 2m-4 0 2 2"/>`)
	iconFolder       = lineIcon(`<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>`)
	iconRefresh      = lineIcon(`<path d="M21 12a9 9 0 1 1-2.6-6.4"/><path d="M21 3v6h-6"/>`)
	iconFilter       = lineIcon(`<path d="M4 5h16l-6 7v6l-4 2v-8Z"/>`)
	iconChevronRight = lineIcon(`<path d="m9 6 6 6-6 6"/>`)
	iconTerminal     = lineIcon(`<path d="m5 7 5 5-5 5M13 17h6"/>`)
	iconFile         = lineIcon(`<path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9Z"/><path d="M14 3v6h6M8 13h8M8 17h5"/>`)
	iconEdit         = lineIcon(`<path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z"/>`)
	iconSearch       = lineIcon(`<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>`)
	iconArchive      = lineIcon(`<rect x="3" y="4" width="18" height="4" rx="1"/><path d="M5 8v11a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8M10 12h4"/>`)
	iconClock        = lineIcon(`<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>`)
	iconImage        = lineIcon(`<rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="8.5" cy="9.5" r="1.5"/><path d="m21 15-5-5L5 20"/>`)
)

func iconButton(c *mgo.Context, label string, icon *mgo.SVG) mgo.Element {
	return mgo.Button(c, "").Label(label).Tooltip(label).Padding(7).Radius(8).
		Transition(quietTransition).Children(func() { mgo.Icon(c, icon).Size(17, 17) })
}
