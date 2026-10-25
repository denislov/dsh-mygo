package view

import mgo "github.com/egoist/mygo/ui"

const workspaceGutter float32 = 13

// The pinned MyGo Split always paints a full divider. Use the same native
// input/focus primitives with a full gutter hit target and a short hover grip.
func workspaceSplit(c *mgo.Context, model *Model, percent *float32, vertical bool, label string, first, second func()) {
	var root mgo.Element
	if vertical {
		root = mgo.Column(c)
	} else {
		root = mgo.Row(c)
	}
	root.Fill().MinWidth(0).MinHeight(0).AlignItems(mgo.Stretch)
	bounds := root.Bounds()
	length := bounds.W
	if vertical {
		length = bounds.H
	}
	available := max(0, length-workspaceGutter)
	minimum := min(float32(120), available/2)
	shown := max(min(available*(*percent)/100, available-minimum), minimum)
	share := max(float32(0.01), min(*percent/100, 0.99))
	if available > 0 {
		share = shown / available
	}
	adjust := func(delta float32) {
		if available > 0 {
			*percent = max(min(shown+delta, available-minimum), minimum) * 100 / available
		}
	}
	root.Children(func() {
		mgo.Column(c.Key("first")).Basis(0).Grow(share).MinWidth(0).MinHeight(0).Clip().Children(first)
		grip := mgo.Box(c.Key("resize")).Focusable().FocusRing(false).Role(mgo.RoleSplitter).
			Label(label).Tooltip(model.T("Resize")).Shrink(0).Range(float64(minimum), float64(max(minimum, available-minimum)), float64(shown))
		back, forth := mgo.KeyLeft, mgo.KeyRight
		if vertical {
			grip.Height(workspaceGutter).FillWidth().Cursor(mgo.CursorResizeNS)
			back, forth = mgo.KeyUp, mgo.KeyDown
		} else {
			grip.Width(workspaceGutter).FillHeight().Cursor(mgo.CursorResizeEW)
		}
		dx, dy, held := grip.Dragged()
		if held {
			grip.Focus()
			if vertical {
				adjust(dy)
			} else {
				adjust(dx)
			}
		}
		grip.OnShortcut(0, back, func() { adjust(-16) }).OnShortcut(0, forth, func() { adjust(16) })
		visible := grip.Hovered() || held || grip.FocusVisible()
		color := tokensFor(c.Theme()).TextFaint
		grip.Draw(func(p *mgo.Painter, r mgo.Rect) {
			if !visible {
				return
			}
			if vertical {
				width := min(float32(70), r.W)
				p.Fill(mgo.Rect{X: r.X + (r.W-width)/2, Y: r.Y + (r.H-3)/2, W: width, H: 3}, color, 2)
			} else {
				height := min(float32(70), r.H)
				p.Fill(mgo.Rect{X: r.X + (r.W-3)/2, Y: r.Y + (r.H-height)/2, W: 3, H: height}, color, 2)
			}
		})
		mgo.Column(c.Key("second")).Basis(0).Grow(1 - share).MinWidth(0).MinHeight(0).Clip().Children(second)
	})
}
