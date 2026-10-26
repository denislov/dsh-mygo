package protocol

const (
	MethodPermissionGet    = "permission/get"
	MethodPermissionSelect = "permission/select"
)

type PermissionGetParams struct {
	SessionID string `json:"sessionId"`
}

// PermissionSelectParams always carries confirmDangerous so an unconfirmed
// dangerous bundle cannot read as confirmed by omission.
type PermissionSelectParams struct {
	SessionID        string `json:"sessionId"`
	Preset           string `json:"preset"`
	ExpectedNextSeq  uint64 `json:"expectedNextSeq"`
	ConfirmDangerous bool   `json:"confirmDangerous"`
}

type PermissionOption struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	Sandbox              string `json:"sandbox"`
	Approval             string `json:"approval"`
	RequiresConfirmation bool   `json:"requiresConfirmation"`
}

type PermissionSnapshot struct {
	SessionID string             `json:"sessionId"`
	Current   string             `json:"current"`
	Sandbox   string             `json:"sandbox"`
	Approval  string             `json:"approval"`
	NextSeq   uint64             `json:"nextSeq"`
	Options   []PermissionOption `json:"options"`
}
