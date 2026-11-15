package fetch

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

// File streams fileURL to target. Unless overwrite is set, it fails with an
// error wrapping fs.ErrExist when target already exists. The data is written
// to a temporary file next to target first, so target is only ever a
// complete download.
func File(ctx context.Context, c *api.Client, fileURL, target string, overwrite bool) error {
	if !overwrite {
		if _, err := os.Lstat(target); err == nil {
			return fmt.Errorf("%s: %w", target, fs.ErrExist)
		}
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return c.Retry(ctx, func() error { return download(ctx, c, fileURL, dir, target, overwrite) })
}

// SafeName turns a server-provided file name into a single path element,
// using fallback when the name is empty or unusable.
func SafeName(name, fallback string) string { return safeName(name, fallback) }
