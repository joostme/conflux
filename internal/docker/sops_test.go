package docker

import (
	"os"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
)

func TestDecryptMarkedFiles(t *testing.T) {
	orig := decryptFile
	t.Cleanup(func() { decryptFile = orig })
	decryptFile = func(path string) ([]byte, error) { return []byte("plain:" + path), nil }

	project := &types.Project{
		Configs: types.Configs{
			"enc":   {File: "/repo/enc.conf", Extensions: types.Extensions{SOPSExtension: true}},
			"plain": {File: "/repo/plain.conf"},
		},
		Secrets: types.Secrets{
			"key": {File: "/repo/key.pem", Extensions: types.Extensions{SOPSExtension: true}},
		},
	}

	cleanup, err := decryptMarkedFiles(project)
	if err != nil {
		t.Fatalf("decryptMarkedFiles() error = %v", err)
	}

	if project.Configs["plain"].File != "/repo/plain.conf" {
		t.Errorf("unmarked config must be untouched, got %s", project.Configs["plain"].File)
	}
	for name, path := range map[string]string{"enc": project.Configs["enc"].File, "key": project.Secrets["key"].File} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading decrypted %s: %v", name, err)
		}
		if string(data[:6]) != "plain:" {
			t.Errorf("%s not decrypted: %q", name, data)
		}
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %o, want 600", name, info.Mode().Perm())
		}
	}

	decrypted := project.Configs["enc"].File
	cleanup()
	if _, err := os.Stat(decrypted); !os.IsNotExist(err) {
		t.Errorf("cleanup did not remove %s", decrypted)
	}
}
