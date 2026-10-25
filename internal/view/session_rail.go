package view

import mgo "github.com/egoist/mygo/ui"

const sessionRailWidth float32 = 340

// Account for the sidebar and the user's actual tool split. Rail is a sibling
// of chat, never a tile in the tool grid, and floats when chat would be cramped.
func sessionRailDocked(c *mgo.Context, model *Model) bool {
	width, _ := c.Size()
	if sidebarVisible(c, model) {
		width -= sidebarWidth
	}
	if len(model.workspacePanels()) > 0 {
		if width < 1000 {
			return false
		}
		width = (width - workspaceGutter) * model.Workspace.ChatPercent / 100
	}
	return width >= sessionRailWidth+480
}

func conversationWithRail(c *mgo.Context, model *Model, chat func()) {
	mgo.Row(c.Key("chat-and-rail")).Fill().MinWidth(0).MinHeight(0).Children(func() {
		chat()
		// A point inside chat places the floating rail independently from the
		// right-aligned toolbar, whose button may sit above a wide tool dock.
		mgo.Box(c.Key("rail-origin")).Absolute().Right(space8).Top(space8).Size(1, 1).Bind(&model.Workspace.RailOrigin)
		if model.Workspace.Open[PanelRail] && sessionRailDocked(c, model) {
			mgo.Scroll(c.Key("docked-rail")).Label(model.T("Docked Session rail")).Width(sessionRailWidth).Shrink(0).FillHeight().Padding(space8).
				Children(func() { detailsBody(c, model) })
		}
	})
}

func floatingSessionRail(c *mgo.Context, model *Model, anchor mgo.Element) {
	if !anchor.Valid() || !model.Workspace.Open[PanelRail] || sessionRailDocked(c, model) || overlayActive(model) {
		return
	}
	width, height := c.Size()
	tokens := tokensFor(c.Theme())
	// PopoverBase reports dismissal during this build; only the map owns open
	// state. Resizing or temporarily hiding beneath a modal must not close it.
	open := true
	popup := mgo.PopoverBase(c.Key("floating-session-rail"), anchor, &open, func(panel mgo.Element) {
		panel.Label(model.T("Floating Session rail")).AttachTo(anchor, mgo.AnchorBottomEnd, mgo.AnchorTopEnd).
			Margin(space8, 0, 0, 0).Width(min(sessionRailWidth, width-space32)).Radius(radiusLg).
			Background(tokens.Background).Border(1, tokens.Border)
		if origin := c.Resolve(model.Workspace.RailOrigin); origin.Valid() {
			panel.AttachTo(origin, mgo.AnchorTopEnd, mgo.AnchorTopEnd).Margin(0)
		}
		shadowCard.apply(panel)
		mgo.Scroll(c.Key("floating-rail-scroll")).FillWidth().MaxHeight(max(80, height-titleBarHeight(c)-space24)).Padding(space8).
			Children(func() { detailsBody(c, model) })
	})
	if popup.Dismissed() {
		// Toolbar/sidebar navigation may open a modal or change another panel;
		// keep the rail preference for when that operation finishes. Clicking
		// the workspace itself or pressing Escape dismisses the floating rail.
		if popup.PressedOutside() {
			x, y, _ := anchor.PointerPosition()
			x, y = anchor.LocalToWindow(x, y)
			if y < titleBarHeight(c) || sidebarVisible(c, model) && x < sidebarWidth {
				return
			}
		}
		model.Workspace.Open[PanelRail] = false
	}
}
