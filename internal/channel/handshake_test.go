package channel

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInitializeResponseFieldCompatibility(t *testing.T) {
	const base = `{"protocolVersion":1,"server":{"name":"host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":[]}`
	for _, test := range []struct {
		name  string
		patch string
		drop  string
		valid bool
	}{
		{"missing protocol", `{}`, "protocolVersion", false},
		{"null protocol", `{"protocolVersion":null}`, "", false},
		{"string protocol", `{"protocolVersion":"1"}`, "", false},
		{"fractional protocol", `{"protocolVersion":1.5}`, "", false},
		{"missing server", `{}`, "server", false},
		{"null server", `{"server":null}`, "", false},
		{"null name", `{"server":{"name":null,"version":"0.1.1","dshVersion":"0.2.1-rc.2"}}`, "", false},
		{"missing capabilities", `{}`, "capabilities", false},
		{"null capabilities", `{"capabilities":null}`, "", false},
		{"scalar capabilities", `{"capabilities":"approval"}`, "", false},
		{"null capability", `{"capabilities":[null]}`, "", false},
		{"unknown capability", `{"capabilities":["future.test"]}`, "", true},
		{"duplicate capability", `{"capabilities":["approval","approval"]}`, "", true},
		{"unknown response field", `{"future":{"version":2}}`, "", true},
		{"null absent policy", `{"attachments":null}`, "", true},
		{"legacy null range", `{"server":{"name":"host","version":"0.1.1","dshVersion":"0.2.1-rc.2","appMin":null,"appMaxExclusive":null}}`, "", true},
		{"legacy empty range", `{"server":{"name":"host","version":"0.1.1","dshVersion":"0.2.1-rc.2","appMin":"","appMaxExclusive":""}}`, "", true},
		{"half null range", `{"server":{"name":"host","version":"0.1.1","dshVersion":"0.2.1-rc.2","appMin":null,"appMaxExclusive":"0.2.0"}}`, "", false},
		{"oversized name", `{"server":{"name":"` + strings.Repeat("x", 257) + `","version":"0.1.1","dshVersion":"0.2.1-rc.2"}}`, "", false},
		{"oversized capability", `{"capabilities":["` + strings.Repeat("x", 257) + `"]}`, "", false},
		{"too many capabilities", `{"capabilities":[` + strings.TrimSuffix(strings.Repeat(`"future",`, 65), ",") + `]}`, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var fields, patch map[string]json.RawMessage
			if err := json.Unmarshal([]byte(base), &fields); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(test.patch), &patch); err != nil {
				t.Fatal(err)
			}
			for key, value := range patch {
				fields[key] = value
			}
			delete(fields, test.drop)
			data, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeInitialize(data); (err == nil) != test.valid {
				t.Fatalf("DecodeInitialize error = %v, want valid=%v", err, test.valid)
			}
		})
	}
}

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
