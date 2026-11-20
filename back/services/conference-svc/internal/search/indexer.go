package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/confhub/conference-svc/internal/domain"
)

// indexMapping is versioned through the index name: a change here needs a
// new default index name in config.go; the indexer fills the new index from
// PostgreSQL on start.
//
// Every text field has a stemmed variant for whole words and a ".prefix"
// variant (edge n-grams) so that the beginning of a word also matches.
const indexMapping = `{
  "settings": {
    "index": {"refresh_interval": "1s", "max_result_window": 100000, "number_of_replicas": 0, "max_ngram_diff": 18},
    "analysis": {
      "char_filter": {
        "yo": {"type": "mapping", "mappings": ["ё => е", "Ё => Е"]}
      },
      "filter": {
        "ru_stop": {"type": "stop", "stopwords": "_russian_"},
        "ru_stem": {"type": "stemmer", "language": "russian"},
        "en_stop": {"type": "stop", "stopwords": "_english_"},
        "en_stem": {"type": "stemmer", "language": "english"},
        "prefix_grams": {"type": "edge_ngram", "min_gram": 2, "max_gram": 20}
      },
      "analyzer": {
        "confhub_ru": {"tokenizer": "standard", "char_filter": ["yo"], "filter": ["lowercase", "ru_stop", "ru_stem"]},
        "confhub_en": {"tokenizer": "standard", "char_filter": ["yo"], "filter": ["lowercase", "en_stop", "en_stem"]},
        "confhub_prefix": {"tokenizer": "standard", "char_filter": ["yo"], "filter": ["lowercase", "ru_stop", "en_stop", "prefix_grams"]},
        "confhub_prefix_search": {"tokenizer": "standard", "char_filter": ["yo"], "filter": ["lowercase", "ru_stop", "en_stop"]}
      },
      "normalizer": {
        "lower": {"type": "custom", "char_filter": ["yo"], "filter": ["lowercase"]}
      }
    }
  },
  "mappings": {
    "dynamic": "strict",
    "properties": {
      "title": {"type": "text", "analyzer": "confhub_ru", "fields": {
        "raw": {"type": "keyword", "normalizer": "lower"},
        "prefix": {"type": "text", "analyzer": "confhub_prefix", "search_analyzer": "confhub_prefix_search"}}},
      "title_en": {"type": "text", "analyzer": "confhub_en", "fields": {
        "prefix": {"type": "text", "analyzer": "confhub_prefix", "search_analyzer": "confhub_prefix_search"}}},
      "slug": {"type": "keyword"},
      "description": {"type": "text", "analyzer": "confhub_ru", "fields": {
        "prefix": {"type": "text", "analyzer": "confhub_prefix", "search_analyzer": "confhub_prefix_search"}}},
      "description_en": {"type": "text", "analyzer": "confhub_en", "fields": {
        "prefix": {"type": "text", "analyzer": "confhub_prefix", "search_analyzer": "confhub_prefix_search"}}},
      "format": {"type": "keyword"},
      "status": {"type": "keyword"},
      "start_date": {"type": "date"},
      "end_date": {"type": "date"},
      "city": {"type": "text", "analyzer": "confhub_ru", "fields": {
        "raw": {"type": "keyword"},
        "lower": {"type": "keyword", "normalizer": "lower"},
        "prefix": {"type": "text", "analyzer": "confhub_prefix", "search_analyzer": "confhub_prefix_search"}}},
      "topics": {"type": "text", "analyzer": "confhub_ru", "fields": {
        "keyword": {"type": "keyword"},
        "prefix": {"type": "text", "analyzer": "confhub_prefix", "search_analyzer": "confhub_prefix_search"}}},
      "organizer_names": {"type": "text", "analyzer": "confhub_ru", "fields": {
        "prefix": {"type": "text", "analyzer": "confhub_prefix", "search_analyzer": "confhub_prefix_search"}}},
      "speakers": {"type": "text", "analyzer": "confhub_ru", "fields": {
        "prefix": {"type": "text", "analyzer": "confhub_prefix", "search_analyzer": "confhub_prefix_search"}}},
      "program": {"type": "text", "analyzer": "confhub_ru", "fields": {
        "prefix": {"type": "text", "analyzer": "confhub_prefix", "search_analyzer": "confhub_prefix_search"}}},
      "publication_allowed": {"type": "boolean"},
      "created_at": {"type": "date"},
      "updated_at": {"type": "date"}
    }
  }
}`

