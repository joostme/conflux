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

// FilesHashLabel carries a hash of the config and secret contents a service
// references. Compose only hashes service definitions, not what it copies into
// the container, so this forces a recreate (and thus a fresh copy) when a
// referenced file, inline `content` or environment source changes.
const FilesHashLabel = "io.conflux.files-hash"

var decryptFile = func(path string) ([]byte, error) { return decrypt.File(path, "") }

// inlineFileReferences reads every file-based config and secret (decrypting
// those marked with x-conflux-sops) and hands it to Compose as an
// environment-sourced value. Compose then copies it into the container through
// the Docker API instead of bind-mounting a host path, which would not exist
// when Conflux itself runs in a container. Nothing is written to disk. The
// environment source is used because Compose skips the bind mount for secrets
// only in that case, not for inline `content`. Every service then gets a hash
// of all config and secret contents it references, see FilesHashLabel.
func inlineFileReferences(project *types.Project) error {
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
		contents := map[string]string{}
		for _, ref := range svc.Configs {
			c := project.Configs[ref.Source]
			if content := injectedContent(project, c.Content, c.Environment); content != "" {
				contents["config/"+ref.Source] = content
			}
		}
		for _, ref := range svc.Secrets {
			s := project.Secrets[ref.Source]
			if content := injectedContent(project, s.Content, s.Environment); content != "" {
				contents["secret/"+ref.Source] = content
			}
		}
		if len(contents) == 0 {
			continue
		}
		keys := make([]string, 0, len(contents))
		for k := range contents {
			keys = append(keys, k)
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

// injectedContent returns what Compose copies into the container for a config
// or secret: its inline content, or else the value of its environment source.
func injectedContent(project *types.Project, content, env string) string {
	if content != "" {
		return content
	}
	if env != "" {
		return project.Environment[env]
	}
	return ""
}
