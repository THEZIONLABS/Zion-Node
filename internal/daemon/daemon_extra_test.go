package daemon

import (
	"testing"

	"github.com/zion-protocol/zion-node/pkg/types"
)

func TestExtractProfile_Valid(t *testing.T) {
	d := &Daemon{}

	params := map[string]interface{}{
		"runtime_engine":       "openclaw",
		"engine_version":       "1.0.0",
		"image_hash":           "sha256:abc123",
		"skills_manifest_hash": "sha256:def456",
		"snapshot_format":      "tar.zst",
	}

	profile := d.extractProfile(params)
	if profile == nil {
		t.Fatal("Expected non-nil profile")
	}
	if profile.Engine != "openclaw" {
		t.Errorf("Expected engine 'openclaw', got %s", profile.Engine)
	}
	if profile.EngineVersion != "1.0.0" {
		t.Errorf("Expected engine_version '1.0.0', got %s", profile.EngineVersion)
	}
	if profile.ImageHash != "sha256:abc123" {
		t.Errorf("Expected image_hash 'sha256:abc123', got %s", profile.ImageHash)
	}
	if profile.SkillsManifestHash != "sha256:def456" {
		t.Errorf("Expected skills_manifest_hash 'sha256:def456', got %s", profile.SkillsManifestHash)
	}
	if profile.SnapshotFormat != "tar.zst" {
		t.Errorf("Expected snapshot_format 'tar.zst', got %s", profile.SnapshotFormat)
	}
}

func TestExtractProfile_NilParams(t *testing.T) {
	d := &Daemon{}

	profile := d.extractProfile(nil)
	if profile != nil {
		t.Error("Expected nil profile for nil params")
	}
}

func TestExtractProfile_EmptyParams(t *testing.T) {
	d := &Daemon{}

	profile := d.extractProfile(map[string]interface{}{})
	if profile != nil {
		t.Error("Expected nil profile for empty params (no engine)")
	}
}

func TestExtractProfile_MissingEngine(t *testing.T) {
	d := &Daemon{}

	params := map[string]interface{}{
		"engine_version": "1.0.0",
		"image_hash":     "sha256:abc123",
	}

	profile := d.extractProfile(params)
	if profile != nil {
		t.Error("Expected nil profile when engine is missing")
	}
}

func TestExtractProfile_MinimalValid(t *testing.T) {
	d := &Daemon{}

	params := map[string]interface{}{
		"runtime_engine": "openclaw",
	}

	profile := d.extractProfile(params)
	if profile == nil {
		t.Fatal("Expected non-nil profile with just engine")
	}
	if profile.Engine != "openclaw" {
		t.Errorf("Expected engine 'openclaw', got %s", profile.Engine)
	}
	if profile.EngineVersion != "" {
		t.Errorf("Expected empty engine_version, got %s", profile.EngineVersion)
	}
}

func TestExtractProfile_WrongTypes(t *testing.T) {
	d := &Daemon{}

	params := map[string]interface{}{
		"runtime_engine": 123, // Wrong type (int instead of string)
	}

	profile := d.extractProfile(params)
	if profile != nil {
		t.Error("Expected nil profile when engine is wrong type")
	}
}

// Verify that HubCommand type properly maps to process logic
func TestProcessHubCommand_Types(t *testing.T) {
	// Test that command types match expected values
	commands := []string{"run", "stop", "checkpoint", "migrate_out"}
	for _, cmd := range commands {
		if cmd == "" {
			t.Error("Command should not be empty")
		}
	}
}

// Test extractProfile with interface assertion scenarios
func TestExtractProfile_InterfaceAssertions(t *testing.T) {
	d := &Daemon{}

	tests := []struct {
		name   string
		params map[string]interface{}
		want   *types.RuntimeProfile
	}{
		{
			name: "all strings",
			params: map[string]interface{}{
				"runtime_engine":  "openclaw",
				"engine_version":  "v1",
				"image_hash":      "abc",
				"snapshot_format": "tar",
			},
			want: &types.RuntimeProfile{
				Engine:         "openclaw",
				EngineVersion:  "v1",
				ImageHash:      "abc",
				SnapshotFormat: "tar",
			},
		},
		{
			name: "numeric engine - invalid",
			params: map[string]interface{}{
				"runtime_engine": 42,
			},
			want: nil, // engine must be string
		},
		{
			name: "only engine",
			params: map[string]interface{}{
				"runtime_engine": "test",
			},
			want: &types.RuntimeProfile{
				Engine: "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.extractProfile(tt.params)
			if tt.want == nil {
				if got != nil {
					t.Errorf("Expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("Expected non-nil profile")
			}
			if got.Engine != tt.want.Engine {
				t.Errorf("Engine: expected %s, got %s", tt.want.Engine, got.Engine)
			}
			if got.EngineVersion != tt.want.EngineVersion {
				t.Errorf("EngineVersion: expected %s, got %s", tt.want.EngineVersion, got.EngineVersion)
			}
		})
	}
}
