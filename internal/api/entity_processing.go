package api

import (
	"context"
	"net/url"
)

// entityTask starts a server task for one entity. The server answers with
// 202 Accepted and an empty body.
func (c *Client) entityTask(ctx context.Context, id, task string, in any) error {
	return c.Request(ctx, "POST", c.URL("entities/"+url.PathEscape(id)+"/"+task, nil), in, nil)
}

// IngestEntity queues a document that already exists in a collection for
// ingestion again.
func (c *Client) IngestEntity(ctx context.Context, id string) error {
	return c.entityTask(ctx, id, "ingest", nil)
}

// AnalyzeEntity queues an entity for analysis (OpenAleph only).
func (c *Client) AnalyzeEntity(ctx context.Context, id string) error {
	return c.entityTask(ctx, id, "analyze", nil)
}

// TranscribeEntity queues an audio or video entity for transcription
// (OpenAleph only).
func (c *Client) TranscribeEntity(ctx context.Context, id string) error {
	return c.entityTask(ctx, id, "transcribe", nil)
}

// TranslateEntity queues a document for translation into the target language
// configured on the server (OpenAleph only). sourceLanguage is optional; if it
// is empty, the server uses the detected language of the document.
func (c *Client) TranslateEntity(ctx context.Context, id, sourceLanguage string) error {
	var in any
	if sourceLanguage != "" {
		in = map[string]string{"source_language": sourceLanguage}
	}
	return c.entityTask(ctx, id, "translate", in)
}
