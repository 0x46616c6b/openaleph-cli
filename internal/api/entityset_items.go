package api

import (
	"context"
	"net/url"
)

// Judgements of an entity set item. JudgementNone removes the item from the set.
const (
	JudgementPositive = "positive"
	JudgementNegative = "negative"
	JudgementUnsure   = "unsure"
	JudgementNone     = "no_judgement"
)

func entitySetPath(id string, sub ...string) string {
	p := "entitysets/" + url.PathEscape(id)
	for _, s := range sub {
		p += "/" + s
	}
	return p
}

// GetEntitySet fetches one entity set.
func (c *Client) GetEntitySet(ctx context.Context, id string) (Object, error) {
	var out Object
	err := c.Request(ctx, "GET", c.URL(entitySetPath(id), nil), nil, &out)
	return out, err
}

// UpdateEntitySet changes the given fields (for example label and summary) of an
// entity set. Fields that are not set keep their value.
func (c *Client) UpdateEntitySet(ctx context.Context, id string, data Object) (Object, error) {
	var out Object
	err := c.Request(ctx, "POST", c.URL(entitySetPath(id), nil), data, &out)
	return out, err
}

// SetEntitySetItem adds an existing entity to an entity set with a judgement,
// or changes its judgement. JudgementNone removes the entity from the set.
func (c *Client) SetEntitySetItem(ctx context.Context, id, entityID, judgement string) (Object, error) {
	var out Object
	err := c.Request(ctx, "POST", c.URL(entitySetPath(id, "items"), nil), Object{
		"entity_id": entityID,
		"judgement": judgement,
	}, &out)
	return out, err
}

// WriteEntitySetEntity creates or updates an entity and adds it to an entity
// set. New entities are created in the collection of the entity set. An entity
// the user cannot edit is only added to the set.
func (c *Client) WriteEntitySetEntity(ctx context.Context, id string, entity Object) (Object, error) {
	var out Object
	err := c.Retry(ctx, func() error {
		return c.Request(ctx, "POST", c.URL(entitySetPath(id, "entities"), nil), entity, &out)
	})
	return out, err
}
