package api

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// IngestUpload uploads a file to a collection. With an empty filePath, or a
// directory, only a folder entity is created from meta. Large files are
// streamed and never read into memory.
func (c *Client) IngestUpload(ctx context.Context, collectionID, filePath string, meta Object, sync, index bool) (Object, error) {
	q := url.Values{"sync": {boolStr(sync)}, "index": {boolStr(index)}}
	target := c.URL("collections/"+url.PathEscape(collectionID)+"/ingest", q)
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}

	if filePath != "" {
		if st, err := os.Stat(filePath); err == nil && st.IsDir() {
			filePath = ""
		}
	}
	var out Object
	if filePath == "" {
		form := url.Values{"meta": {string(metaJSON)}}
		h := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
		err := c.Retry(ctx, func() error {
			resp, err := c.Do(ctx, "POST", target, strings.NewReader(form.Encode()), h)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			return json.NewDecoder(resp.Body).Decode(&out)
		})
		return out, err
	}

	err = c.Retry(ctx, func() error {
		f, err := os.Open(filePath)
		if err != nil {
			return err
		}
		defer f.Close()

		pr, pw := io.Pipe()
		mw := multipart.NewWriter(pw)
		go func() {
			err := mw.WriteField("meta", string(metaJSON))
			if err == nil {
				var part io.Writer
				part, err = mw.CreateFormFile("file", filepath.Base(filePath))
				if err == nil {
					_, err = io.Copy(part, f)
				}
			}
			if err == nil {
				err = mw.Close()
			}
			_ = pw.CloseWithError(err)
		}()

		h := http.Header{"Content-Type": {mw.FormDataContentType()}}
		resp, err := c.Do(ctx, "POST", target, pr, h)
		_ = pr.Close()
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		return json.NewDecoder(resp.Body).Decode(&out)
	})
	return out, err
}
