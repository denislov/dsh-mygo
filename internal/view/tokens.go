package view

import (
	mgo "github.com/egoist/mygo/ui"
)

// Layout metrics shared by the whole shell.
const (
	WindowTitleBarHeight float32 = 44
	sidebarWidth         float32 = 260
	detailsWidth         float32 = 300
	readingWidth         float32 = 760
	sidebarBreakpoint    float32 = 820
	detailsBreakpoint    float32 = 1180

	space2  float32 = 2
	space4  float32 = 4
	space6  float32 = 6
	space8  float32 = 8
	space12 float32 = 12
	space16 float32 = 16
	space20 float32 = 20
	space24 float32 = 24
	space32 float32 = 32

	radiusXs       float32 = 6
	radiusSm       float32 = 8
	radiusMd       float32 = 10
	radiusLg       float32 = 12
	radiusXl       float32 = 16
	radiusComposer float32 = 22
	radiusPill     float32 = 999

	textMicro   float32 = 11
	textCaption float32 = 12
	textLabel   float32 = 13
	textBody    float32 = 15
	textTitle   float32 = 18
	textDisplay float32 = 28
)

// appTokens is the semantic palette the shell paints with. MyGo's Theme only
// carries the colors its widgets need; surfaces such as the sidebar, raised
// cards and the user bubble live here.
type appTokens struct {
	Background     mgo.Color
	Sidebar        mgo.Color
	Surface        mgo.Color
	SurfaceRaised  mgo.Color
	SurfaceHover   mgo.Color
	SurfacePressed mgo.Color

	Border       mgo.Color
	BorderStrong mgo.Color

	Text      mgo.Color
	TextMuted mgo.Color
	TextFaint mgo.Color

	Accent        mgo.Color
	AccentHover   mgo.Color
	AccentPressed mgo.Color
	AccentSoft    mgo.Color
	AccentText    mgo.Color

	UserBubble     mgo.Color
	UserBubbleText mgo.Color
	CodeBackground mgo.Color

	Danger      mgo.Color
	DangerSoft  mgo.Color
	Warning     mgo.Color
	WarnSoft    mgo.Color
	Success     mgo.Color
	SuccessSoft mgo.Color

	Selection mgo.Color
	Focus     mgo.Color
	Scrollbar mgo.Color
}

var (
	lightPalette = paletteFor(false)
	darkPalette  = paletteFor(true)
)

func paletteFor(dark bool) appTokens {
	if dark {
		return appTokens{
			Background:     mgo.Hex("#232320"),
			Sidebar:        mgo.Hex("#1D1D1A"),
			Surface:        mgo.Hex("#2C2C28"),
			SurfaceRaised:  mgo.Hex("#31312D"),
			SurfaceHover:   mgo.Hex("#373732"),
			SurfacePressed: mgo.Hex("#41403A"),
			Border:         mgo.Hex("#403F37"),
			BorderStrong:   mgo.Hex("#4D4B42"),
			Text:           mgo.Hex("#F3F0E8"),
			TextMuted:      mgo.Hex("#ABA69A"),
			TextFaint:      mgo.Hex("#7E7A70"),
			Accent:         mgo.Hex("#D97757"),
			AccentHover:    mgo.Hex("#E18A6C"),
			AccentPressed:  mgo.Hex("#C96546"),
			AccentSoft:     mgo.Hex("#3B2C25"),
			AccentText:     mgo.Hex("#FFFFFF"),
			UserBubble:     mgo.Hex("#34332E"),
			UserBubbleText: mgo.Hex("#F3F0E8"),
			CodeBackground: mgo.Hex("#1B1B19"),
			Danger:         mgo.Hex("#E27C68"),
			DangerSoft:     mgo.Hex("#3A2522"),
			Warning:        mgo.Hex("#D7A058"),
			WarnSoft:       mgo.Hex("#362B1D"),
			Success:        mgo.Hex("#83AA8B"),
			SuccessSoft:    mgo.Hex("#22302A"),
			Selection:      mgo.RGBA(217, 119, 87, 0.28),
			Focus:          mgo.RGBA(217, 119, 87, 0.60),
			Scrollbar:      mgo.RGBA(243, 240, 232, 0.28),
		}
	}
	return appTokens{
		Background:     mgo.Hex("#FBFAF6"),
		Sidebar:        mgo.Hex("#F4F1EA"),
		Surface:        mgo.Hex("#F0EDE5"),
		SurfaceRaised:  mgo.Hex("#FFFDFA"),
		SurfaceHover:   mgo.Hex("#EAE6DD"),
		SurfacePressed: mgo.Hex("#DFDAD0"),
		Border:         mgo.Hex("#E2DDD2"),
		BorderStrong:   mgo.Hex("#D3CDC0"),
		Text:           mgo.Hex("#2E2C27"),
		TextMuted:      mgo.Hex("#716D64"),
		TextFaint:      mgo.Hex("#9A9488"),
		Accent:         mgo.Hex("#D97757"),
		AccentHover:    mgo.Hex("#C96546"),
		AccentPressed:  mgo.Hex("#B45437"),
		AccentSoft:     mgo.Hex("#F5E3DA"),
		AccentText:     mgo.Hex("#FFFFFF"),
		UserBubble:     mgo.Hex("#ECE7DD"),
		UserBubbleText: mgo.Hex("#2E2C27"),
		CodeBackground: mgo.Hex("#F2EEE5"),
		Danger:         mgo.Hex("#C04735"),
		DangerSoft:     mgo.Hex("#F7E4DF"),
		Warning:        mgo.Hex("#A56A22"),
		WarnSoft:       mgo.Hex("#F6EBD8"),
		Success:        mgo.Hex("#4C7A55"),
		SuccessSoft:    mgo.Hex("#E3EEE4"),
		Selection:      mgo.RGBA(217, 119, 87, 0.22),
		Focus:          mgo.RGBA(217, 119, 87, 0.55),
		Scrollbar:      mgo.RGBA(46, 44, 39, 0.26),
	}
}

func tokensFor(theme *mgo.Theme) appTokens {
	if theme != nil && theme.Dark {
		return darkPalette
	}
	return lightPalette
}

type shadowSpec struct {
	X, Y, Blur, Spread float32
	Color              mgo.Color
}

func (s shadowSpec) apply(e mgo.Element) mgo.Element {
	if s.Blur == 0 && s.X == 0 && s.Y == 0 && s.Spread == 0 {
		return e
	}
	return e.Shadow(s.X, s.Y, s.Blur, s.Spread, s.Color)
}

var (
	shadowCard     = shadowSpec{X: 0, Y: 1, Blur: 2, Spread: 0, Color: mgo.RGBA(0, 0, 0, 0.06)}
	shadowFloating = shadowSpec{X: 0, Y: 6, Blur: 20, Spread: 0, Color: mgo.RGBA(0, 0, 0, 0.10)}
	shadowModal    = shadowSpec{X: 0, Y: 16, Blur: 48, Spread: 0, Color: mgo.RGBA(0, 0, 0, 0.18)}
)
