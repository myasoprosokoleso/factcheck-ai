package telegram

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	tdsession "github.com/gotd/td/session"
)

var errSessionUnauthorized = errors.New("telegram: session is not authorized")

func newSessionFileStorage(path string) (*tdsession.FileStorage, error) {
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create telegram session directory: %w", err)
	}
	if err := ensurePrivateFile(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("secure telegram session: %w", err)
	}
	return &tdsession.FileStorage{Path: path}, nil
}

// ensurePrivateFile verifies and, when necessary, tightens a regular file to
// mode 0600. It never follows a symlink.
func ensurePrivateFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("telegram: private file path must be a regular file")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod private file: %w", err)
	}
	return nil
}
