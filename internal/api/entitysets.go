package api

import (
	"context"
	"net/url"
)

// EntitySets lists entity sets, optionally limited to a collection and types.
func (c *Client) EntitySets(ctx context.Context, collectionID string, types []string, prefix string) (*ResultSet, error) {
	filters := []Filter{{"collection_id", collectionID}}
	for _, t := range types {
		filters = append(filters, Filter{"type", t})
	}
	params := url.Values{}
	if prefix != "" {
		params.Set("prefix", prefix)
	}
	return c.resultSet(ctx, c.URL("entitysets", query("", filters, params)), nil)
}

// EntitySetItems lists the items of an entity set.
func (c *Client) EntitySetItems(ctx context.Context, id string, publisher bool) (*ResultSet, error) {
	return c.resultSet(ctx, c.URL("entitysets/"+url.PathEscape(id)+"/items", nil), func(item Object) Object {
		if e, ok := item["entity"].(map[string]any); ok {
			item["entity"] = c.patchEntity(e, publisher, nil)
		}
		return item
	})
}

// CreateEntitySet creates an entity set (for example a list) inside a collection.
func (c *Client) CreateEntitySet(ctx context.Context, collectionID, typ, label, summary string) (Object, error) {
	var out Object
	err := c.Request(ctx, "POST", c.URL("entitysets", nil), Object{
		"collection_id": collectionID,
		"type":          typ,
		"label":         label,
		"summary":       summary,
		"entities":      []any{},
	}, &out)
	return out, err
}

// DeleteEntitySet deletes an entity set.
func (c *Client) DeleteEntitySet(ctx context.Context, id string, sync bool) error {
	q := url.Values{"sync": {boolStr(sync)}}
	return c.Request(ctx, "DELETE", c.URL("entitysets/"+url.PathEscape(id), q), nil, nil)
}
