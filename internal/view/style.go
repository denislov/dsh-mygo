package view

import (
	"runtime"
	"time"

	mgo "github.com/egoist/mygo/ui"
)

const readingWidth float32 = 760

var quietTransition = mgo.ElementTransition{Colors: true, Duration: 140 * time.Millisecond}

// Keep native widget metrics and semantic colors shared across the whole app.
func applicationTheme(dark bool) *mgo.Theme {
	t := mgo.LightTheme()
	if dark {
		t = mgo.DarkTheme()
		t.Background, t.Surface = mgo.Hex("#1C1D1F"), mgo.Hex("#25272A")
		t.SurfaceHover, t.SurfacePressed = mgo.Hex("#303438"), mgo.Hex("#3A4043")
		t.Border, t.Text, t.TextMuted = mgo.Hex("#3B3E41"), mgo.Hex("#ECEDEA"), mgo.Hex("#A6AAA5")
		t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = mgo.Hex("#A5C4B6"), mgo.Hex("#B9D3C7"), mgo.Hex("#8DAF9F"), mgo.Hex("#18271F")
	} else {
		t.Background, t.Surface = mgo.Hex("#FAF9F6"), mgo.Hex("#F0EFEB")
		t.SurfaceHover, t.SurfacePressed = mgo.Hex("#E7E8E1"), mgo.Hex("#DCE0D7")
		t.Border, t.Text, t.TextMuted = mgo.Hex("#DCDDD5"), mgo.Hex("#292D28"), mgo.Hex("#6C7369")
		t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = mgo.Hex("#34675C"), mgo.Hex("#29594F"), mgo.Hex("#204B42"), mgo.Hex("#FFFFFF")
	}
	t.Focus, t.Selection = t.Accent.Mix(mgo.Transparent, 0.55), t.Accent.Mix(mgo.Transparent, 0.78)
	t.Radius, t.FontSize = 8, 14
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
)

func iconButton(c *mgo.Context, label string, icon *mgo.SVG) mgo.Element {
	return mgo.Button(c, "").Label(label).Tooltip(label).Padding(7).Radius(8).
		Transition(quietTransition).Children(func() { mgo.Icon(c, icon).Size(17, 17) })
}
