package config

import (
	"encoding/json"
	"testing"
)

func TestV3NetLeafAutoJoinEnabledDefaultsTrue(t *testing.T) {
	var l V3NetLeafConfig
	if err := json.Unmarshal([]byte(`{"hubUrl":"http://x","network":"felonynet"}`), &l); err != nil {
		t.Fatal(err)
	}
	if !l.AutoJoinEnabled() {
		t.Error("missing autoJoinAreas must default to enabled")
	}

	f := false
	l.AutoJoinAreas = &f
	if l.AutoJoinEnabled() {
		t.Error("explicit false must disable auto-join")
	}
}

func TestV3NetLeafRequireSignedMessagesDefaultsOff(t *testing.T) {
	var l V3NetLeafConfig
	if err := json.Unmarshal([]byte(`{"hubUrl":"http://x","network":"felonynet"}`), &l); err != nil {
		t.Fatal(err)
	}
	if l.RequireSignedMessages {
		t.Error("missing requireSignedMessages must default to off")
	}

	if err := json.Unmarshal([]byte(`{"hubUrl":"http://x","network":"felonynet","requireSignedMessages":true}`), &l); err != nil {
		t.Fatal(err)
	}
	if !l.RequireSignedMessages {
		t.Error("requireSignedMessages true was not read")
	}
}
