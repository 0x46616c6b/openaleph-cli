package api

import (
	"context"
	"strconv"
)

// Status returns the collections currently being processed. It requires a
// valid login and therefore also serves as an API key check.
func (c *Client) Status(ctx context.Context) (Object, error) {
	var out Object
	err := c.Request(ctx, "GET", c.URL("status", nil), nil, &out)
	return out, err
}

// Metadata returns operational metadata of the instance.
func (c *Client) Metadata(ctx context.Context) (Object, error) {
	var out Object
	err := c.Request(ctx, "GET", c.URL("metadata", nil), nil, &out)
	return out, err
}

// ID returns the "id" of an API object as a string, or "".
func ID(o Object) string { return idString(o["id"]) }

func idString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	}
	return ""
}
