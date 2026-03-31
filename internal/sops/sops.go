package sops

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/getsops/sops/v3/decrypt"
)

// Decryptor handles SOPS decryption of encrypted files using the SOPS Go
// library. This replaces the previous implementation that shelled out to the
// `sops` CLI binary — eliminating an external dependency from the container
// image and providing richer error handling.
type Decryptor struct {
	ageKeyFile string
	logger     *slog.Logger
}

// NewDecryptor creates a new SOPS decryptor. If ageKeyFile is non-empty, the
// SOPS_AGE_KEY_FILE environment variable is set so the SOPS library can find
// the age identity file for decryption.
func NewDecryptor(ageKeyFile string, logger *slog.Logger) *Decryptor {
	if ageKeyFile != "" {
		os.Setenv("SOPS_AGE_KEY_FILE", ageKeyFile)
	}
	return &Decryptor{
		ageKeyFile: ageKeyFile,
		logger:     logger,
	}
}

// Decrypt decrypts a SOPS-encrypted file and returns the plaintext content.
// The decrypted data is held only in memory — nothing is written to disk.
//
// The file format (yaml, json, dotenv, ini, binary) is auto-detected from
// the file extension by the SOPS library. Files ending in .env are treated
// as dotenv format, which is the expected format for conflux secret files.
func (d *Decryptor) Decrypt(srcPath string) ([]byte, error) {
	d.logger.Info("decrypting file", "src", srcPath)

	// Empty format string → auto-detect from file extension.
	// .env → dotenv, .yaml/.yml → yaml, .json → json, etc.
	cleartext, err := decrypt.File(srcPath, "")
	if err != nil {
		return nil, fmt.Errorf("sops decrypt %s: %w", srcPath, err)
	}

	d.logger.Debug("file decrypted successfully", "src", srcPath, "bytes", len(cleartext))
	return cleartext, nil
}
