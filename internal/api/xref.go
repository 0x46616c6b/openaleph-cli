package api

import (
	"context"
	"net/url"
	"strconv"
)

func xrefPath(collectionID string) string {
	return "collections/" + url.PathEscape(collectionID) + "/xref"
}

// XrefOptions narrows the cross-referencing results of a collection.
type XrefOptions struct {
	// MatchCollectionIDs keeps only matches from these collections (numeric IDs).
	MatchCollectionIDs []string
	// MinScore keeps only matches with at least this score. The server always
	// drops matches with a score of 0.5 or less.
	MinScore float64
	// Schema keeps only matches of this schema.
	Schema string
	// Query searches the names of the matched entities.
	Query string
	// Limit is the page size.
	Limit int
}

// Xref lists the cross-referencing matches of a collection, best score first.
func (c *Client) Xref(ctx context.Context, collectionID string, o XrefOptions) (*ResultSet, error) {
	params := url.Values{}
	if o.Limit > 0 {
		params.Set("limit", strconv.Itoa(o.Limit))
	}
	var filters []Filter
	for _, id := range o.MatchCollectionIDs {
		filters = append(filters, Filter{Key: "match_collection_id", Value: id})
	}
	if o.MinScore > 0 {
		filters = append(filters, Filter{Key: "gte:score", Value: strconv.FormatFloat(o.MinScore, 'f', -1, 64)})
	}
	filters = append(filters, Filter{Key: "schema", Value: o.Schema})
	return c.resultSet(ctx, c.URL(xrefPath(collectionID), query(o.Query, filters, params)), nil)
}

// StartXref starts cross-referencing the collection against all other
// collections. It runs on the server and replaces earlier results.
func (c *Client) StartXref(ctx context.Context, collectionID string) error {
	return c.Request(ctx, "POST", c.URL(xrefPath(collectionID), nil), nil, nil)
}

// ExportXref starts an export of the cross-referencing results as an Excel
// file. The server builds the file in the background and lists it under
// /api/2/exports when it is ready.
func (c *Client) ExportXref(ctx context.Context, collectionID string) error {
	return c.Request(ctx, "POST", c.URL(xrefPath(collectionID)+".xlsx", nil), nil, nil)
}
