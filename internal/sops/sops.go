package sops

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// Decryptor handles SOPS decryption of encrypted files.
type Decryptor struct {
	ageKeyFile string
	logger     *slog.Logger
}

// NewDecryptor creates a new SOPS decryptor.
func NewDecryptor(ageKeyFile string, logger *slog.Logger) *Decryptor {
	return &Decryptor{
		ageKeyFile: ageKeyFile,
		logger:     logger,
	}
}

// Decrypt decrypts a SOPS-encrypted file and returns the plaintext content.
// The decrypted data is held only in memory — nothing is written to disk.
// Any file referenced as a secret is assumed to be SOPS-encrypted; if
// decryption fails, a descriptive error is returned.
func (d *Decryptor) Decrypt(srcPath string) ([]byte, error) {
	d.logger.Info("decrypting file", "src", srcPath)

	cmd := exec.Command("sops", "--decrypt", srcPath)
	if d.ageKeyFile != "" {
		cmd.Env = append(os.Environ(), "SOPS_AGE_KEY_FILE="+d.ageKeyFile)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("sops decrypt %s: %s: %w", srcPath, strings.TrimSpace(string(out)), err)
	}

	d.logger.Debug("file decrypted successfully", "src", srcPath)
	return out, nil
}
