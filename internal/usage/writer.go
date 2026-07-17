package usage

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type Event struct {
	RequestID     string
	ClientKeyID   int64
	Protocol      string
	EndpointKind  string
	PublicModelID string
	ProviderID    string
	UpstreamID    int64
	AccountID     int64
	StatusCode    int
	ErrorCode     string
	Streamed      bool
	StartedAtMS   int64
	FirstByteAtMS *int64
	CompletedAtMS int64
	RequestBytes  int64
	ResponseBytes int64
	UsageJSON     string
	RoutingJSON   string
}

type Writer struct {
	db     *sql.DB
	queue  chan Event
	stop   chan struct{}
	done   chan struct{}
	closed atomic.Bool
	once   sync.Once

	errMu sync.Mutex
	err   error
}

func NewWriter(db *sql.DB) *Writer {
	writer := &Writer{
		db: db, queue: make(chan Event, 1000), stop: make(chan struct{}), done: make(chan struct{}),
	}
	go writer.run()
	return writer
}

func (w *Writer) Enqueue(ctx context.Context, event Event) error {
	if w == nil || w.db == nil {
		return errors.New("usage writer is unavailable")
	}
	if w.closed.Load() {
		return errors.New("usage writer is closed")
	}
	select {
	case w.queue <- event:
		return nil
	default:
		return w.writeBatch(ctx, []Event{event})
	}
}

func (w *Writer) Close(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.once.Do(func() {
		w.closed.Store(true)
		close(w.stop)
	})
	select {
	case <-w.done:
		w.errMu.Lock()
		defer w.errMu.Unlock()
		return w.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Writer) run() {
	defer close(w.done)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	batch := make([]Event, 0, 100)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := w.writeBatch(ctx, batch)
		cancel()
		if err != nil {
			w.setError(err)
			return
		}
		batch = batch[:0]
	}
	for {
		select {
		case event := <-w.queue:
			batch = append(batch, event)
			if len(batch) >= 100 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-w.stop:
			for {
				select {
				case event := <-w.queue:
					batch = append(batch, event)
				default:
					flush()
					return
				}
			}
		}
	}
}

func (w *Writer) writeBatch(ctx context.Context, events []Event) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statement, err := tx.PrepareContext(ctx, `INSERT INTO request_records(
		public_id, client_key_id, protocol, endpoint_kind, public_model_id,
		provider_id, upstream_id, account_id, status_code, error_code, streamed,
		started_at_ms, first_byte_at_ms, completed_at_ms, request_bytes, response_bytes,
		usage_json, routing_json
	) VALUES(?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, 0), NULLIF(?, 0), ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(public_id) DO NOTHING`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, event := range events {
		if _, err := statement.ExecContext(ctx,
			event.RequestID, event.ClientKeyID, event.Protocol, event.EndpointKind, event.PublicModelID,
			event.ProviderID, event.UpstreamID, event.AccountID, event.StatusCode, event.ErrorCode, boolInt(event.Streamed),
			event.StartedAtMS, event.FirstByteAtMS, event.CompletedAtMS, event.RequestBytes, event.ResponseBytes,
			event.UsageJSON, event.RoutingJSON,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (w *Writer) setError(err error) {
	w.errMu.Lock()
	defer w.errMu.Unlock()
	w.err = errors.Join(w.err, err)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
