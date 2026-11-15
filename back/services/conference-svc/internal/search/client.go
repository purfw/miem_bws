package search

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var ErrUnavailable = errors.New("search unavailable")

type Bucket struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type Filter struct {
	Query    string
	Topics   []string
	Statuses []string
	Formats  []string
	Cities   []string
	FromDate *time.Time
	ToDate   *time.Time
	Sort     string
	Page     int
	PerPage  int
}

type Result struct {
	IDs     []uuid.UUID `json:"ids"`
	Total   int         `json:"total"`
	Topics  []Bucket    `json:"topics"`
	Formats []Bucket    `json:"formats"`
	Cities  []Bucket    `json:"cities"`
}

type Suggestion struct {
	Text string `json:"text"`
	Slug string `json:"slug"`
}

type Client struct {
	endpoint string
	index    string
	http     *http.Client
	cache    *redis.Client
	cacheTTL time.Duration
	ready    atomic.Bool
}

func NewClient(endpoint, index string, cache *redis.Client, cacheTTL time.Duration) *Client {
	if index == "" {
		index = "confhub-conferences-v1"
	}
	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"), index: index, cache: cache, cacheTTL: cacheTTL,
		http: &http.Client{Timeout: 4 * time.Second},
	}
}

func (c *Client) setReady(v bool) { c.ready.Store(v) }

func (c *Client) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

func (c *Client) Search(ctx context.Context, f Filter) (Result, error) {
	if !c.ready.Load() {
		return Result{}, ErrUnavailable
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PerPage < 1 || f.PerPage > 100 {
		f.PerPage = 20
	}
	payload := buildSearchPayload(f)
	key := cacheKey("sr", payload)
	if c.cache != nil {
		if raw, err := c.cache.Get(ctx, key).Bytes(); err == nil {
			var cached Result
			if json.Unmarshal(raw, &cached) == nil {
				return cached, nil
			}
		}
	}
	resp, err := c.do(ctx, http.MethodPost, "/"+c.index+"/_search", payload)
	if err != nil {
		return Result{}, errors.Join(ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, elasticError(resp)
	}
	var decoded struct {
		Hits struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
			Hits []struct {
				ID string `json:"_id"`
			} `json:"hits"`
		} `json:"hits"`
		Aggregations struct {
			Topics  aggregation `json:"topics"`
			Formats aggregation `json:"formats"`
			Cities  aggregation `json:"cities"`
		} `json:"aggregations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Result{}, err
	}
	result := Result{Total: decoded.Hits.Total.Value}
	for _, hit := range decoded.Hits.Hits {
		if id, err := uuid.Parse(hit.ID); err == nil {
			result.IDs = append(result.IDs, id)
		}
	}
	result.Topics = decoded.Aggregations.Topics.values()
	result.Formats = decoded.Aggregations.Formats.values()
	result.Cities = decoded.Aggregations.Cities.values()
	if c.cache != nil {
		if raw, err := json.Marshal(result); err == nil {
			_ = c.cache.Set(ctx, key, raw, c.cacheTTL).Err()
		}
	}
	return result, nil
}

type aggregation struct {
	Buckets []struct {
		Key      string `json:"key"`
		DocCount int    `json:"doc_count"`
	} `json:"buckets"`
}

func (a aggregation) values() []Bucket {
	out := make([]Bucket, 0, len(a.Buckets))
	for _, b := range a.Buckets {
		out = append(out, Bucket{Value: b.Key, Count: b.DocCount})
	}
	return out
}

func (c *Client) Autocomplete(ctx context.Context, query string, limit int) ([]Suggestion, error) {
	if !c.ready.Load() {
		return nil, ErrUnavailable
	}
	if limit < 1 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	payload, _ := json.Marshal(map[string]any{
		"size": limit, "_source": []string{"title", "title_en", "slug"},
		"query": map[string]any{"bool": map[string]any{
			"filter": []any{
				map[string]any{"term": map[string]any{"publication_allowed": true}},
				map[string]any{"bool": map[string]any{"must_not": []any{
					map[string]any{"terms": map[string]any{"status": []string{"draft", "cancelled"}}},
				}}},
			},
			"should": []any{
				map[string]any{"multi_match": map[string]any{
					"query": query, "type": "bool_prefix", "fields": []string{
						"title.suggest", "title.suggest._2gram", "title.suggest._3gram",
						"title_en.suggest", "title_en.suggest._2gram", "title_en.suggest._3gram",
					},
				}},
				map[string]any{"multi_match": map[string]any{
					"query": query, "fields": []string{"title^3", "title_en^3"}, "fuzziness": "AUTO",
				}},
			},
			"minimum_should_match": 1,
		}},
	})
	key := cacheKey("ac", payload)
	if c.cache != nil {
		if raw, err := c.cache.Get(ctx, key).Bytes(); err == nil {
			var cached []Suggestion
			if json.Unmarshal(raw, &cached) == nil {
				return cached, nil
			}
		}
	}
	resp, err := c.do(ctx, http.MethodPost, "/"+c.index+"/_search", payload)
	if err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, elasticError(resp)
	}
	var decoded struct {
		Hits struct {
			Hits []struct {
				Source struct {
					Title   string  `json:"title"`
					TitleEn *string `json:"title_en"`
					Slug    string  `json:"slug"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	items := make([]Suggestion, 0, len(decoded.Hits.Hits))
	for _, hit := range decoded.Hits.Hits {
		text := hit.Source.Title
		if strings.TrimSpace(text) == "" && hit.Source.TitleEn != nil {
			text = *hit.Source.TitleEn
		}
		items = append(items, Suggestion{Text: text, Slug: hit.Source.Slug})
	}
	if c.cache != nil {
		if raw, err := json.Marshal(items); err == nil {
			_ = c.cache.Set(ctx, key, raw, c.cacheTTL).Err()
		}
	}
	return items, nil
}

func cacheKey(prefix string, payload []byte) string {
	sum := sha256.Sum256(payload)
	return "search:" + prefix + ":" + hex.EncodeToString(sum[:])
}

func elasticError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return errors.Join(ErrUnavailable, fmt.Errorf("elasticsearch returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
}
