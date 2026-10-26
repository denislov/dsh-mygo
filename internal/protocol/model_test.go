package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestModelPayloadsMatchSharedFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "protocol", "model.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ListDefault  json.RawMessage `json:"listDefault"`
		List         json.RawMessage `json:"list"`
		Select       json.RawMessage `json:"select"`
		SelectEffort json.RawMessage `json:"selectEffort"`
		Catalog      json.RawMessage `json:"catalog"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		value any
		want  json.RawMessage
	}{
		{"list default", ModelListParams{SessionID: "s1"}, fixture.ListDefault},
		{"list provider", ModelListParams{SessionID: "s1", Provider: "test"}, fixture.List},
		{"select empty effort", ModelSelectParams{SessionID: "s1", Provider: "test", Model: "echo"}, fixture.Select},
		{"select effort", ModelSelectParams{SessionID: "s1", Provider: "test", Model: "echo", Effort: "high"}, fixture.SelectEffort},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertJSONEqual(t, test.value, test.want)
		})
	}
	var catalog ModelCatalog
	if err := json.Unmarshal(fixture.Catalog, &catalog); err != nil {
		t.Fatal(err)
	}
	want := ModelCatalog{
		Providers: []ModelCatalogName{{ID: "test", Name: "Test"}},
		Provider:  "test",
		Models: []ModelCatalogModel{{
			ID: "echo", Name: "Echo", Efforts: []ModelCatalogName{{ID: "low", Name: "Low"}},
			Available: true, Reason: "", InputModalities: []string{"text", "image"},
		}},
		Selection: ModelRoute{Provider: "test", Model: "echo", Effort: ""},
		Available: true,
		Reason:    "",
	}
	if !reflect.DeepEqual(catalog, want) {
		t.Fatalf("catalog = %+v, want %+v", catalog, want)
	}
	if MethodModelList != "model/list" || MethodModelSelect != "model/select" {
		t.Fatal("model method names changed")
	}
}
