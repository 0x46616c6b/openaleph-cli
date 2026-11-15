package api

import (
	"context"
	"net/url"
)

// CancelCollectionStatus cancels the processing of a collection. The server
// queues the cancellation and returns at once: pending tasks are dropped and
// running tasks are left to finish. It needs write access to the collection.
func (c *Client) CancelCollectionStatus(ctx context.Context, id string) error {
	return c.Request(ctx, "DELETE", c.URL("collections/"+url.PathEscape(id)+"/status", nil), nil, nil)
}
