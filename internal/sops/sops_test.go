package sops

import (
	"testing"
)

func TestDecryptedPath(t *testing.T) {
	tests := []struct {
		name         string
		workDir      string
		relativePath string
		want         string
	}{
		{
			name:         "simple env file",
			workDir:      "/work",
			relativePath: "secrets.env",
			want:         "/work/secrets.decrypted.env",
		},
		{
			name:         "nested path",
			workDir:      "/work",
			relativePath: "stacks/nginx/secrets.env",
			want:         "/work/stacks/nginx/secrets.decrypted.env",
		},
		{
			name:         "yaml file",
			workDir:      "/work",
			relativePath: "config.yaml",
			want:         "/work/config.decrypted.yaml",
		},
		{
			name:         "different work dir",
			workDir:      "/data/work",
			relativePath: "secrets.env",
			want:         "/data/work/secrets.decrypted.env",
		},
		{
			name:         "deeply nested",
			workDir:      "/work",
			relativePath: "a/b/c/secrets.yaml",
			want:         "/work/a/b/c/secrets.decrypted.yaml",
		},
		{
			name:         "no extension",
			workDir:      "/work",
			relativePath: "secrets",
			want:         "/work/secrets.decrypted",
		},
		{
			name:         "legacy enc naming still works",
			workDir:      "/work",
			relativePath: "secrets.enc.env",
			want:         "/work/secrets.enc.decrypted.env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecryptedPath(tt.workDir, tt.relativePath)
			if got != tt.want {
				t.Errorf("DecryptedPath(%q, %q) = %q, want %q", tt.workDir, tt.relativePath, got, tt.want)
			}
		})
	}
}
