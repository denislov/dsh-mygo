package view

import (
	mgo "github.com/egoist/mygo/ui"
)

// runtimeSetupView is the pre-host onboarding screen. It must not depend on any
// host capability or session state.
func runtimeSetupView(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	mgo.Column(c.Key("runtime-setup")).Fill().Background(tokens.Background).Children(func() {
		runtimeSetupTitleBar(c, model)
		mgo.Scroll(c.Key("runtime-setup-scroll")).Grow(1).MinHeight(0).FillWidth().Children(func() {
			mgo.Column(c.Key("runtime-setup-content")).FillWidth().MaxWidth(readingWidth).
				AlignSelf(mgo.Center).
				Padding(space16, space24, space24, space24).Gap(space16).Children(func() {
				runtimeSetupHeader(c, model)
				if model.Runtime.Busy {
					runtimeSetupProgress(c, model)
				} else {
					runtimeSetupInstalledDSH(c, model)
					runtimeSetupBundle(c, model)
					if len(model.Runtime.Installed) > 0 {
						runtimeSetupInstalled(c, model)
					}
				}
				if model.Runtime.Error != "" {
					mgo.Text(c.Key("runtime-error"), model.T(model.Runtime.Error)).FontSize(textCaption).TextColor(tokens.Danger)
				}
				if model.Runtime.Notice != "" {
					mgo.Text(c.Key("runtime-notice"), model.T(model.Runtime.Notice)).FontSize(textCaption).TextColor(tokens.TextMuted)
				}
				mgo.Text(c.Key("runtime-trust"), model.T("Runtime bundles contain Node and DeepSeek Harness. They run with your user permissions; only install bundles from the official release.")).
					FontSize(textMicro).TextColor(tokens.TextMuted)
			})
		})
	})
}

func runtimeSetupTitleBar(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	bar := c.TitleBar()
	row := mgo.Row(c.Key("runtime-setup-bar")).FillWidth().Height(titleBarHeight(c)).Shrink(0).
		Background(tokens.Sidebar).Padding(0, bar.Right+space8, 0, bar.Left+space8).
		Gap(space8).AlignItems(mgo.Center).DragWindow()
	row.Children(func() {
		mgo.Row(c.Key("runtime-setup-brand")).Gap(space6).AlignItems(mgo.Center).DragWindow().Children(func() {
			mgo.Icon(c, iconSpark).Size(16, 16).TextColor(tokens.Accent)
			mgo.Text(c, "dsh-mygo").FontSize(textLabel).Bold()
		})
		mgo.Spacer(c)
		if model.Quit != nil {
			mgo.Button(c.Key("runtime-setup-quit"), model.T("Quit")).Padding(space4, space12).
				OnClick(model.Quit)
		}
	})
}

func runtimeSetupHeader(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	mgo.Column(c.Key("runtime-setup-header")).FillWidth().AlignItems(mgo.Center).Gap(space6).Children(func() {
		mgo.Icon(c, iconSpark).Size(28, 28).TextColor(tokens.Accent)
		mgo.Text(c, model.T("Set up DeepSeek Harness")).FontSize(textDisplay).Bold().TextAlign(mgo.Center)
		mgo.Text(c, model.T("Choose an existing DSH installation or install a runtime bundle that includes Node and DSH.")).
			FontSize(textBody).TextColor(tokens.TextMuted).TextAlign(mgo.Center)
	})
}

func runtimeSetupProgress(c *mgo.Context, model *Model) {
	card := surfaceCard(c, "runtime-progress-card").FillWidth().Padding(space16).Gap(space8)
	card.Children(func() {
		mgo.Row(c).FillWidth().Gap(space8).AlignItems(mgo.Center).Children(func() {
			mgo.Spinner(c.Key("runtime-progress-spinner")).Size(iconDefault, iconDefault)
			mgo.Text(c, model.T(model.Runtime.Status)).FontSize(textBody).Bold()
		})
		mgo.Progress(c.Key("runtime-progress"), float64(model.Runtime.Progress)).FillWidth()
		if model.Runtime.Cancel != nil {
			mgo.Button(c.Key("runtime-cancel"), model.T("Cancel")).OnClick(model.Runtime.Cancel)
		}
	})
}

func runtimeSetupInstalledDSH(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	card := surfaceCard(c, "runtime-existing-card").FillWidth().Padding(space16).Gap(space8)
	card.Children(func() {
		sectionLabel(c, model.T("Use an installed DSH"))
		mgo.Text(c, model.T("Use a DSH command and Node.js 24+ that are already installed on this computer. The DSH version must match this app.")).
			FontSize(textCaption).TextColor(tokens.TextMuted)
		if model.Runtime.Checking {
			mgo.Row(c.Key("runtime-checking")).Gap(space6).AlignItems(mgo.Center).Children(func() {
				mgo.Spinner(c.Key("runtime-checking-spinner")).Size(iconSmall, iconSmall)
				mgo.Text(c, model.T("Checking installed DSH…")).FontSize(textCaption).TextColor(tokens.TextMuted)
			})
		}
		for _, choice := range model.Runtime.Candidates {
			runtimeCandidateRow(c, model, choice)
		}
		mgo.Row(c.Key("runtime-detect-actions")).Gap(space8).Children(func() {
			mgo.Button(c.Key("runtime-detect"), model.T("Check again")).Disabled(model.Runtime.Checking).
				OnClick(model.Runtime.Detect)
			if model.Runtime.BrowseDSH != nil {
				mgo.Button(c.Key("runtime-browse-dsh"), model.T("Choose dsh command…")).Disabled(model.Runtime.Checking).
					OnClick(model.Runtime.BrowseDSH)
			}
		})
	})
}

