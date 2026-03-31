package sops

import (
	"testing"
)

func TestIsEncrypted(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"encrypted env file", "secrets.enc.env", true},
		{"encrypted yaml file", "config.enc.yaml", true},
		{"plain env file", "environment.env", false},
		{"plain secrets file", "secrets.env", false},
		{"no extension", "Dockerfile", false},
		{"enc in directory not filename", "enc.dir/file.env", false},
		{"multiple enc markers", "data.enc.enc.env", true},
		{"enc at end", "file.enc.", true},
		{"just enc", ".enc.", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsEncrypted(tt.filename)
			if got != tt.want {
				t.Errorf("IsEncrypted(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestDecryptedPath(t *testing.T) {
	tests := []struct {
		name         string
		workDir      string
		relativePath string
		want         string
	}{
		{
			name:         "simple encrypted env",
			workDir:      "/work",
			relativePath: "secrets.enc.env",
			want:         "/work/secrets.env",
		},
		{
			name:         "nested path",
			workDir:      "/work",
			relativePath: "stacks/nginx/secrets.enc.env",
			want:         "/work/stacks/nginx/secrets.env",
		},
		{
			name:         "no enc marker (still works, just no replacement)",
			workDir:      "/work",
			relativePath: "environment.env",
			want:         "/work/environment.env",
		},
		{
			name:         "different work dir",
			workDir:      "/data/work",
			relativePath: "secrets.enc.env",
			want:         "/data/work/secrets.env",
		},
		{
			name:         "deeply nested",
			workDir:      "/work",
			relativePath: "a/b/c/secrets.enc.yaml",
			want:         "/work/a/b/c/secrets.yaml",
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
