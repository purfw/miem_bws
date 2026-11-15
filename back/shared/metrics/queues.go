package metrics

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

type queueQuery struct{ name, sql string }
type queues struct {
	pool                *pgxpool.Pool
	queries             []queueQuery
	items, age, success *prometheus.Desc
}

func newQueues(service string, pool *pgxpool.Pool) *queues {
	labels := prometheus.Labels{"service": service}
	q := &queues{
		pool:    pool,
		items:   prometheus.NewDesc("confhub_queue_items", "Items awaiting processing or manual retry.", []string{"queue", "status"}, labels),
		age:     prometheus.NewDesc("confhub_queue_oldest_age_seconds", "Age of the oldest unfinished item.", []string{"queue", "status"}, labels),
		success: prometheus.NewDesc("confhub_queue_collection_success", "Whether database queue metrics were collected.", nil, labels),
	}
	schema := map[string]string{"identity-svc": "identity_svc", "conference-svc": "conference_svc", "application-svc": "application_svc"}[service]
	if schema != "" {
		q.queries = append(q.queries, queueQuery{"outbox", `SELECT status,count(*),GREATEST(COALESCE(max(extract(epoch FROM now()-created_at)),0),0) FROM ` + schema + `.outbox_events WHERE status IN ('pending','failed') GROUP BY status`})
	}
	if service == "engagement-svc" {
		q.queries = append(q.queries, queueQuery{"notifications", `SELECT status,count(*),GREATEST(COALESCE(max(extract(epoch FROM now()-created_at)),0),0) FROM engagement_svc.notifications WHERE status IN ('pending','sending','failed','dead') GROUP BY status`})
	}
	if service == "application-svc" {
		q.queries = append(q.queries, queueQuery{"storage_deletions", `SELECT status,count(*),GREATEST(COALESCE(max(extract(epoch FROM now()-created_at)),0),0) FROM application_svc.storage_deletions WHERE status IN ('pending','dead') GROUP BY status`})
		q.queries = append(q.queries, queueQuery{"verification_roles", `SELECT 'pending',count(*),GREATEST(COALESCE(max(extract(epoch FROM now()-v.created_at)),0),0) FROM application_svc.verification_role_jobs j JOIN application_svc.organizer_verifications v ON v.id=j.verification_id WHERE j.completed_at IS NULL`})
	}
	failureSchema := map[string]string{"conference-svc": "conference_svc", "application-svc": "application_svc", "engagement-svc": "engagement_svc"}[service]
	if failureSchema != "" {
		q.queries = append(q.queries, queueQuery{"events", `SELECT status,count(*),GREATEST(COALESCE(max(extract(epoch FROM now()-created_at)),0),0) FROM ` + failureSchema + `.event_failures WHERE status IN ('pending','dead') GROUP BY status`})
	}
	return q
}

func (q *queues) Describe(ch chan<- *prometheus.Desc) { ch <- q.items; ch <- q.age; ch <- q.success }

func (q *queues) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	success := 1.0
	for _, query := range q.queries {
		rows, err := q.pool.Query(ctx, query.sql)
		if err != nil {
			success = 0
			break
		}
		for rows.Next() {
			var status string
			var count, age float64
			if err := rows.Scan(&status, &count, &age); err != nil {
				success = 0
				break
			}
			ch <- prometheus.MustNewConstMetric(q.items, prometheus.GaugeValue, count, query.name, status)
			ch <- prometheus.MustNewConstMetric(q.age, prometheus.GaugeValue, age, query.name, status)
		}
		if rows.Err() != nil {
			success = 0
		}
		rows.Close()
		if success == 0 {
			break
		}
	}
	ch <- prometheus.MustNewConstMetric(q.success, prometheus.GaugeValue, success)
}
