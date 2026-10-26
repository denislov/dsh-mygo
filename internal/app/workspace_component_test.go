package app

import "testing"

func TestWorkspaceComponentSeparatesProjectAndPanelRequests(t *testing.T) {
	var workspace workspaceComponent
	generation := workspace.switchProject()
	ctx := workspace.project.readContext(false)
	if !workspace.current(generation) {
		t.Fatal("new project generation was not current")
	}

	previewGeneration, previewRequest := workspace.preview.begin(generation)
	_, newerPreview := workspace.preview.begin(generation)
	diffGeneration, diffRequest := workspace.diff.begin(generation)
	if !workspace.preview.current(previewGeneration, newerPreview) || workspace.preview.current(previewGeneration, previewRequest) {
		t.Fatal("newer preview request did not replace the previous one")
	}
	if !workspace.diff.current(diffGeneration, diffRequest) {
		t.Fatal("diff request token was rejected")
	}

	workspace.switchProject()
	if workspace.current(generation) || workspace.preview.current(previewGeneration, previewRequest) ||
		workspace.diff.current(diffGeneration, diffRequest) {
		t.Fatal("old project resources survived the switch")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("project switch did not cancel reads")
	}
}
