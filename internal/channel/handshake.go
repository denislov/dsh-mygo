package channel

import (
	"encoding/json"
	"fmt"

	"dsh-mygo/internal/version"
)

const ProtocolVersion = version.Protocol
const ReleaseVersion = version.App
const DSHVersion = version.DSH

type InitializeResult struct {
	ProtocolVersion int `json:"protocolVersion"`
	Server          struct {
		Name       string `json:"name"`
		Version    string `json:"version"`
		DSHVersion string `json:"dshVersion"`
	} `json:"server"`
	Capabilities []string `json:"capabilities"`
}

func DecodeInitialize(data json.RawMessage) (InitializeResult, error) {
	var result InitializeResult
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("invalid host handshake")
	}
	if result.ProtocolVersion != ProtocolVersion {
		return result, fmt.Errorf("incompatible host protocol version: expected %d", ProtocolVersion)
	}
	if result.Server.Version != ReleaseVersion || result.Server.DSHVersion != DSHVersion {
		return result, fmt.Errorf("incompatible host release: expected UI/host %s and DSH %s", ReleaseVersion, DSHVersion)
	}
	if result.Server.Name == "" || len(result.Server.Name) > 256 || result.Capabilities == nil || len(result.Capabilities) > 64 {
		return result, fmt.Errorf("invalid host identity or capabilities")
	}
	for _, capability := range result.Capabilities {
		if capability == "" || len(capability) > 256 {
			return result, fmt.Errorf("invalid host capability")
		}
	}
	return result, nil
}
