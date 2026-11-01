package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/output"
)

func newAPICmd(app *App) *cobra.Command {
	var (
		method string
		fields []string
		input  string
		jq     string
		raw    bool
	)
	cmd := &cobra.Command{
		Use:   "api <path>",
		Short: "Make an authenticated request to the OpenAleph API",
		Long: "Send a request to the API of the configured host and print the response.\n\n" +
			"A path starting with / is relative to the host, anything else is relative to /api/2/.\n" +
			"With -f key=value, parameters are sent as query string for GET and DELETE requests and as\n" +
			"a JSON object body otherwise. Use --input to send a file (or - for stdin) as the body.",
		Example: "  $ oa api status\n" +
			"  $ oa api collections -f limit=5 --jq '.results[].label'\n" +
			"  $ oa api /api/2/collections/12/reindex -X POST",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			target := args[0]
			if strings.HasPrefix(target, "/") {
				target = c.Host() + target
			} else {
				target = c.URL(target, nil)
			}
			method = strings.ToUpper(method)

			params := map[string]any{}
			for _, f := range fields {
				k, v, ok := strings.Cut(f, "=")
				if !ok || k == "" {
					return fmt.Errorf("invalid field %q, expected key=value", f)
				}
				params[k] = v
			}

			var body io.Reader
			header := http.Header{}
			switch {
			case input != "":
				if len(params) > 0 {
					return errors.New("--input cannot be combined with -f")
				}
				r := app.IO.In
				if input != "-" {
					f, err := os.Open(input)
					if err != nil {
						return err
					}
					defer f.Close()
					r = f
				}
				body = r
				header.Set("Content-Type", "application/json")
			case len(params) > 0 && (method == "GET" || method == "DELETE"):
				u, err := url.Parse(target)
				if err != nil {
					return err
				}
				q := u.Query()
				for k, v := range params {
					q.Add(k, v.(string))
				}
				u.RawQuery = q.Encode()
				target = u.String()
			case len(params) > 0:
				b, err := json.Marshal(params)
				if err != nil {
					return err
				}
				body = bytes.NewReader(b)
				header.Set("Content-Type", "application/json")
			}

			resp, err := c.Do(cmd.Context(), method, target, body, header)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			if len(bytes.TrimSpace(data)) == 0 {
				return nil
			}
			if raw {
				_, err = app.IO.Out.Write(data)
				return err
			}
			var v any
			if err := json.Unmarshal(data, &v); err != nil {
				// Not JSON (for example a file download): pass it through.
				_, werr := app.IO.Out.Write(data)
				return werr
			}
			return output.WriteJSON(app.IO.Out, v, output.JSONOptions{JQ: jq, Pretty: app.IO.OutTTY})
		},
	}
	cmd.Flags().StringVarP(&method, "method", "X", "GET", "HTTP `method` for the request")
	cmd.Flags().StringArrayVarP(&fields, "field", "f", nil, "Add a request parameter as `key=value`")
	cmd.Flags().StringVar(&input, "input", "", "Send the contents of this `file` as the request body (- for stdin)")
	cmd.Flags().StringVarP(&jq, "jq", "q", "", "Filter the JSON response with a jq `expression`")
	cmd.Flags().BoolVar(&raw, "raw", false, "Print the response body unchanged")
	return cmd
}
