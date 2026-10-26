package protocol

const (
	MethodModelList   = "model/list"
	MethodModelSelect = "model/select"
)

type ModelListParams struct {
	SessionID string `json:"sessionId"`
	Provider  string `json:"provider,omitempty"`
}

// ModelSelectParams always carries effort so an empty value clears the
// session's explicit override.
type ModelSelectParams struct {
	SessionID string `json:"sessionId"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Effort    string `json:"effort"`
}

type ModelRoute struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
}

type ModelCatalogName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ModelCatalogModel struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Efforts         []ModelCatalogName `json:"efforts"`
	Available       bool               `json:"available"`
	Reason          string             `json:"reason"`
	InputModalities []string           `json:"inputModalities,omitempty"`
}

type ModelCatalog struct {
	Providers []ModelCatalogName  `json:"providers"`
	Provider  string              `json:"provider"`
	Models    []ModelCatalogModel `json:"models"`
	Selection ModelRoute          `json:"selection"`
	Available bool                `json:"available"`
	Reason    string              `json:"reason"`
}
