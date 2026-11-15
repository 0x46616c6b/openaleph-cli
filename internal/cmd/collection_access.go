package cmd

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
	"github.com/0x46616c6b/openaleph-cli/internal/output"
)

func newCollectionAccessCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "access <command>",
		Aliases: []string{"permissions"},
		Short:   "Manage who can read and write a collection",
		Long: "Show and change the access of users and groups to a collection. This needs write access to the\n" +
			"collection. Admins also see and can change the system roles \"guest\" (everyone, also without login)\n" +
			"and \"user\" (every logged-in user), and see the email addresses of users.\n\n" +
			"A user or group is given as a role ID, as the email address of a user, or by the name or foreign ID\n" +
			"of a role that `oa collection access list --all` shows.",
	}
	cmd.AddCommand(
		newCollectionAccessListCmd(app),
		newCollectionAccessGrantCmd(app),
		newCollectionAccessRevokeCmd(app),
	)
	return cmd
}

func newCollectionAccessListCmd(app *App) *cobra.Command {
	var jf jsonFlags
	var all bool
	cmd := &cobra.Command{
		Use:     "list <collection>",
		Aliases: []string{"ls"},
		Short:   "List the users and groups with access to a collection",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			coll, err := c.ResolveCollection(ctx, args[0])
			if err != nil {
				return err
			}
			perms, err := c.ListPermissions(ctx, str(coll, "id"))
			if err != nil {
				return err
			}
			if !all {
				perms = withAccess(perms)
			}
			if jf.enabled() {
				return jf.write(app, perms)
			}
			if len(perms) == 0 {
				fmt.Fprintln(app.IO.Err, "Nobody has access through a permission (use --all to list the groups you can add)")
				return nil
			}
			t := output.NewTable(app.IO.Out, "ROLE ID", "TYPE", "NAME", "EMAIL", "ACCESS")
			for _, p := range perms {
				r := permRole(p)
				t.Row(str(r, "id"), str(r, "type"), roleName(r), str(r, "email"), accessLevel(p))
			}
			return t.Flush()
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Also list groups and system roles without access")
	jf.add(cmd)
	return cmd
}

func newCollectionAccessGrantCmd(app *App) *cobra.Command {
	var write bool
	cmd := &cobra.Command{
		Use:   "grant <collection> <user-or-group>",
		Short: "Give a user or group read or write access to a collection",
		Long: "Give a user or group read access, or write access with --write. Running it again changes the access,\n" +
			"so grant without --write turns write access into read access.\n\n" +
			"Granting access to the system role \"guest\" publishes the collection to everyone, also to visitors\n" +
			"who are not logged in.",
		Example: "  $ oa collection access grant leak_a jane@example.org\n" +
			"  $ oa collection access grant leak_a 42 --write\n" +
			"  $ oa collection access grant leak_a \"Research desk\"",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			level := "read"
			if write {
				level = "write"
			}
			return setAccess(cmd.Context(), app, args[0], args[1], true, write,
				"Granted "+level+" access to %s for %s\n")
		},
	}
	cmd.Flags().BoolVarP(&write, "write", "w", false, "Grant write access (default: read only)")
	return cmd
}

func newCollectionAccessRevokeCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "revoke <collection> <user-or-group>",
		Short:   "Remove the access of a user or group to a collection",
		Example: "  $ oa collection access revoke leak_a jane@example.org",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setAccess(cmd.Context(), app, args[0], args[1], false, false,
				"Revoked access to %s for %s\n")
		},
	}
}

