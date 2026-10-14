package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
)

func newProject(t *testing.T, plain, enc string) *types.Project {
	t.Helper()
	dir := t.TempDir()
	plainPath := filepath.Join(dir, "site.conf")
	encPath := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(plainPath, []byte(plain), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(encPath, []byte(enc), 0o644); err != nil {
		t.Fatal(err)
	}
	return &types.Project{
		Configs: types.Configs{"site": {File: plainPath}},
		Secrets: types.Secrets{"key": {File: encPath, Extensions: types.Extensions{SOPSExtension: true}}},
		Services: types.Services{
			"web":   {Name: "web", Configs: []types.ServiceConfigObjConfig{{Source: "site"}}, Secrets: []types.ServiceSecretConfig{{Source: "key"}}},
			"other": {Name: "other"},
		},
	}
}

func stubDecrypt(t *testing.T) {
	t.Helper()
	orig := decryptFile
	t.Cleanup(func() { decryptFile = orig })
	decryptFile = func(path string) ([]byte, error) {
		data, err := os.ReadFile(path)
		return []byte(strings.TrimPrefix(string(data), "enc:")), err
	}
}

func TestInlineFileReferences(t *testing.T) {
	stubDecrypt(t)
	p := newProject(t, "hello", "enc:s3cret")

	if err := inlineFileReferences(p); err != nil {
		t.Fatalf("inlineFileReferences() error = %v", err)
	}

	c, s := p.Configs["site"], p.Secrets["key"]
	if c.File != "" || p.Environment[c.Environment] != "hello" {
		t.Errorf("config not inlined: %+v", c)
	}
	if s.File != "" || p.Environment[s.Environment] != "s3cret" {
		t.Errorf("secret not decrypted and inlined: %+v", s)
	}
	if p.Services["web"].Labels[FilesHashLabel] == "" {
		t.Error("service referencing files must get a hash label")
	}
	if _, ok := p.Services["other"].Labels[FilesHashLabel]; ok {
		t.Error("service without files must not get a hash label")
	}
}

func TestInlineFileReferences_HashChangesWithContent(t *testing.T) {
	stubDecrypt(t)
	hash := func(plain, enc string) string {
		p := newProject(t, plain, enc)
		if err := inlineFileReferences(p); err != nil {
			t.Fatal(err)
		}
		return p.Services["web"].Labels[FilesHashLabel]
	}

	base := hash("a", "enc:b")
	if base != hash("a", "enc:b") {
		t.Error("hash must be stable for identical content")
	}
	if base == hash("a2", "enc:b") || base == hash("a", "enc:b2") {
		t.Error("hash must change when content changes")
	}
}

func TestInlineFileReferences_RejectsEmptyFile(t *testing.T) {
	stubDecrypt(t)
	if err := inlineFileReferences(newProject(t, "", "enc:x")); err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestInlineFileReferences_SkipsExternalAndContent(t *testing.T) {
	p := &types.Project{Configs: types.Configs{
		"inline": {Content: "already"},
		"ext":    {External: true},
	}}
	if err := inlineFileReferences(p); err != nil {
		t.Fatalf("inlineFileReferences() error = %v", err)
	}
	if p.Configs["inline"].Content != "already" || p.Configs["inline"].Environment != "" {
		t.Error("inline content must be left alone")
	}
}

func TestInlineFileReferences_HashesInlineAndEnvironmentSources(t *testing.T) {
	hash := func(content, env string) string {
		p := &types.Project{
			Environment: types.Mapping{"ROUTES": env},
			Configs: types.Configs{
				"inline": {Content: content},
				"env":    {Environment: "ROUTES"},
			},
			Services: types.Services{
				"proxy": {Name: "proxy", Configs: []types.ServiceConfigObjConfig{{Source: "inline"}, {Source: "env"}}},
			},
		}
		if err := inlineFileReferences(p); err != nil {
			t.Fatal(err)
		}
		return p.Services["proxy"].Labels[FilesHashLabel]
	}

	base := hash("a", "b")
	if base == "" {
		t.Fatal("service referencing inline or environment configs must get a hash label")
	}
	if base != hash("a", "b") {
		t.Error("hash must be stable for identical content")
	}
	if base == hash("a2", "b") || base == hash("a", "b2") {
		t.Error("hash must change when inline or environment content changes")
	}
}
