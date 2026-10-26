package protocol

const (
	MethodConfigurationGet          = "configuration/get"
	MethodConfigurationDefaultModel = "configuration/default-model"
	MethodConfigurationCredential   = "configuration/credential"
)

const (
	CredentialOperationSet   = "set"
	CredentialOperationUnset = "unset"
)

type ConfigurationDefaultModelParams struct {
	Revision uint64 `json:"revision"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
}

// ConfigurationCredentialParams carries a write-only secret for set and omits
// the value entirely for unset.
type ConfigurationCredentialParams struct {
	Reference string  `json:"reference"`
	Operation string  `json:"operation"`
	Value     *string `json:"value,omitempty"`
}

type ConfigurationCredential struct {
	Available  bool   `json:"available"`
	Reference  string `json:"reference"`
	Configured bool   `json:"configured"`
	Writable   bool   `json:"writable"`
	Source     string `json:"source"`
}

type ConfigurationSnapshot struct {
	Revision   uint64                  `json:"revision"`
	Defaults   ModelRoute              `json:"defaults"`
	Credential ConfigurationCredential `json:"credential"`
}
