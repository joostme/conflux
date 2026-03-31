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
// The source file should have ".enc." in the name (e.g., secrets.enc.env).
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
// It replaces ".enc." with "." in the filename and places it under workDir.
// The relativePath is preserved to maintain directory structure.
func DecryptedPath(workDir, relativePath string) string {
	dir := filepath.Dir(relativePath)
	base := filepath.Base(relativePath)
	decryptedBase := strings.Replace(base, ".enc.", ".", 1)
	return filepath.Join(workDir, dir, decryptedBase)
}

// IsEncrypted checks if a filename contains ".enc." indicating it's SOPS-encrypted.
func IsEncrypted(filename string) bool {
	return strings.Contains(filepath.Base(filename), ".enc.")
}
