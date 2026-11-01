package api

import (
	"context"
	"net/url"
)

// Object is a generic JSON object as returned by the API.
type Object = map[string]any

// Page is one page of a paginated API result.
type Page struct {
	Results []Object `json:"results"`
	Total   int      `json:"total"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
	Next    string   `json:"next"`
}

// ResultSet iterates over all items of a paginated listing, following the
// "next" link of each page.
type ResultSet struct {
	c     *Client
	page  Page
	idx   int
	err   error
	patch func(Object) Object
}

func (c *Client) resultSet(ctx context.Context, rawURL string, patch func(Object) Object) (*ResultSet, error) {
	rs := &ResultSet{c: c, patch: patch}
	if err := c.Request(ctx, "GET", rawURL, nil, &rs.page); err != nil {
		return nil, err
	}
	return rs, nil
}

// Total is the total number of items reported by the server.
func (rs *ResultSet) Total() int { return rs.page.Total }

// Err returns the error that stopped iteration, if any.
func (rs *ResultSet) Err() error { return rs.err }

// Next returns the next item. ok is false at the end or on error (see Err).
func (rs *ResultSet) Next(ctx context.Context) (item Object, ok bool) {
	for rs.idx >= len(rs.page.Results) {
		if rs.page.Next == "" {
			return nil, false
		}
		var next Page
		if err := rs.c.Request(ctx, "GET", rs.page.Next, nil, &next); err != nil {
			rs.err = err
			return nil, false
		}
		rs.page, rs.idx = next, 0
		if len(next.Results) == 0 {
			return nil, false
		}
	}
	item = rs.page.Results[rs.idx]
	rs.idx++
	if rs.patch != nil {
		item = rs.patch(item)
	}
	return item, true
}

// All drains the remaining items. Use only for bounded listings.
func (rs *ResultSet) All(ctx context.Context) ([]Object, error) {
	var out []Object
	for {
		item, ok := rs.Next(ctx)
		if !ok {
			return out, rs.err
		}
		out = append(out, item)
	}
}

// Filter is a key/value pair sent as filter:<key>=<value>.
type Filter struct{ Key, Value string }

func query(q string, filters []Filter, params url.Values) url.Values {
	v := url.Values{}
	for k, vals := range params {
		for _, val := range vals {
			v.Add(k, val)
		}
	}
	if q != "" {
		v.Set("q", q)
	}
	for _, f := range filters {
		if f.Value != "" {
			v.Add("filter:"+f.Key, f.Value)
		}
	}
	return v
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
