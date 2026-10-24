package channel

import (
	"encoding/json"
	"testing"
)

func TestDecodeInitialize(t *testing.T) {
	for _, test := range []struct {
		name  string
		wire  string
		valid bool
	}{
		{"compatible", `{"protocolVersion":1,"server":{"name":"dsh-mygo-host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":[]}`, true},
		{"wrong protocol", `{"protocolVersion":2,"server":{"name":"host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":[]}`, false},
		{"wrong release", `{"protocolVersion":1,"server":{"name":"host","version":"0.2.0","dshVersion":"0.2.1-rc.2"},"capabilities":[]}`, false},
		{"sdk response", `{"serverInfo":{"name":"sdk"}}`, false},
		{"null", `null`, false},
		{"invalid capability", `{"protocolVersion":1,"server":{"name":"host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":[1]}`, false},
		{"attachments", `{"protocolVersion":1,"server":{"name":"dsh-mygo-host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":["attachments"],"attachments":{"image":{"mediaTypes":["image/png","image/jpeg"],"maxBase64Bytes":8388608,"maxImageBytes":6291456,"maxImagesPerMessage":8,"maxMessageImageBytes":6291456,"maxImagePixels":64000000,"maxImageDimension":8192}}}`, true},
		{"attachment without policy", `{"protocolVersion":1,"server":{"name":"dsh-mygo-host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":["attachments"]}`, false},
		{"policy without capability", `{"protocolVersion":1,"server":{"name":"dsh-mygo-host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":[],"attachments":{"image":{"mediaTypes":["image/png"],"maxBase64Bytes":8388608,"maxImageBytes":6291456,"maxImagesPerMessage":8,"maxMessageImageBytes":6291456,"maxImagePixels":64000000,"maxImageDimension":8192}}}`, false},
		{"unknown attachment type", `{"protocolVersion":1,"server":{"name":"dsh-mygo-host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":["attachments"],"attachments":{"image":{"mediaTypes":["image/bmp"],"maxBase64Bytes":8388608,"maxImageBytes":6291456,"maxImagesPerMessage":8,"maxMessageImageBytes":6291456,"maxImagePixels":64000000,"maxImageDimension":8192}}}`, false},
		{"implausible attachment limit", `{"protocolVersion":1,"server":{"name":"dsh-mygo-host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":["attachments"],"attachments":{"image":{"mediaTypes":["image/png"],"maxBase64Bytes":8388608,"maxImageBytes":0,"maxImagesPerMessage":8,"maxMessageImageBytes":6291456,"maxImagePixels":64000000,"maxImageDimension":8192}}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeInitialize(json.RawMessage(test.wire))
			if (err == nil) != test.valid {
				t.Fatalf("DecodeInitialize error = %v", err)
			}
		})
	}
}

func TestDecodeInitializeExpectingSelectedRuntime(t *testing.T) {
	const wire = `{"protocolVersion":1,"server":{"name":"dsh-mygo-host","version":"0.1.2","dshVersion":"0.3.0","appMin":"0.1.1","appMaxExclusive":"0.2.0"},"capabilities":[]}`
	result, err := DecodeInitializeExpecting(json.RawMessage(wire), HostIdentity{Version: "0.1.2", DSHVersion: "0.3.0"})
	if err != nil {
		t.Fatalf("DecodeInitializeExpecting: %v", err)
	}
	if result.Server.Version != "0.1.2" || result.Server.AppMin != "0.1.1" {
		t.Fatalf("unexpected handshake result: %+v", result.Server)
	}
	for _, test := range []struct {
		name     string
		wire     string
		expected HostIdentity
	}{
		{"wrong selected host", wire, HostIdentity{Version: "0.1.3", DSHVersion: "0.3.0"}},
		{"wrong selected dsh", wire, HostIdentity{Version: "0.1.2", DSHVersion: "0.3.1"}},
		{"app outside range", `{"protocolVersion":1,"server":{"name":"host","version":"0.1.2","dshVersion":"0.3.0","appMin":"0.2.0","appMaxExclusive":"0.3.0"},"capabilities":[]}`, HostIdentity{Version: "0.1.2", DSHVersion: "0.3.0"}},
		{"incomplete range", `{"protocolVersion":1,"server":{"name":"host","version":"0.1.2","dshVersion":"0.3.0","appMin":"0.1.1"},"capabilities":[]}`, HostIdentity{Version: "0.1.2", DSHVersion: "0.3.0"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeInitializeExpecting(json.RawMessage(test.wire), test.expected); err == nil {
				t.Fatal("DecodeInitializeExpecting accepted an incompatible host")
			}
		})
	}
}
