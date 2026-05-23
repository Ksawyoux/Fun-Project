package ingestor

import (
	"io/fs"
	"os"
)

// osStat is wrapped so the ast-go ingestor's `fsStat` indirection compiles
// without importing os everywhere.
func osStat(p string) (fs.FileInfo, error) { return os.Stat(p) }
