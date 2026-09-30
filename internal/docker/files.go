package docker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/getsops/sops/v3/decrypt"
)

// SOPSExtension marks a compose config or secret whose `file:` is SOPS-encrypted.
const SOPSExtension = "x-conflux-sops"

// FilesHashLabel carries a hash of the file contents a service references.
// Compose only hashes service definitions, so this forces a container
// recreate (and thus a fresh copy) when a referenced file changes.
const FilesHashLabel = "io.conflux.files-hash"

var decryptFile = func(path string) ([]byte, error) { return decrypt.File(path, "") }

// inlineFileReferences reads every file-based config and secret (decrypting
// those marked with x-conflux-sops) and hands it to Compose as an
// environment-sourced value. Compose then copies it into the container through
// the Docker API instead of bind-mounting a host path, which would not exist
// when Conflux itself runs in a container. Nothing is written to disk. The
// environment source is used because Compose skips the bind mount for secrets
// only in that case, not for inline `content`.
func inlineFileReferences(project *types.Project) error {
	contents := map[string]string{}
	if project.Environment == nil {
		project.Environment = types.Mapping{}
	}
	inline := func(kind, name, content string) string {
		key := "CONFLUX_FILE_" + kind + "_" + name
		project.Environment[key] = content
		return key
	}

	read := func(kind, name, file string, ext types.Extensions) (string, error) {
		var data []byte
		var err error
		if marked, _ := ext[SOPSExtension].(bool); marked {
			data, err = decryptFile(file)
		} else {
			data, err = os.ReadFile(file)
		}
		if err != nil {
			return "", fmt.Errorf("reading %s %s (%s): %w", kind, name, file, err)
		}
		if len(data) == 0 {
			return "", fmt.Errorf("%s %s (%s) is empty, which Compose cannot inject", kind, name, file)
		}
		contents[kind+"/"+name] = string(data)
		return string(data), nil
	}

	for name, c := range project.Configs {
		if c.File == "" || bool(c.External) {
			continue
		}
		content, err := read("config", name, c.File, c.Extensions)
		if err != nil {
			return err
		}
		c.Environment, c.File = inline("config", name, content), ""
		project.Configs[name] = c
	}
	for name, s := range project.Secrets {
		if s.File == "" || bool(s.External) {
			continue
		}
		content, err := read("secret", name, s.File, s.Extensions)
		if err != nil {
			return err
		}
		s.Environment, s.File = inline("secret", name, content), ""
		project.Secrets[name] = s
	}

	for name, svc := range project.Services {
		var keys []string
		for _, c := range svc.Configs {
			if _, ok := contents["config/"+c.Source]; ok {
				keys = append(keys, "config/"+c.Source)
			}
		}
		for _, s := range svc.Secrets {
			if _, ok := contents["secret/"+s.Source]; ok {
				keys = append(keys, "secret/"+s.Source)
			}
		}
		if len(keys) == 0 {
			continue
		}
		sort.Strings(keys)

		h := sha256.New()
		for _, k := range keys {
			fmt.Fprintf(h, "%s\x00%d\x00%s\x00", k, len(contents[k]), contents[k])
		}
		if svc.Labels == nil {
			svc.Labels = types.Labels{}
		}
		svc.Labels[FilesHashLabel] = hex.EncodeToString(h.Sum(nil))
		project.Services[name] = svc
	}
	return nil
}
