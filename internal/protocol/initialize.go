package protocol

import "dsh-mygo/internal/attachment"

type InitializeParams struct {
	ProtocolVersion int        `json:"protocolVersion"`
	Client          ClientInfo `json:"client"`
	Capabilities    []string   `json:"capabilities"`
}

type ClientInfo struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Platform string `json:"platform"`
	Locale   string `json:"locale"`
}

type ServerInfo struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	DSHVersion      string `json:"dshVersion"`
	AppMin          string `json:"appMin"`
	AppMaxExclusive string `json:"appMaxExclusive"`
}

// InitializeResult describes the wire shape. Channel decoding still owns
// identity, compatibility, field limits and attachment policy validation.
type InitializeResult struct {
	ProtocolVersion int               `json:"protocolVersion"`
	Server          ServerInfo        `json:"server"`
	Capabilities    []string          `json:"capabilities"`
	Attachments     *AttachmentPolicy `json:"attachments"`
}

type AttachmentPolicy struct {
	Image attachment.Limits `json:"image"`
}

// SDKInitializeParams belongs only to explicit legacy SDK regression mode.
// Empty effort is omitted; cwd/provider/model remain explicit even when empty.
type SDKInitializeParams struct {
	CWD             string `json:"cwd"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

type SDKInitializeResult struct {
	ServerInfo SDKServerInfo `json:"serverInfo"`
}

type SDKServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
