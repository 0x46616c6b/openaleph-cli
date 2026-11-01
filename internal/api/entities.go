package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strings"
)

// patchEntity adds context properties (alephUrl, publisher) to an entity.
func (c *Client) patchEntity(entity Object, publisher bool, collection Object) Object {
	props, _ := entity["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	if collection == nil {
		collection, _ = entity["collection"].(map[string]any)
	}
	links, _ := entity["links"].(map[string]any)

	apiURL, _ := links["self"].(string)
	if apiURL == "" {
		id, _ := entity["id"].(string)
		apiURL = c.URL("entities/"+id, nil)
	}
	propPush(props, "alephUrl", apiURL)

	if publisher {
		label, _ := collection["label"].(string)
		if p, ok := collection["publisher"].(string); ok && p != "" {
			label = p
		}
		propPush(props, "publisher", label)

		colLinks, _ := collection["links"].(map[string]any)
		pubURL, _ := colLinks["ui"].(string)
		if p, ok := collection["publisher_url"].(string); ok && p != "" {
			pubURL = p
		}
		propPush(props, "publisherUrl", pubURL)
	}
	entity["properties"] = props
	return entity
}

func propPush(props map[string]any, prop, value string) {
	if value == "" {
		return
	}
	vals, _ := props[prop].([]any)
	props[prop] = append(vals, value)
}

// SearchOptions configures Search.
type SearchOptions struct {
	Query     string
	Schema    string
	Schemata  string
	Filters   []Filter
	Publisher bool
	Params    url.Values
}

// Search runs an entity search. Without Schema or Schemata it searches Things.
func (c *Client) Search(ctx context.Context, o SearchOptions) (*ResultSet, error) {
	filters := append([]Filter(nil), o.Filters...)
	if o.Schema != "" {
		filters = append(filters, Filter{"schema", o.Schema})
	}
	if o.Schemata != "" {
		filters = append(filters, Filter{"schemata", o.Schemata})
	}
	if o.Schema == "" && o.Schemata == "" {
		filters = append(filters, Filter{"schemata", "Thing"})
	}
	return c.resultSet(ctx, c.URL("entities", query(o.Query, filters, o.Params)), func(e Object) Object {
		return c.patchEntity(e, o.Publisher, nil)
	})
}

// GetEntity fetches a single entity.
func (c *Client) GetEntity(ctx context.Context, id string, publisher bool) (Object, error) {
	var out Object
	if err := c.Request(ctx, "GET", c.URL("entities/"+url.PathEscape(id), nil), nil, &out); err != nil {
		return nil, err
	}
	return c.patchEntity(out, publisher, nil), nil
}

// DeleteEntity deletes a single entity.
func (c *Client) DeleteEntity(ctx context.Context, id string) error {
	return c.Request(ctx, "DELETE", c.URL("entities/"+url.PathEscape(id), nil), nil, nil)
}

// StreamOptions configures StreamEntities.
type StreamOptions struct {
	// Collection limits the stream to one collection (may be nil).
	Collection Object
	Include    []string
	Schemas    []string
	Publisher  bool
}

// StreamEntities calls fn for every entity of the stream (one JSON object per line).
func (c *Client) StreamEntities(ctx context.Context, o StreamOptions, fn func(Object) error) error {
	path := "entities/_stream"
	if o.Collection != nil {
		path = "collections/" + idString(o.Collection["id"]) + "/_stream"
	}
	q := url.Values{}
	for _, i := range o.Include {
		q.Add("include", i)
	}
	for _, s := range o.Schemas {
		q.Add("schema", s)
	}
	resp, err := c.Do(ctx, "GET", c.URL(path, q), nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	r := bufio.NewReaderSize(resp.Body, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			var e Object
			if jerr := json.Unmarshal(line, &e); jerr != nil {
				return jerr
			}
			if ferr := fn(c.patchEntity(e, o.Publisher, o.Collection)); ferr != nil {
				return ferr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return newTransportError(ctx, err)
		}
	}
}

// WriteEntity creates or updates a single entity in a collection.
func (c *Client) WriteEntity(ctx context.Context, collectionID string, entity Object, entityID string) (Object, error) {
	entity["collection_id"] = collectionID
	path := "entities"
	if entityID != "" {
		entity["id"] = entityID
		path = "entities/" + url.PathEscape(entityID)
	}
	var out Object
	err := c.Retry(ctx, func() error {
		return c.Request(ctx, "POST", c.URL(path, nil), entity, &out)
	})
	return out, err
}

// BulkOptions configures WriteEntities.
type BulkOptions struct {
	ChunkSize   int
	EntitySetID string
	// Unsafe allows references to archive hashes.
	Unsafe bool
	// Cleaned disables server-side validation of all types.
	Cleaned bool
	// Force logs failed chunks via OnError and continues.
	Force   bool
	OnError func(error)
	// OnChunk is called after each sent chunk with the number of entities so far.
	OnChunk func(total int)
}

// WriteEntities sends entities to a collection in chunks.
func (c *Client) WriteEntities(ctx context.Context, collectionID string, entities iter.Seq2[Object, error], o BulkOptions) error {
	size := o.ChunkSize
	if size <= 0 {
		size = 1000
	}
	chunk := make([]Object, 0, size)
	total := 0
	flush := func() error {
		if len(chunk) == 0 {
			return nil
		}
		err := c.bulkChunk(ctx, collectionID, chunk, o)
		total += len(chunk)
		chunk = chunk[:0]
		if o.OnChunk != nil {
			o.OnChunk(total)
		}
		return err
	}
	for e, err := range entities {
		if err != nil {
			return err
		}
		chunk = append(chunk, e)
		if len(chunk) >= size {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

func (c *Client) bulkChunk(ctx context.Context, collectionID string, chunk []Object, o BulkOptions) error {
	q := url.Values{}
	if o.EntitySetID != "" {
		q.Set("entityset_id", o.EntitySetID)
	}
	if o.Unsafe {
		q.Set("safe", "false")
	}
	if o.Cleaned {
		q.Set("clean", "false")
	}
	err := c.Retry(ctx, func() error {
		return c.Request(ctx, "POST", c.URL("collections/"+url.PathEscape(collectionID)+"/_bulk", q), chunk, nil)
	})
	if err != nil && o.Force {
		if o.OnError != nil {
			o.OnError(err)
		}
		return nil
	}
	return err
}

// Match finds entities similar to the given sample entity.
func (c *Client) Match(ctx context.Context, entity Object, collectionIDs []string, publisher bool) ([]Object, error) {
	q := url.Values{}
	for _, id := range collectionIDs {
		q.Add("collection_ids", id)
	}
	var page Page
	if err := c.Request(ctx, http.MethodPost, c.URL("match", q), entity, &page); err != nil {
		return nil, err
	}
	for i, r := range page.Results {
		page.Results[i] = c.patchEntity(r, publisher, nil)
	}
	return page.Results, nil
}
