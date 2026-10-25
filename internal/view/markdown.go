package view

import (
	"net/url"
	"strings"

	"dsh-mygo/internal/projection"
	mgo "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

func safeMarkdownLink(address string) bool {
	if len(address) > 2048 {
		return false
	}
	for _, character := range address {
		if character <= ' ' || character == '\x7f' {
			return false
		}
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	return scheme == "http" || scheme == "https"
}

func messageCopyButton(c *mgo.Context, model *Model, item projection.Item, fromUser bool) mgo.Element {
	label := "Copy Markdown"
	if fromUser {
		label = "Copy message"
	}
	if item.State == "streaming" {
		label = "Copy current text"
	}
	if item.State == "interrupted" {
		label = "Copy partial Markdown"
	}
	if item.Truncated {
		label = "Copy visible text"
	}
	if model.CopiedMessageID == item.ID && model.CopiedMessageText == item.Text {
		label = "Message copied"
	}
	services := c.Services()
	icon := iconCopy
	if model.CopiedMessageID == item.ID && model.CopiedMessageText == item.Text {
		icon = iconCheck
	}
	return iconButton(c.Key("copy-message-"+item.ID), model.T(label), icon).Padding(space4).OnClick(func() {
		services.WriteClipboard(item.Text)
		model.CopiedMessageID = item.ID
		model.CopiedMessageText = item.Text
	})
}

// Keep the native highlighter and copy lifecycle, while giving localized copy
// labels enough room beside the language instead of wrapping in a narrow header.
func nativeCodeBlock(c *mgo.Context, block *markdown.CodeBlockView) {
	tokens := tokensFor(c.Theme())
	mgo.Column(c).FillWidth().Direction(mgo.LeftToRight).Radius(radiusMd).Background(tokens.CodeBackground).
		Border(1, tokens.Border).Role(mgo.RoleGroup).Label("Code, " + block.Language).Clip().Children(func() {
		mgo.Row(c).FillWidth().Padding(space6, space8, 0, space14).AlignItems(mgo.Center).Children(func() {
			mgo.Text(c, block.Language).FontSize(textCaption).TextColor(tokens.TextMuted)
			mgo.Spacer(c)
			block.CopyButton(c).MinWidth(76).Shrink(0)
		})
		block.Body(c)
	})
}

func (model *Model) interruptedCodeBlock(c *mgo.Context, block *markdown.CodeBlockView) {
	mgo.Column(c).Gap(6).FillWidth().Children(func() {
		mgo.Row(c).Gap(8).Children(func() {
			mgo.Text(c, block.Language).FontSize(11)
			mgo.Text(c, model.T("Interrupted code (copy disabled)")).FontSize(11).TextColor(c.Theme().Warning)
			mgo.Button(c, model.T("Copy code")).Disabled(true)
		})
		block.Body(c)
	})
}
