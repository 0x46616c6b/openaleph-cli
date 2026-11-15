package api

import (
	"context"
	"net/url"
	"strconv"
)

// RelationOptions configures the paginated relation listings of an entity
// (similar, mentions and nearby).
type RelationOptions struct {
	// Query narrows the results with a free-text search. Only the mentions
	// listing supports it.
	Query   string
	Filters []Filter
	Params  url.Values
}

func (c *Client) entityRelation(ctx context.Context, id, rel string, o RelationOptions, patch func(Object) Object) (*ResultSet, error) {
	return c.resultSet(ctx, c.URL("entities/"+url.PathEscape(id)+"/"+rel, query(o.Query, o.Filters, o.Params)), patch)
}

// SimilarEntities lists entities that are similar to the given one, best
// match first. Each item has a "score", a "judgement" (if the pair was judged
// in a profile) and the matching "entity".
func (c *Client) SimilarEntities(ctx context.Context, id string, o RelationOptions) (*ResultSet, error) {
	return c.entityRelation(ctx, id, "similar", o, func(item Object) Object {
		if e, ok := item["entity"].(map[string]any); ok {
			item["entity"] = c.patchEntity(e, false, nil)
		}
		return item
	})
}

// EntityMentions lists documents whose text mentions the name of the given
// entity.
func (c *Client) EntityMentions(ctx context.Context, id string, o RelationOptions) (*ResultSet, error) {
	return c.entityRelation(ctx, id, "mentions", o, func(e Object) Object {
		return c.patchEntity(e, false, nil)
	})
}

// NearbyEntities lists entities with a location close to the given entity,
// nearest first. The entity needs latitude and longitude (an Address), else
// the result is empty.
func (c *Client) NearbyEntities(ctx context.Context, id string, o RelationOptions) (*ResultSet, error) {
	return c.entityRelation(ctx, id, "nearby", o, func(e Object) Object {
		return c.patchEntity(e, false, nil)
	})
}

// ExpandGroup holds the entities connected to an entity through one property.
type ExpandGroup struct {
	Property string `json:"property"`
	// Count is the number of connections, which can be more than the
	// entities returned.
	Count    int      `json:"count"`
	Entities []Object `json:"entities"`
}

// ExpandEntity lists the entities connected to the given one, grouped by the
// property that connects them. properties limits the expansion to these
// properties, and limit is the maximum number of entities per property (0
// uses the server default).
func (c *Client) ExpandEntity(ctx context.Context, id string, properties []string, limit int) ([]ExpandGroup, error) {
	var filters []Filter
	for _, p := range properties {
		filters = append(filters, Filter{"property", p})
	}
	params := url.Values{}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	var out struct {
		Results []ExpandGroup `json:"results"`
	}
	if err := c.Request(ctx, "GET", c.URL("entities/"+url.PathEscape(id)+"/expand", query("", filters, params)), nil, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

// EntityTag is a value of an entity (such as a name, email address or IBAN)
// with the number of other entities that have the same value.
type EntityTag struct {
	ID    string `json:"id"`
	Field string `json:"field"`
	Value string `json:"value"`
	Count int    `json:"count"`
}

// EntityTags lists the values of an entity that other entities share, most
// shared first.
func (c *Client) EntityTags(ctx context.Context, id string) ([]EntityTag, error) {
	var out struct {
		Results []EntityTag `json:"results"`
	}
	if err := c.Request(ctx, "GET", c.URL("entities/"+url.PathEscape(id)+"/tags", nil), nil, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}