func runtimeCandidateRow(c *mgo.Context, model *Model, choice RuntimeChoice) {
	tokens := tokensFor(c.Theme())
	row := mgo.Row(c.Key("runtime-candidate-" + choice.ID)).FillWidth().Gap(space8).AlignItems(mgo.Center).
		Padding(space8).Radius(radiusSm).Background(tokens.Surface)
	row.Children(func() {
		mgo.Column(c).Grow(1).MinWidth(0).Gap(space2).Children(func() {
			mgo.Text(c, model.T(choice.Label)).FontSize(textCaption).Bold().SingleLine()
			detail := model.T("Node") + " " + choice.Node + " · DSH " + choice.DSH
			if choice.Detail != "" {
				detail += " · " + choice.Detail
			}
			mgo.Text(c, detail).FontSize(textMicro).TextColor(tokens.TextMuted).SingleLine()
			if !choice.Compatible && choice.Reason != "" {
				mgo.Text(c, model.T(choice.Reason)).FontSize(textMicro).TextColor(tokens.Warning).Wrap()
			}
		})
		if choice.Compatible {
			button := mgo.PrimaryButton(c.Key("runtime-use-"+choice.ID), model.T("Use this DSH"))
			button.Disabled(model.Runtime.Busy || model.Runtime.Switching)
			if choice.Kind == "legacy" {
				button.OnClick(model.Runtime.UseLegacy)
			} else if model.Runtime.UseExternal != nil {
				button.OnClick(func() { model.Runtime.UseExternal(choice.Path) })
			}
		}
	})
}

func runtimeSetupBundle(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	card := surfaceCard(c, "runtime-bundle-card").FillWidth().Padding(space16).Gap(space8)
	card.Children(func() {
		sectionLabel(c, model.T("Install a runtime bundle"))
		mgo.Text(c, model.T("Download the runtime for this app, or import a .tgz file you received separately. The bundle includes Node and DSH.")).
			FontSize(textCaption).TextColor(tokens.TextMuted)
		mgo.Row(c.Key("runtime-bundle-actions")).Gap(space8).Wrap().Children(func() {
			if model.Runtime.Download != nil {
				mgo.PrimaryButton(c.Key("runtime-download-latest"), model.T("Download latest runtime")).
					Disabled(model.Runtime.Checking).
					OnClick(func() { model.Runtime.Download("") })
			}
			if model.Runtime.InstallFile != nil {
				mgo.Button(c.Key("runtime-install-file"), model.T("Choose .tgz file…")).
					Disabled(model.Runtime.Checking).
					OnClick(model.Runtime.InstallFile)
			}
			if model.Runtime.CheckUpdates != nil {
				mgo.Button(c.Key("runtime-check-updates"), model.T("Check available versions")).
					Disabled(model.Runtime.Checking).
					OnClick(model.Runtime.CheckUpdates)
			}
		})
		for _, choice := range model.Runtime.Available {
			runtimeAvailableRow(c, model, choice)
		}
	})
}

func runtimeAvailableRow(c *mgo.Context, model *Model, choice RuntimeChoice) {
	tokens := tokensFor(c.Theme())
	row := mgo.Row(c.Key("runtime-available-" + choice.ID)).FillWidth().Gap(space8).AlignItems(mgo.Center).
		Padding(space8).Radius(radiusSm).Background(tokens.Surface)
	row.Children(func() {
		mgo.Column(c).Grow(1).MinWidth(0).Gap(space2).Children(func() {
			mgo.Text(c, "DSH "+choice.DSH+" · Node "+choice.Node).FontSize(textCaption).Bold().SingleLine()
			detail := "Host " + choice.HostVersion
			if choice.Detail != "" {
				detail += " · " + choice.Detail
			}
			mgo.Text(c, detail).FontSize(textMicro).TextColor(tokens.TextMuted).SingleLine()
		})
		if model.Runtime.Download != nil {
			mgo.Button(c.Key("runtime-download-"+choice.ID), model.T("Download")).
				OnClick(func() { model.Runtime.Download(choice.ID) })
		}
	})
}

func runtimeSetupInstalled(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	card := surfaceCard(c, "runtime-installed-card").FillWidth().Padding(space16).Gap(space8)
	card.Children(func() {
		sectionLabel(c, model.T("Installed runtimes"))
		for _, choice := range model.Runtime.Installed {
			row := mgo.Row(c.Key("runtime-installed-" + choice.ID)).FillWidth().Gap(space8).AlignItems(mgo.Center).
				Padding(space8).Radius(radiusSm).Background(tokens.Surface)
			row.Children(func() {
				mgo.Column(c).Grow(1).MinWidth(0).Gap(space2).Children(func() {
					mgo.Text(c, model.T(choice.Label)).FontSize(textCaption).Bold().SingleLine()
					mgo.Text(c, "DSH "+choice.DSH+" · Node "+choice.Node).FontSize(textMicro).TextColor(tokens.TextMuted).SingleLine()
				})
				if choice.ID == model.Runtime.Current.ID {
					statusPill(c, "runtime-current-"+choice.ID, model.T("Current"), tokens.Success, tokens.SuccessSoft)
				} else if model.Runtime.UseInstalled != nil {
					mgo.Button(c.Key("runtime-installed-use-"+choice.ID), model.T("Use")).
						OnClick(func() { model.Runtime.UseInstalled(choice.ID) })
				}
			})
		}
	})
}
