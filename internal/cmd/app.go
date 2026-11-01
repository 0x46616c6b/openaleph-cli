package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
	"github.com/0x46616c6b/openaleph-cli/internal/config"
	"github.com/0x46616c6b/openaleph-cli/internal/output"
)

// IOStreams bundles the standard streams of a command run.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	OutTTY bool
	InTTY  bool
}

// SystemIO returns the process streams.
func SystemIO() *IOStreams {
	return &IOStreams{
		In: os.Stdin, Out: os.Stdout, Err: os.Stderr,
		OutTTY: output.IsTerminal(os.Stdout),
		InTTY:  output.IsTerminal(os.Stdin),
	}
}

// App holds the dependencies shared by all commands.
type App struct {
	IO     *IOStreams
	Getenv func(string) string
	Store  *config.Store
	HTTP   *http.Client

	// global flags
	host    string
	apiKey  string
	retries int
}

var errNotConfigured = errors.New("no OpenAleph host configured: run `oa auth login` or set OA_HOST and OA_API_KEY")

func (a *App) settings() (config.Settings, error) {
	return a.Store.Resolve(a.Getenv, a.host, a.apiKey, a.retries)
}

func (a *App) clientFor(host, key string, retries int) (*api.Client, error) {
	return api.New(api.Options{
		Host:       host,
		APIKey:     key,
		Retries:    retries,
		UserAgent:  "oa/" + version,
		HTTPClient: a.HTTP,
	})
}

// client builds an API client from the resolved settings.
func (a *App) client() (*api.Client, error) {
	s, err := a.settings()
	if err != nil {
		return nil, err
	}
	if s.Host == "" {
		return nil, errNotConfigured
	}
	return a.clientFor(s.Host, s.APIKey, s.Retries)
}

// jsonFlags are the shared --json and --jq flags.
type jsonFlags struct {
	fields string
	jq     string
}

func (j *jsonFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringVar(&j.fields, "json", "", "Output JSON with the specified `fields` (comma-separated, or \"all\")")
	cmd.Flags().StringVarP(&j.jq, "jq", "q", "", "Filter JSON output with a jq `expression`")
}

func (j *jsonFlags) enabled() bool { return j.fields != "" || j.jq != "" }

func (j *jsonFlags) write(a *App, v any) error {
	var fields []string
	for _, f := range strings.Split(j.fields, ",") {
		if f = strings.TrimSpace(f); f != "" {
			fields = append(fields, f)
		}
	}
	return output.WriteJSON(a.IO.Out, v, output.JSONOptions{Fields: fields, JQ: j.jq, Pretty: a.IO.OutTTY})
}

// str returns a string field of an API object.
func str(o api.Object, key string) string {
	switch v := o[key].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	case bool:
		return fmt.Sprint(v)
	}
	return ""
}
