package view

import (
	mgo "github.com/egoist/mygo/ui"
)

func sectionLabel(c *mgo.Context, label string) mgo.Element {
	return mgo.Text(c, label).FontSize(textMicro).Bold().TextColor(tokensFor(c.Theme()).TextMuted).LetterSpacing(0.3)
}

func statusPill(c *mgo.Context, key, label string, foreground, background mgo.Color) mgo.Element {
	return mgo.Badge(c.Key(key), label).FontSize(textMicro).Padding(space2, space8).
		Radius(radiusPill).Background(background).TextColor(foreground)
}

func surfaceCard(c *mgo.Context, key string) mgo.Element {
	t := tokensFor(c.Theme())
	return mgo.Column(c.Key(key)).Background(t.SurfaceRaised).Border(1, t.Border).Radius(radiusLg).Transition(quietTransition)
}