type Source interface {
	SearchIndexBatch(context.Context, time.Time, uuid.UUID, int) ([]domain.SearchDocument, error)
}

type Indexer struct {
	client *Client
	source Source
	// indexed remembers updated_at of recently indexed rows so the overlap
	// window does not resend unchanged documents.
	indexed map[uuid.UUID]time.Time
}

func NewIndexer(client *Client, source Source) *Indexer {
	return &Indexer{client: client, source: source, indexed: map[uuid.UUID]time.Time{}}
}

const (
	syncInterval = 2 * time.Second
	// syncOverlap re-reads recent rows: updated_at is the transaction start
	// time, so a long transaction can commit with a timestamp behind the cursor.
	syncOverlap = time.Minute
	// fullResyncEvery re-reads the whole table to repair any remaining drift.
	fullResyncEvery = time.Hour
	syncBatch       = 500
)

func (i *Indexer) Run(ctx context.Context) error {
	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	var cursor time.Time
	var cursorID uuid.UUID
	lastFull := time.Now()
	for {
		created, err := i.client.ensureIndex(ctx)
		if err != nil {
			i.client.setReady(false)
			slog.Error("ensure search index", "err", err)
		} else {
			if created || time.Since(lastFull) >= fullResyncEvery {
				cursor, cursorID, lastFull = time.Time{}, uuid.Nil, time.Now()
				clear(i.indexed)
			}
			i.client.setReady(true) // a partial rebuild remains searchable
			if next, err := i.sync(ctx, cursor, cursorID); err != nil {
				slog.Error("sync search index", "err", err)
			} else if !next.IsZero() {
				cursor, cursorID = next.Add(-syncOverlap), uuid.Nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// sync indexes rows after the cursor and returns the newest updated_at seen,
// or the zero time when nothing was read.
func (i *Indexer) sync(ctx context.Context, cursor time.Time, cursorID uuid.UUID) (time.Time, error) {
	var newest time.Time
	for {
		docs, err := i.source.SearchIndexBatch(ctx, cursor, cursorID, syncBatch)
		if err != nil {
			return newest, err
		}
		if len(docs) == 0 {
			return newest, nil
		}
		changed := make([]domain.SearchDocument, 0, len(docs))
		for _, d := range docs {
			if seen, ok := i.indexed[d.ID]; !ok || !seen.Equal(d.UpdatedAt) {
				changed = append(changed, d)
			}
		}
		if len(changed) > 0 {
			if err := i.client.bulk(ctx, changed); err != nil {
				return newest, err
			}
		}
		last := docs[len(docs)-1]
		cursor, cursorID = last.UpdatedAt, last.ID
		if last.UpdatedAt.After(newest) {
			newest = last.UpdatedAt
		}
		for _, d := range changed {
			i.indexed[d.ID] = d.UpdatedAt
		}
		if len(docs) < syncBatch {
			i.forgetBefore(newest.Add(-2 * syncOverlap))
			return newest, nil
		}
	}
}

func (i *Indexer) forgetBefore(t time.Time) {
	for id, at := range i.indexed {
		if at.Before(t) {
			delete(i.indexed, id)
		}
	}
}

func (c *Client) ensureIndex(ctx context.Context) (bool, error) {
	resp, err := c.do(ctx, http.MethodHead, "/"+c.index, nil)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return false, nil
	}
	if resp.StatusCode != http.StatusNotFound {
		return false, fmt.Errorf("index probe returned %d", resp.StatusCode)
	}
	resp, err = c.do(ctx, http.MethodPut, "/"+c.index, []byte(indexMapping))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		return false, elasticError(resp)
	}
	return resp.StatusCode == http.StatusOK, nil
}

func (c *Client) bulk(ctx context.Context, docs []domain.SearchDocument) error {
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	for _, d := range docs {
		action := "delete"
		if d.Searchable() {
			action = "index"
		}
		if err := enc.Encode(map[string]any{action: map[string]any{"_index": c.index, "_id": d.ID.String()}}); err != nil {
			return err
		}
		if action == "index" {
			if err := enc.Encode(d); err != nil {
				return err
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/_bulk", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return elasticError(resp)
	}
	var decoded struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			Status int             `json:"status"`
			Error  json.RawMessage `json:"error"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return err
	}
	if !decoded.Errors {
		return nil
	}
	for _, item := range decoded.Items {
		for action, result := range item {
			if result.Status >= 300 && !(action == "delete" && result.Status == http.StatusNotFound) {
				return errors.New("elasticsearch bulk item failed: " + string(result.Error))
			}
		}
	}
	return nil
}
