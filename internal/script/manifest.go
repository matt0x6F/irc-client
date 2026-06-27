package script

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Manifest is the optional `// cascade:` header a script declares. It is parsed
// from raw source (before the package merge, which strips file-level comments)
// and is informational in v1 (permissions are captured but not enforced).
type Manifest struct {
	Name        string
	Description string
	Permissions []string
}

// parseManifest scans the .go files in dir for `// cascade:<key> <value>` lines.
// Unknown keys are ignored; the first value for a key wins.
func parseManifest(dir string) Manifest {
	var m Manifest
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			rest, ok := strings.CutPrefix(line, "//")
			if !ok {
				continue
			}
			rest = strings.TrimSpace(rest)
			kv, ok := strings.CutPrefix(rest, "cascade:")
			if !ok {
				continue
			}
			key, val, _ := strings.Cut(kv, " ")
			val = strings.TrimSpace(val)
			switch strings.TrimSpace(key) {
			case "name":
				if m.Name == "" {
					m.Name = val
				}
			case "description":
				if m.Description == "" {
					m.Description = val
				}
			case "permissions":
				if m.Permissions == nil {
					for _, p := range strings.Split(val, ",") {
						if p = strings.TrimSpace(p); p != "" {
							m.Permissions = append(m.Permissions, p)
						}
					}
				}
			}
		}
		fh.Close()
	}
	return m
}
