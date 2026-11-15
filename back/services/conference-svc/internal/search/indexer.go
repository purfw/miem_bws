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

const indexMapping = `{
  "settings": {
    "index": {"refresh_interval": "1s", "max_result_window": 100000, "number_of_replicas": 0},
    "analysis": {
      "filter": {
        "ru_stop": {"type": "stop", "stopwords": "_russian_"},
        "ru_stem": {"type": "stemmer", "language": "russian"},
        "en_stop": {"type": "stop", "stopwords": "_english_"},
        "en_stem": {"type": "stemmer", "language": "english"}
      },
      "analyzer": {
        "confhub_ru": {"tokenizer": "standard", "filter": ["lowercase", "ru_stop", "ru_stem"]},
        "confhub_en": {"tokenizer": "standard", "filter": ["lowercase", "en_stop", "en_stem"]}
      }
    }
  },
  "mappings": {
    "dynamic": "strict",
    "properties": {
      "title": {"type": "text", "analyzer": "confhub_ru", "fields": {"raw": {"type": "keyword"}, "suggest": {"type": "search_as_you_type"}}},
      "title_en": {"type": "text", "analyzer": "confhub_en", "fields": {"suggest": {"type": "search_as_you_type"}}},
      "slug": {"type": "keyword"},
      "description": {"type": "text", "analyzer": "confhub_ru"},
      "description_en": {"type": "text", "analyzer": "confhub_en"},
      "format": {"type": "keyword"},
      "status": {"type": "keyword"},
      "start_date": {"type": "date"},
      "end_date": {"type": "date"},
      "city": {"type": "text", "analyzer": "confhub_ru", "fields": {"raw": {"type": "keyword"}}},
      "topics": {"type": "text", "analyzer": "confhub_ru", "fields": {"keyword": {"type": "keyword"}}},
      "organizer_names": {"type": "text", "analyzer": "confhub_ru"},
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
}

func NewIndexer(client *Client, source Source) *Indexer {
	return &Indexer{client: client, source: source}
}

func (i *Indexer) Run(ctx context.Context) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var cursor time.Time
	var cursorID uuid.UUID
	for {
		created, err := i.client.ensureIndex(ctx)
		if err != nil {
			i.client.setReady(false)
			slog.Error("ensure search index", "err", err)
		} else {
			if created {
				cursor, cursorID = time.Time{}, uuid.Nil
			}
			i.client.setReady(true) // a partial rebuild remains searchable
			if next, nextID, err := i.sync(ctx, cursor, cursorID); err != nil {
				slog.Error("sync search index", "err", err)
			} else {
				cursor, cursorID = next, nextID
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (i *Indexer) sync(ctx context.Context, cursor time.Time, cursorID uuid.UUID) (time.Time, uuid.UUID, error) {
	for {
		docs, err := i.source.SearchIndexBatch(ctx, cursor, cursorID, 500)
		if err != nil {
			return cursor, cursorID, err
		}
		if len(docs) == 0 {
			return cursor, cursorID, nil
		}
		if err := i.client.bulk(ctx, docs); err != nil {
			return cursor, cursorID, err
		}
		last := docs[len(docs)-1]
		cursor, cursorID = last.UpdatedAt, last.ID
		if len(docs) < 500 {
			return cursor, cursorID, nil
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