// setAccess changes the access of one role and checks that the server applied it,
// because the server skips unknown roles without an error.
func setAccess(ctx context.Context, app *App, collRef, who string, read, write bool, done string) error {
	c, err := app.client()
	if err != nil {
		return err
	}
	coll, err := c.ResolveCollection(ctx, collRef)
	if err != nil {
		return err
	}
	id := str(coll, "id")
	before, err := c.ListPermissions(ctx, id)
	if err != nil {
		return err
	}
	update, match, err := accessTarget(before, who)
	if err != nil {
		return err
	}
	update.Read, update.Write = read, write
	after, err := c.UpdatePermissions(ctx, id, []api.PermissionUpdate{update})
	if err != nil {
		return err
	}

	ok := false
	if p := findPerm(after, match); p != nil {
		r, w := permFlag(p, "read"), permFlag(p, "write")
		if r != read || w != write {
			return fmt.Errorf("the server did not apply the change for %s: access is %s", who, accessLevel(p))
		}
		ok = true
	} else if !read && findPerm(before, match) != nil {
		// A user without access is no longer listed.
		ok = true
	} else if permState(before) != permState(after) {
		// The change is visible, but not the user: only admins see email addresses.
		ok = true
	}
	if !ok {
		if read {
			return fmt.Errorf("nothing changed for %s: no such user or group that you can see, or the access was already set", who)
		}
		return fmt.Errorf("nothing changed for %s: no such user or group that you can see, or it had no access", who)
	}
	fmt.Fprintf(app.IO.Err, done, firstNonEmpty(str(coll, "foreign_id"), id), who)
	return nil
}

// accessTarget turns a role ID, an email address or a role name into an update
// and a matcher for the permissions the server returns.
func accessTarget(perms []api.Object, who string) (api.PermissionUpdate, func(api.Object) bool, error) {
	who = strings.TrimSpace(who)
	switch {
	case who == "":
		return api.PermissionUpdate{}, nil, errors.New("no user or group given")
	case strings.Contains(who, "@"):
		return api.PermissionUpdate{Email: who}, func(r api.Object) bool {
			return strings.EqualFold(str(r, "email"), who)
		}, nil
	}
	if _, err := strconv.ParseUint(who, 10, 64); err == nil {
		return api.PermissionUpdate{RoleID: who}, func(r api.Object) bool { return str(r, "id") == who }, nil
	}
	byName := func(r api.Object) bool {
		for _, k := range []string{"foreign_id", "name", "label"} {
			if v := str(r, k); v != "" && strings.EqualFold(v, who) {
				return true
			}
		}
		return false
	}
	var ids []string
	for _, p := range perms {
		if r := permRole(p); byName(r) {
			ids = append(ids, str(r, "id"))
		}
	}
	switch len(ids) {
	case 0:
		return api.PermissionUpdate{}, nil, fmt.Errorf("no role named %q is listed for this collection: "+
			"give a role ID or the email address of a user (see `oa collection access list --all`)", who)
	case 1:
		id := ids[0]
		return api.PermissionUpdate{RoleID: id}, func(r api.Object) bool { return str(r, "id") == id }, nil
	}
	sort.Strings(ids)
	return api.PermissionUpdate{}, nil, fmt.Errorf("%q matches several roles (IDs %s): give a role ID", who, strings.Join(ids, ", "))
}

func withAccess(perms []api.Object) []api.Object {
	var out []api.Object
	for _, p := range perms {
		if permFlag(p, "read") || permFlag(p, "write") {
			out = append(out, p)
		}
	}
	return out
}

func findPerm(perms []api.Object, match func(api.Object) bool) api.Object {
	for _, p := range perms {
		if match(permRole(p)) {
			return p
		}
	}
	return nil
}

// permState summarizes who has which access, to detect a change.
func permState(perms []api.Object) string {
	var parts []string
	for _, p := range withAccess(perms) {
		parts = append(parts, str(permRole(p), "id")+"="+accessLevel(p))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func permRole(p api.Object) api.Object {
	r, _ := p["role"].(map[string]any)
	return r
}

func permFlag(p api.Object, key string) bool {
	b, _ := p[key].(bool)
	return b
}

func accessLevel(p api.Object) string {
	switch {
	case permFlag(p, "write"):
		return "write"
	case permFlag(p, "read"):
		return "read"
	}
	return "none"
}

func roleName(r api.Object) string {
	return firstNonEmpty(str(r, "label"), str(r, "name"), str(r, "foreign_id"))
}
