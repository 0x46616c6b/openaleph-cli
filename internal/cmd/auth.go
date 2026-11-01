package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
	"github.com/0x46616c6b/openaleph-cli/internal/config"
)

func newAuthCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth <command>",
		Short: "Authenticate oa with an OpenAleph instance",
	}
	cmd.AddCommand(newAuthLoginCmd(app), newAuthLogoutCmd(app), newAuthStatusCmd(app))
	return cmd
}

func newAuthLoginCmd(app *App) *cobra.Command {
	var withToken bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in with an API key",
		Long: "Verify an API key against an OpenAleph instance and save it.\n\n" +
			"The key is stored in the OS keyring, or in the config file (mode 0600) when no keyring is available.\n" +
			"Create the key in the OpenAleph UI under your profile settings.",
		Example: "  $ oa auth login --host https://aleph.example.org\n" +
			"  $ echo \"$KEY\" | oa auth login --host https://aleph.example.org --with-token",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := app.settings()
			if err != nil {
				return err
			}
			in := bufio.NewReader(app.IO.In)

			host := s.Host
			if app.host == "" && (host == "" || app.IO.InTTY) && !withToken {
				host, err = prompt(app, in, "OpenAleph host URL", host)
				if err != nil {
					return err
				}
			}
			host = config.NormalizeHost(host)
			if host == "" {
				return errors.New("no host given: use --host")
			}

			var key string
			switch {
			case withToken:
				line, err := in.ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) {
					return err
				}
				key = strings.TrimSpace(line)
			case app.apiKey != "":
				key = app.apiKey
			default:
				key, err = promptSecret(app, in, "API key")
				if err != nil {
					return err
				}
			}
			if key == "" {
				return errors.New("no API key given")
			}

			c, err := app.clientFor(host, key, s.Retries)
			if err != nil {
				return err
			}
			if _, err := c.Status(cmd.Context()); err != nil {
				if code := api.StatusCode(err); code == 401 || code == 403 {
					return fmt.Errorf("the API key was rejected by %s", host)
				}
				return fmt.Errorf("could not verify the API key: %w", err)
			}
			where, err := app.Store.SaveLogin(host, key)
			if err != nil {
				return err
			}
			fmt.Fprintf(app.IO.Err, "Logged in to %s (key stored in %s)\n", host, where)
			return nil
		},
	}
	cmd.Flags().BoolVar(&withToken, "with-token", false, "Read the API key from standard input")
	return cmd
}

func newAuthLogoutCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the saved API key for the current host",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := app.settings()
			if err != nil {
				return err
			}
			if s.Host == "" {
				return errNotConfigured
			}
			had, err := app.Store.Logout(s.Host)
			if err != nil {
				return err
			}
			if !had {
				fmt.Fprintf(app.IO.Err, "No saved login for %s\n", s.Host)
				return nil
			}
			fmt.Fprintf(app.IO.Err, "Logged out of %s\n", s.Host)
			return nil
		},
	}
}

func newAuthStatusCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the active host and check that the API key works",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := app.settings()
			if err != nil {
				return err
			}
			if s.Host == "" {
				return errNotConfigured
			}
			out := app.IO.Out
			fmt.Fprintf(out, "%s\n", s.Host)
			fmt.Fprintf(out, "  Host from: %s\n", s.HostSource)
			if s.APIKey == "" {
				fmt.Fprintln(out, "  API key:   none (anonymous access)")
				return errors.New("not logged in")
			}
			fmt.Fprintf(out, "  API key:   %s (from %s)\n", mask(s.APIKey), s.KeySource)

			c, err := app.clientFor(s.Host, s.APIKey, s.Retries)
			if err != nil {
				return err
			}
			if _, err := c.Status(cmd.Context()); err != nil {
				if code := api.StatusCode(err); code == 401 || code == 403 {
					fmt.Fprintln(out, "  Status:    API key rejected")
					return errors.New("authentication failed")
				}
				fmt.Fprintf(out, "  Status:    request failed: %v\n", err)
				return errors.New("could not reach the host")
			}
			fmt.Fprintln(out, "  Status:    logged in")
			return nil
		},
	}
}

func mask(key string) string {
	if len(key) <= 4 {
		return strings.Repeat("*", len(key))
	}
	return strings.Repeat("*", 8) + key[len(key)-4:]
}

func prompt(app *App, in *bufio.Reader, label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(app.IO.Err, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(app.IO.Err, "%s: ", label)
	}
	line, err := in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if line = strings.TrimSpace(line); line != "" {
		return line, nil
	}
	return def, nil
}

func promptSecret(app *App, in *bufio.Reader, label string) (string, error) {
	fmt.Fprintf(app.IO.Err, "%s: ", label)
	if f, ok := app.IO.In.(interface{ Fd() uintptr }); ok && app.IO.InTTY {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(app.IO.Err)
		return strings.TrimSpace(string(b)), err
	}
	line, err := in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
