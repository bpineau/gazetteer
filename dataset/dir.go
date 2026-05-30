package dataset

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultDirEnv is the environment variable that overrides the default
// datadir location.
const DefaultDirEnv = "GAZETTEER_DATA_DIR"

// dirName is the subdirectory created under the user cache dir.
const dirName = "gazetteer"

// DefaultDir returns the default datadir: the value of $GAZETTEER_DATA_DIR
// when set and non-empty, otherwise os.UserCacheDir()/gazetteer
// (e.g. ~/.cache/gazetteer on Linux, ~/Library/Caches/gazetteer on macOS).
//
// It does not create the directory; callers that write into it (Refresh)
// create it on demand.
func DefaultDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv(DefaultDirEnv)); v != "" {
		return v, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("dataset: resolve user cache dir: %w", err)
	}
	return filepath.Join(cache, dirName), nil
}

// ResolveDir returns explicit when it is non-empty, otherwise DefaultDir.
// The precedence is therefore explicit argument > $GAZETTEER_DATA_DIR >
// os.UserCacheDir()/gazetteer.
func ResolveDir(explicit string) (string, error) {
	if s := strings.TrimSpace(explicit); s != "" {
		return s, nil
	}
	return DefaultDir()
}
