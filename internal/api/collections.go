package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// CollectionConfig holds the defaults used when a collection is created on demand.
type CollectionConfig struct {
	Label     string
	Casefile  bool
	Category  string
	Languages []string
	Summary   string
}

// GetCollection fetches a collection by numeric ID (not foreign ID).
func (c *Client) GetCollection(ctx context.Context, id string) (Object, error) {
	var out Object
	err := c.Request(ctx, "GET", c.URL("collections/"+url.PathEscape(id), nil), nil, &out)
	return out, err
}

// ListCollections lists collections matching an optional query and filters.
func (c *Client) ListCollections(ctx context.Context, q string, filters []Filter, params url.Values) (*ResultSet, error) {
	return c.resultSet(ctx, c.URL("collections", query(q, filters, params)), nil)
}

// CollectionByForeignID returns the collection with the given foreign ID, or nil.
func (c *Client) CollectionByForeignID(ctx context.Context, foreignID string) (Object, error) {
	if foreignID == "" {
		return nil, nil
	}
	rs, err := c.ListCollections(ctx, "", []Filter{{"foreign_id", foreignID}}, nil)
	if err != nil {
		return nil, err
	}
	item, ok := rs.Next(ctx)
	if !ok {
		return nil, rs.Err()
	}
	return item, nil
}

// ResolveCollection accepts a numeric ID or a foreign ID.
func (c *Client) ResolveCollection(ctx context.Context, ref string) (Object, error) {
	if _, err := strconv.ParseUint(ref, 10, 64); err == nil {
		return c.GetCollection(ctx, ref)
	}
	coll, err := c.CollectionByForeignID(ctx, ref)
	if err != nil {
		return nil, err
	}
	if coll == nil {
		return nil, fmt.Errorf("collection %q does not exist", ref)
	}
	return coll, nil
}

// LoadCollectionByForeignID returns the collection with the given foreign ID,
// creating it from cfg if it does not exist.
func (c *Client) LoadCollectionByForeignID(ctx context.Context, foreignID string, cfg CollectionConfig) (Object, error) {
	coll, err := c.CollectionByForeignID(ctx, foreignID)
	if err != nil || coll != nil {
		return coll, err
	}
	label := cfg.Label
	if label == "" {
		label = foreignID
	}
	category := cfg.Category
	if category == "" {
		category = "other"
	}
	languages := cfg.Languages
	if languages == nil {
		languages = []string{}
	}
	return c.CreateCollection(ctx, Object{
		"foreign_id": foreignID,
		"label":      label,
		"casefile":   cfg.Casefile,
		"category":   category,
		"languages":  languages,
		"summary":    cfg.Summary,
	})
}

// CreateCollection creates a collection from the given data.
func (c *Client) CreateCollection(ctx context.Context, data Object) (Object, error) {
	var out Object
	err := c.Request(ctx, "POST", c.URL("collections", nil), data, &out)
	return out, err
}

// UpdateCollection updates an existing collection.
func (c *Client) UpdateCollection(ctx context.Context, id string, data Object, sync bool) (Object, error) {
	q := url.Values{"sync": {boolStr(sync)}}
	var out Object
	err := c.Request(ctx, "PUT", c.URL("collections/"+url.PathEscape(id), q), data, &out)
	return out, err
}

// DeleteCollection deletes a collection and all of its contents.
func (c *Client) DeleteCollection(ctx context.Context, id string, sync bool) error {
	q := url.Values{"sync": {boolStr(sync)}}
	return c.Request(ctx, "DELETE", c.URL("collections/"+url.PathEscape(id), q), nil, nil)
}

// FlushCollection empties a collection but keeps its metadata.
func (c *Client) FlushCollection(ctx context.Context, id string, sync bool) error {
	q := url.Values{"sync": {boolStr(sync)}, "keep_metadata": {"true"}}
	return c.Request(ctx, "DELETE", c.URL("collections/"+url.PathEscape(id), q), nil, nil)
}

// ReingestCollection re-ingests all documents in a collection.
func (c *Client) ReingestCollection(ctx context.Context, id string, index bool) error {
	q := url.Values{"index": {boolStr(index)}}
	return c.Request(ctx, "POST", c.URL("collections/"+url.PathEscape(id)+"/reingest", q), nil, nil)
}

// ReindexCollection re-indexes all entities in a collection.
func (c *Client) ReindexCollection(ctx context.Context, id string, flush, sync bool) error {
	q := url.Values{"sync": {boolStr(sync)}, "flush": {boolStr(flush)}}
	return c.Request(ctx, "POST", c.URL("collections/"+url.PathEscape(id)+"/reindex", q), nil, nil)
}

// CollectionStatus returns processing status of a single collection.
func (c *Client) CollectionStatus(ctx context.Context, id string) (Object, error) {
	var out Object
	err := c.Request(ctx, "GET", c.URL("collections/"+url.PathEscape(id)+"/status", nil), nil, &out)
	return out, err
}
