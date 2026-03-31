package sops

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Decryptor handles SOPS decryption of encrypted env files.
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

// DecryptFile decrypts a SOPS-encrypted file and writes the output to destPath.
// Any file referenced as a secret is assumed to be SOPS-encrypted. If decryption
// fails, an error is returned — callers should not pre-check the filename.
func (d *Decryptor) DecryptFile(srcPath, destPath string) error {
	d.logger.Info("decrypting file", "src", srcPath, "dest", destPath)

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("creating dest dir: %w", err)
	}

	cmd := exec.Command("sops", "--decrypt", srcPath)
	if d.ageKeyFile != "" {
		cmd.Env = append(os.Environ(), "SOPS_AGE_KEY_FILE="+d.ageKeyFile)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sops decrypt %s: %s: %w", srcPath, strings.TrimSpace(string(out)), err)
	}

	if err := os.WriteFile(destPath, out, 0600); err != nil {
		return fmt.Errorf("writing decrypted file %s: %w", destPath, err)
	}

	d.logger.Debug("file decrypted successfully", "dest", destPath)
	return nil
}

// DecryptedPath returns the destination path for a decrypted file.
// The file is placed under workDir with its directory structure preserved
// and a ".decrypted" suffix inserted before the file extension
// (e.g., "secrets.env" → "secrets.decrypted.env").
// If the file has no extension, ".decrypted" is appended.
func DecryptedPath(workDir, relativePath string) string {
	dir := filepath.Dir(relativePath)
	base := filepath.Base(relativePath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	decryptedBase := name + ".decrypted" + ext
	return filepath.Join(workDir, dir, decryptedBase)
}
