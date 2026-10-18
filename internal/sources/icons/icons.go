// Package icons turns a program's icon (a file and an icon index, as listed in
// the registry's DisplayIcon) into a small PNG data URI for the UI. It only
// reads: the icon file is opened for resource extraction, never written, and
// nothing in it is executed.
package icons

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

type key struct {
	path  string
	index int
}

var (
	mu    sync.Mutex
	cache = map[key]string{} // "" is a remembered failure
)

// DataURI returns the icon at index in the file at path as a
// `data:image/png;base64,…` string. Results, including failures, are cached
// for the life of the process. Only absolute paths to .exe, .dll and .ico
// files are accepted.
func DataURI(path string, index int) (string, error) {
	if err := checkPath(path); err != nil {
		return "", err
	}
	k := key{strings.ToLower(path), index}
	mu.Lock()
	uri, seen := cache[k]
	mu.Unlock()
	if seen {
		if uri == "" {
			return "", fmt.Errorf("no icon in %s", path)
		}
		return uri, nil
	}

	png, err := extractPNG(path, index)
	if err == nil {
		uri = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	}
	mu.Lock()
	cache[k] = uri
	mu.Unlock()
	return uri, err
}

func checkPath(path string) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".exe", ".dll", ".ico":
	default:
		return fmt.Errorf("not an icon source: %s", path)
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("icon path is not absolute: %s", path)
	}
	return nil
}
