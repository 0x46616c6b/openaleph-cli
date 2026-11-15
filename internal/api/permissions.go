package api

import (
	"context"
	"net/url"
)

// PermissionUpdate sets the access of one role to a collection. The role is
// given by RoleID or Email. Write implies read, and both false removes access.
type PermissionUpdate struct {
	RoleID string `json:"role_id,omitempty"`
	Email  string `json:"email,omitempty"`
	Read   bool   `json:"read"`
	Write  bool   `json:"write"`
}

// ListPermissions returns the permissions of a collection. The server also
// lists groups (and, for admins, the system roles) without access, with read
// and write set to false. It needs write access to the collection.
func (c *Client) ListPermissions(ctx context.Context, collectionID string) ([]Object, error) {
	var out struct {
		Results []Object `json:"results"`
	}
	err := c.Request(ctx, "GET", c.URL("collections/"+url.PathEscape(collectionID)+"/permissions", nil), nil, &out)
	return out.Results, err
}

// UpdatePermissions changes the access of the given roles and returns the
// permissions of the collection afterwards. The server silently skips roles
// that do not exist or that the caller may not see.
func (c *Client) UpdatePermissions(ctx context.Context, collectionID string, updates []PermissionUpdate) ([]Object, error) {
	var out struct {
		Results []Object `json:"results"`
	}
	err := c.Request(ctx, "PUT", c.URL("collections/"+url.PathEscape(collectionID)+"/permissions", nil), updates, &out)
	return out.Results, err
}
