package scanner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Match represents the relationship between a *.RDY file and a directory with
// the same base name.
type Match struct {
	ReadyFile     string      `json:"readyFile"`
	Folder        string      `json:"folder,omitempty"`
	MissingFolder bool        `json:"missingFolder"`
	FolderEntries []FileEntry `json:"folderEntries,omitempty"`
}

// FileEntry represents a child entry inside a matched folder.
type FileEntry struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	Path    string    `json:"path"`
}

// Options control scanning behavior.
type Options struct {
	Recursive      bool
	FollowSymlinks bool
}

////////////////////////////////////////////////////////////////////////////////

// Scan scans the provided directory for *.RDY files and finds sibling folders
// sharing the same base name.
func Scan(root string, opts Options) ([]Match, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("root is not a directory")
	}

	var rdyFiles []string

	if opts.Recursive {
		walkFn := func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				if strings.HasSuffix(strings.ToUpper(d.Name()), ".RDY") {
					rdyFiles = append(rdyFiles, path)
				}
				return nil
			}
			if !opts.FollowSymlinks && d.Type()&os.ModeSymlink != 0 {
				return fs.SkipDir
			}
			return nil
		}
		if err := filepath.WalkDir(root, walkFn); err != nil {
			return nil, fmt.Errorf("walk error: %w", err)
		}
	} else {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.HasSuffix(strings.ToUpper(e.Name()), ".RDY") {
				rdyFiles = append(rdyFiles, filepath.Join(root, e.Name()))
			}
		}
	}

	sort.Strings(rdyFiles)
	matches := make([]Match, 0, len(rdyFiles))

	for _, rdy := range rdyFiles {
		base := filepath.Base(rdy)
		nameNoExt := strings.TrimSuffix(base, filepath.Ext(base))
		candidateDir := filepath.Join(filepath.Dir(rdy), nameNoExt)

		m := Match{ReadyFile: rdy}
		if st, err := os.Stat(candidateDir); err == nil && st.IsDir() {
			m.Folder = candidateDir
			entries, err := os.ReadDir(candidateDir)
			if err != nil {
				// NOTE(joel): Treat as missing contents rather than whole failure.
				m.MissingFolder = true
			} else {
				for _, e := range entries {
					// NOTE(joel): Ignoring error; may lack modtime/size if fail
					finfo, _ := e.Info()
					fe := FileEntry{
						Name: e.Name(),
						Path: filepath.Join(candidateDir, e.Name()),
					}
					if finfo != nil {
						fe.Size = finfo.Size()
						fe.ModTime = finfo.ModTime()
					}
					m.FolderEntries = append(m.FolderEntries, fe)
				}
				sort.Slice(m.FolderEntries, func(i, j int) bool { return m.FolderEntries[i].Name < m.FolderEntries[j].Name })
			}
		} else {
			m.MissingFolder = true
		}
		matches = append(matches, m)
	}

	return matches, nil
}
