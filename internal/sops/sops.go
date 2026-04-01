package sops

import (
	"fmt"
	"log/slog"

	"github.com/getsops/sops/v3/decrypt"
)

// Decryptor handles SOPS decryption of encrypted files.
type Decryptor struct{}

// NewDecryptor creates a SOPS decryptor. Age key discovery is handled by
// the SOPS_AGE_KEY_FILE environment variable (set externally).
func NewDecryptor() *Decryptor {
	return &Decryptor{}
}

// Decrypt decrypts a SOPS-encrypted file and returns the plaintext.
// Format is auto-detected from file extension.
func (d *Decryptor) Decrypt(srcPath string) ([]byte, error) {
	slog.Info("decrypting file", "src", srcPath)

	cleartext, err := decrypt.File(srcPath, "")
	if err != nil {
		return nil, fmt.Errorf("sops decrypt %s: %w", srcPath, err)
	}

	slog.Debug("file decrypted successfully", "src", srcPath, "bytes", len(cleartext))
	return cleartext, nil
}
