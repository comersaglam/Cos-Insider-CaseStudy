package notification

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification/internal/db"
)

// rejectionRecord is a rejection as batches.rejections keeps it.
type rejectionRecord struct {
	Index  int           `json:"index"`
	Fields []fieldRecord `json:"fields"`
}

type fieldRecord struct {
	Field   string `json:"field"`
	Code    Code   `json:"code"`
	Message string `json:"message"`
}

// CreateBatch stores the batch, its accepted notifications and their outbox rows in one transaction, and reports
// created = true. The notifications and outbox rows are written with COPY, one round trip for up to a thousand
// rows.
//
// When idem carries a key an earlier batch used, nothing is written: CreateBatch returns that batch, with its
// items, and created = false if the fingerprints match, and ErrIdempotencyKeyReused if they differ.
func (s *Store) CreateBatch(ctx context.Context, b NewBatch, idem IdempotencyKey) (stored Batch, created bool, err error) {
	rejections, err := encodeRejections(b.Rejections)
	if err != nil {
		return Batch{}, false, err
	}
	params := db.InsertBatchParams{
		ID: b.ID, Total: b.Total, Accepted: len(b.Items), Rejections: rejections, CorrelationID: b.CorrelationID,
	}
	if idem.Key != "" {
		params.IdempotencyKey = &idem.Key
		params.RequestHash = idem.Fingerprint[:]
	}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		row, err := q.InsertBatch(ctx, params)
		if errors.Is(err, pgx.ErrNoRows) {
			// The key is taken by a committed batch.
			existing, err := q.GetBatchByIdempotencyKey(ctx, &idem.Key)
			if err != nil {
				return fmt.Errorf("read the batch holding the idempotency key: %w", err)
			}
			if !bytes.Equal(existing.RequestHash, params.RequestHash) {
				return ErrIdempotencyKeyReused
			}
			if stored, err = batchFromRow(existing); err != nil {
				return err
			}
			items, err := q.ListBatchItems(ctx, &existing.ID)
			if err != nil {
				return fmt.Errorf("read the batch's notifications: %w", err)
			}
			for _, item := range items {
				stored.Items = append(stored.Items, BatchItem{Index: *item.BatchIndex, ID: item.ID})
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("insert batch: %w", err)
		}

		notifications := make([]db.CopyBatchNotificationsParams, len(b.Items))
		var outbox []uuid.UUID
		for i, item := range b.Items {
			n, index := item.Notification, item.Index
			notifications[i] = db.CopyBatchNotificationsParams{
				ID: n.ID, Channel: string(n.Channel), Recipient: n.To, Content: n.Content, Priority: string(n.Priority),
				Status: string(n.Status), CorrelationID: n.CorrelationID, BatchID: &row.ID, BatchIndex: &index,
				ScheduledAt: n.ScheduledAt, NextAttemptAt: dueAt(n),
			}
			if n.Status == StatusQueued {
				outbox = append(outbox, n.ID)
			}
		}
		if _, err := q.CopyBatchNotifications(ctx, notifications); err != nil {
			return fmt.Errorf("copy the batch's notifications: %w", err)
		}
		if _, err := q.CopyOutbox(ctx, outbox); err != nil {
			return fmt.Errorf("copy the batch's outbox rows: %w", err)
		}

		if stored, err = batchFromRow(row); err != nil {
			return err
		}
		for _, item := range b.Items {
			stored.Items = append(stored.Items, BatchItem{Index: item.Index, ID: item.Notification.ID})
		}
		created = true
		return nil
	})
	return stored, created, err
}

// GetBatch returns the batch with the given ID and its notifications counted by status now, or ErrNotFound.
func (s *Store) GetBatch(ctx context.Context, id uuid.UUID) (Batch, error) {
	q := db.New(s.pool)
	row, err := q.GetBatch(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Batch{}, ErrNotFound
	}
	if err != nil {
		return Batch{}, fmt.Errorf("read batch: %w", err)
	}
	b, err := batchFromRow(row)
	if err != nil {
		return Batch{}, err
	}
	counts, err := q.CountBatchStatuses(ctx, &id)
	if err != nil {
		return Batch{}, fmt.Errorf("count the batch's notifications: %w", err)
	}
	b.Statuses = make(map[Status]int, len(counts))
	for _, c := range counts {
		b.Statuses[Status(c.Status)] = c.Count
	}
	return b, nil
}

func batchFromRow(r db.Batch) (Batch, error) {
	var records []rejectionRecord
	if err := json.Unmarshal(r.Rejections, &records); err != nil {
		return Batch{}, fmt.Errorf("decode the batch's rejections: %w", err)
	}
	b := Batch{
		ID: r.ID, Total: r.Total, Accepted: r.Accepted, CorrelationID: r.CorrelationID, CreatedAt: r.CreatedAt.UTC(),
	}
	for _, rec := range records {
		rej := Rejection{Index: rec.Index}
		for _, f := range rec.Fields {
			rej.Fields = append(rej.Fields, FieldError(f))
		}
		b.Rejections = append(b.Rejections, rej)
	}
	return b, nil
}

func encodeRejections(rejections []Rejection) ([]byte, error) {
	// Never nil: the column holds an array, and its length must add up with the counts.
	records := make([]rejectionRecord, 0, len(rejections))
	for _, r := range rejections {
		rec := rejectionRecord{Index: r.Index}
		for _, f := range r.Fields {
			rec.Fields = append(rec.Fields, fieldRecord(f))
		}
		records = append(records, rec)
	}
	out, err := json.Marshal(records)
	if err != nil {
		return nil, fmt.Errorf("encode the batch's rejections: %w", err)
	}
	return out, nil
}
