package controller

import (
	"testing"

	"bop/internal/core"
)

func TestResolveVerification(t *testing.T) {
	global := core.Verification{Enabled: false, TargetDir: "/tmp/bop-verify"}

	tests := []struct {
		name     string
		override *core.Verification
		want     core.Verification
	}{
		{
			name:     "no override falls back to global",
			override: nil,
			want:     global,
		},
		{
			name:     "override replaces global wholesale",
			override: &core.Verification{Enabled: true, TargetDir: "/tmp/bop-restore-test"},
			want:     core.Verification{Enabled: true, TargetDir: "/tmp/bop-restore-test"},
		},
		{
			name:     "partially specified override is used as-is, not backfilled",
			override: &core.Verification{Enabled: true},
			want:     core.Verification{Enabled: true, TargetDir: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveVerification(global, tt.override)
			if got != tt.want {
				t.Errorf("resolveVerification() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
