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

func messageCopyButton(c *mgo.Context, model *Model, item projection.Item, fromUser bool) {
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
	mgo.Button(c.Key("copy-message"), label).OnClick(func() {
		services.WriteClipboard(item.Text)
		model.CopiedMessageID = item.ID
		model.CopiedMessageText = item.Text
	})
}

func interruptedCodeBlock(c *mgo.Context, block *markdown.CodeBlockView) {
	mgo.Column(c).Gap(6).FillWidth().Children(func() {
		mgo.Row(c).Gap(8).Children(func() {
			mgo.Text(c, block.Language).FontSize(11)
			mgo.Text(c, "Interrupted code (copy disabled)").FontSize(11).TextColor(c.Theme().Warning)
			mgo.Button(c, "Copy code").Disabled(true)
		})
		block.Body(c)
	})
}
