package api

import (
	"context"
	"net/url"
)

func mappingPath(collectionID, id string) string {
	p := "collections/" + url.PathEscape(collectionID) + "/mappings"
	if id != "" {
		p += "/" + url.PathEscape(id)
	}
	return p
}

// Mappings lists the mappings of a collection.
func (c *Client) Mappings(ctx context.Context, collectionID string) (*ResultSet, error) {
	return c.resultSet(ctx, c.URL(mappingPath(collectionID, ""), nil), nil)
}

// GetMapping fetches a single mapping.
func (c *Client) GetMapping(ctx context.Context, collectionID, id string) (Object, error) {
	var out Object
	err := c.Request(ctx, "GET", c.URL(mappingPath(collectionID, id), nil), nil, &out)
	return out, err
}

// CreateMapping creates a mapping that applies query to the table entity tableID.
func (c *Client) CreateMapping(ctx context.Context, collectionID, tableID string, query Object) (Object, error) {
	var out Object
	err := c.Request(ctx, "POST", c.URL(mappingPath(collectionID, ""), nil), Object{
		"table_id":      tableID,
		"mapping_query": query,
	}, &out)
	return out, err
}

// UpdateMapping replaces the table and query of a mapping.
func (c *Client) UpdateMapping(ctx context.Context, collectionID, id, tableID string, query Object) (Object, error) {
	var out Object
	err := c.Request(ctx, "POST", c.URL(mappingPath(collectionID, id), nil), Object{
		"table_id":      tableID,
		"mapping_query": query,
	}, &out)
	return out, err
}

// DeleteMapping deletes a mapping. The entities it generated are kept.
func (c *Client) DeleteMapping(ctx context.Context, collectionID, id string) error {
	return c.Request(ctx, "DELETE", c.URL(mappingPath(collectionID, id), nil), nil, nil)
}

// TriggerMapping starts generating entities from the mapping's table.
func (c *Client) TriggerMapping(ctx context.Context, collectionID, id string) error {
	return c.Request(ctx, "POST", c.URL(mappingPath(collectionID, id)+"/trigger", nil), nil, nil)
}

// FlushMapping deletes the entities the mapping generated.
func (c *Client) FlushMapping(ctx context.Context, collectionID, id string) error {
	return c.Request(ctx, "POST", c.URL(mappingPath(collectionID, id)+"/flush", nil), nil, nil)
}
