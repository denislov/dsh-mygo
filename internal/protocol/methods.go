package protocol

// Implemented JSON-RPC request methods. Unimplemented upstream methods stay out
// of this list so a constant never implies a handler.
const (
	MethodInitialize = "initialize"

	MethodSessionCreate  = "session/create"
	MethodSessionResume  = "session/resume"
	MethodSessionList    = "session/list"
	MethodSessionHistory = "session/history"
	MethodSessionRelease = "session/release"
	MethodSessionRename  = "session/rename"
	MethodSessionArchive = "session/archive"
	MethodSessionPrompt  = "session/prompt"
	MethodSessionCancel  = "session/cancel"

	MethodModelList   = "model/list"
	MethodModelSelect = "model/select"

	MethodConfigurationGet          = "configuration/get"
	MethodConfigurationDefaultModel = "configuration/default-model"
	MethodConfigurationCredential   = "configuration/credential"

	MethodPermissionGet    = "permission/get"
	MethodPermissionSelect = "permission/select"

	MethodApprovalRespond = "approval/respond"
	MethodQuestionRespond = "question/respond"
	MethodQuestionCancel  = "question/cancel"

	MethodSubagentList    = "subagent/list"
	MethodSubagentHistory = "subagent/history"
)
