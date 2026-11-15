package api

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

// ExportSearch asks the server to export the results of a search as a zip
// archive. The export is built in the background and shows up in
// ListExports. The options are the same as for Search.
func (c *Client) ExportSearch(ctx context.Context, o SearchOptions) error {
	return c.Request(ctx, "POST", c.URL("search/export", query(o.Query, o.filters(), o.Params)), nil, nil)
}

// ListExports lists the exports of the current user, newest first. Deleted
// and expired exports are left out by the server.
func (c *Client) ListExports(ctx context.Context, params url.Values) (*ResultSet, error) {
	return c.resultSet(ctx, c.URL("exports", params), nil)
}

// ErrExportNotFound is returned by GetExport for an unknown export ID.
var ErrExportNotFound = errors.New("export not found")

// GetExport finds an export of the current user by ID. The API has no
// endpoint for a single export, so it searches the listing.
func (c *Client) GetExport(ctx context.Context, id string) (Object, error) {
	rs, err := c.ListExports(ctx, url.Values{"limit": {"100"}})
	if err != nil {
		return nil, err
	}
	for {
		e, ok := rs.Next(ctx)
		if !ok {
			break
		}
		if ID(e) == id {
			return e, nil
		}
	}
	if err := rs.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%w: %s", ErrExportNotFound, id)
}

// ExportDownloadURL returns the download link of a finished export, or ""
// while it is not available. The link carries a signed token that expires
// after a day, so it is fetched fresh before each download.
func ExportDownloadURL(e Object) string {
	links, _ := e["links"].(map[string]any)
	u, _ := links["download"].(string)
	return u
}
