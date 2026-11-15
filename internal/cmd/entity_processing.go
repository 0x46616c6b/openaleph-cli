package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

const entityTaskHint = "The command returns once the tasks are queued. Follow them with `oa status watch <collection>`."

// eachEntity runs fn for every entity ID and stops at the first error.
func eachEntity(cmd *cobra.Command, app *App, ids []string, fn func(ctx context.Context, c *api.Client, id string) error) error {
	c, err := app.client()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := fn(cmd.Context(), c, id); err != nil {
			return fmt.Errorf("entity %s: %w", id, err)
		}
	}
	return nil
}

func newEntityReingestCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "reingest <id>...",
		Short: "Re-ingest single documents",
		Long: "Queue documents that already exist in a collection for ingestion again. Needs write access to the " +
			"collection.\n\n" + entityTaskHint,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return eachEntity(cmd, app, args, func(ctx context.Context, c *api.Client, id string) error {
				return c.IngestEntity(ctx, id)
			})
		},
	}
}

func newEntityAnalyzeCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "analyze <id>...",
		Short: "Analyze single entities (OpenAleph only)",
		Long: "Queue entities for analysis: metadata extraction, language detection and OCR. Needs write access to the " +
			"collection, and the analysis queue must be enabled on the server.\n\n" + entityTaskHint,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return eachEntity(cmd, app, args, func(ctx context.Context, c *api.Client, id string) error {
				return c.AnalyzeEntity(ctx, id)
			})
		},
	}
}

func newEntityTranscribeCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "transcribe <id>...",
		Short: "Transcribe audio and video files (OpenAleph only)",
		Long: "Queue audio and video entities for speech-to-text transcription. Needs write access to the collection, " +
			"and the transcription queue must be enabled on the server.\n\n" + entityTaskHint,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return eachEntity(cmd, app, args, func(ctx context.Context, c *api.Client, id string) error {
				return c.TranscribeEntity(ctx, id)
			})
		},
	}
}

func newEntityTranslateCmd(app *App) *cobra.Command {
	var source string
	cmd := &cobra.Command{
		Use:   "translate <id>...",
		Short: "Translate documents (OpenAleph only)",
		Long: "Queue documents for translation into the target language configured on the server. Read access is " +
			"enough. The translation queue must be enabled on the server, and documents that are already translated " +
			"are rejected.\n\nWithout --source-language the server uses the detected language of the document. A " +
			"given source language must be detected for the document or set on its collection.\n\n" + entityTaskHint,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return eachEntity(cmd, app, args, func(ctx context.Context, c *api.Client, id string) error {
				return c.TranslateEntity(ctx, id, source)
			})
		},
	}
	cmd.Flags().StringVarP(&source, "source-language", "s", "", "Source language as 2-letter ISO 639 `code` (default: detected)")
	return cmd
}
