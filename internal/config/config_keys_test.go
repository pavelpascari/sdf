package config

import "testing"

func TestConfigKeysNotEmpty(t *testing.T) {
	keys := ConfigKeys()
	if len(keys) == 0 {
		t.Fatal("ConfigKeys() returned empty slice")
	}
	for _, k := range keys {
		if k.Key == "" || k.Type == "" || k.Description == "" {
			t.Errorf("incomplete config key metadata: %+v", k)
		}
	}
}
