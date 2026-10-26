package protocol

import "encoding/json"

// SessionRoute encodes exactly one documented create/resume model route. Use
// HostDefaultRoute or ExplicitRoute; the zero value is not a valid route.
type SessionRoute struct {
	UseDefaultModel bool    `json:"useDefaultModel,omitempty"`
	Provider        *string `json:"provider,omitempty"`
	Model           *string `json:"model,omitempty"`
	Effort          *string `json:"effort,omitempty"`
}

// HostDefaultRoute asks the Host to resolve its persisted default model.
func HostDefaultRoute() SessionRoute {
	return SessionRoute{UseDefaultModel: true}
}

// ExplicitRoute always encodes provider and model, including empty strings, and
// omits an empty effort. Callers that must send an explicit empty effort set
// Effort directly.
func ExplicitRoute(provider, model, effort string) SessionRoute {
	route := SessionRoute{Provider: &provider, Model: &model}
	if effort != "" {
		route.Effort = &effort
	}
	return route
}

type SessionCreateParams struct {
	CWD string `json:"cwd"`
	SessionRoute
}

type SessionResumeParams struct {
	SessionID string `json:"sessionId"`
	SessionRoute
}

// SessionResult is the envelope shared by create, resume and metadata writes.
// The summary stays raw so the app keeps required-field presence checks.
type SessionResult struct {
	Session json.RawMessage `json:"session"`
}

// SessionSummary is the wire shape shared by the catalog and create/resume
// results. Zero values do not prove a field was present, so callers validate
// the raw object before trusting the decoded values.
type SessionSummary struct {
	MetadataIncomplete bool    `json:"metadataIncomplete"`
	MetadataCached     bool    `json:"metadataCached"`
	ID                 string  `json:"id"`
	Title              string  `json:"title"`
	CWD                string  `json:"cwd"`
	Provider           string  `json:"provider"`
	Model              string  `json:"model"`
	Effort             string  `json:"effort"`
	CreatedAt          int64   `json:"createdAt"`
	UpdatedAt          int64   `json:"updatedAt"`
	Status             string  `json:"status"`
	Archived           bool    `json:"archived"`
	Blank              bool    `json:"blank"`
	NextSeq            *uint64 `json:"nextSeq"`
}
