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
		{"compatible", `{"protocolVersion":1,"server":{"name":"dsh-mygo-host","version":"0.1.0","dshVersion":"0.2.1-rc.2"},"capabilities":[]}`, true},
		{"wrong protocol", `{"protocolVersion":2,"server":{"name":"host","version":"0.1.0","dshVersion":"0.2.1-rc.2"},"capabilities":[]}`, false},
		{"wrong release", `{"protocolVersion":1,"server":{"name":"host","version":"0.2.0","dshVersion":"0.2.1-rc.2"},"capabilities":[]}`, false},
		{"sdk response", `{"serverInfo":{"name":"sdk"}}`, false},
		{"null", `null`, false},
		{"invalid capability", `{"protocolVersion":1,"server":{"name":"host","version":"0.1.0","dshVersion":"0.2.1-rc.2"},"capabilities":[1]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeInitialize(json.RawMessage(test.wire))
			if (err == nil) != test.valid {
				t.Fatalf("DecodeInitialize error = %v", err)
			}
		})
	}
}
