// Package events buffers incoming events and writes them to the database in
// batches, so the request path never touches the database.
package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/philippdormann/umago/server/internal/database"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/philippdormann/umago/server/internal/ids"
)

// Kinds.
const (
	KindPageview = "pageview"
	KindEvent    = "event"
	KindIdentify = "identify"
	KindVitals   = "vitals"
	KindLink     = "link"
	KindPixel    = "pixel"
)

// VisitTimeout is the idle gap after which a visitor starts a new visit.
const VisitTimeout = 30 * time.Minute

// Vitals are optional Web Vitals metrics; a nil pointer means not measured.
type Vitals struct {
	LCP  *float64
	INP  *float64
	CLS  *float64
	FCP  *float64
	TTFB *float64
}

// Event is one enriched, anonymous hit.
type Event struct {
	SiteID   string
	At       time.Time
	Kind     string
	Name     string
	Path     string
	RefHost  string
	Country  string
	Device   string
	Browser  string
	OS       string
	Region   string
	UTMSrc   string
	UTMMed   string
	UTMCamp  string
	UTMCont  string
	UTMTerm  string
	Gclid    string
	Fbclid   string
	Msclkid  string
	Ttclid   string
	LiFatID  string
	Twclid   string
	Channel  string
	Language string
	Title    string
	Hostname string
	Visitor  string
	VisitID  string
	Tag      string
	// DistinctID is the site's own logged-in user id, if it identifies.
	DistinctID string
	Vitals     *Vitals
	// Props are custom event properties, stored as event_data rows.
	Props map[string]any
}

// VisitorHash derives the day-scoped anonymous visitor id. The IP and user
// agent go in, only a truncated hash comes out, and the salt changes daily.
func VisitorHash(salt, siteID, ip, ua string) string {
	sum := sha256.Sum256([]byte(salt + "|" + siteID + "|" + ip + "|" + ua))
	return hex.EncodeToString(sum[:8])
}

const (
	bufferSize    = 4096
	flushEvery    = time.Second
	flushAtCount  = 200
	dropLogPeriod = time.Minute
)

// Writer batches events into the database.
type Writer struct {
	db  *database.DB
	log *slog.Logger
	ch  chan Event
	wg  sync.WaitGroup

	dropped  atomic.Int64
	lastDrop atomic.Int64 // unix seconds
	Written  atomic.Int64
	flushReq chan chan error
	running  atomic.Bool
}

// NewWriter returns a Writer; call Start to begin flushing.
func NewWriter(db *database.DB, log *slog.Logger) *Writer {
	return &Writer{db: db, log: log, ch: make(chan Event, bufferSize), flushReq: make(chan chan error)}
}

// Enqueue adds an event without blocking. When the buffer is full the event
// is dropped and counted.
func (w *Writer) Enqueue(e Event) {
	select {
	case w.ch <- e:
	default:
		n := w.dropped.Add(1)
		now := time.Now().Unix()
		if last := w.lastDrop.Load(); now-last >= int64(dropLogPeriod.Seconds()) && w.lastDrop.CompareAndSwap(last, now) {
			w.log.Warn("events.dropped", "total", n)
		}
	}
}

// Start runs the flush loop until ctx is cancelled, then drains what is left.
func (w *Writer) Start(ctx context.Context) {
	w.wg.Add(1)
	w.running.Store(true)
	go func() {
		defer w.wg.Done()
		defer w.running.Store(false)
		batch := make([]Event, 0, flushAtCount)
		t := time.NewTicker(flushEvery)
		defer t.Stop()
		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			err := w.write(batch)
			if err != nil {
				w.log.Error("events.write_failed", "count", len(batch), "error", err.Error())
			}
			batch = batch[:0]
			return err
		}
		for {
			select {
			case reply := <-w.flushReq:
				// Drain the channel into the batch too, then write everything.
				for drained := false; !drained; {
					select {
					case e := <-w.ch:
						batch = append(batch, e)
					default:
						drained = true
					}
				}
				reply <- flush()
			case <-ctx.Done():
				// Drain anything still queued.
				for {
					select {
					case e := <-w.ch:
						batch = append(batch, e)
						if len(batch) >= flushAtCount {
							flush()
						}
					default:
						flush()
						return
					}
				}
			case e := <-w.ch:
				batch = append(batch, e)
				if len(batch) >= flushAtCount {
					flush()
				}
			case <-t.C:
				flush()
			}
		}
	}()
}

// Wait blocks until the flush loop has exited.
func (w *Writer) Wait() { w.wg.Wait() }

// Flush writes everything queued right now, including events the loop has
// already taken into its pending batch. Works with or without Start.
func (w *Writer) Flush() error {
	if w.running.Load() {
		reply := make(chan error, 1)
		select {
		case w.flushReq <- reply:
			return <-reply
		case <-time.After(5 * time.Second):
			return context.DeadlineExceeded
		}
	}
	var batch []Event
	for {
		select {
		case e := <-w.ch:
			batch = append(batch, e)
		default:
			if len(batch) == 0 {
				return nil
			}
			return w.write(batch)
		}
	}
}

func (w *Writer) write(batch []Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO events (site_id, ts, kind, name, path, ref_host, country, device, browser, os, region, utm_source, utm_medium, utm_campaign, utm_content, utm_term, gclid, fbclid, msclkid, ttclid, li_fat_id, twclid, channel, language, title, hostname, tag, visitor, visit_id, distinct_id, lcp, inp, cls, fcp, ttfb)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING id`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	type dataRow struct {
		siteID  string
		eventID int64
		key     string
		sval    string
		nval    *float64
	}
	var data []dataRow
	touch := map[[2]string]string{} // [site, visitor] -> max ts
	for _, e := range batch {
		var rowID int64
		var v *Vitals
		if e.Vitals != nil {
			v = e.Vitals
		}
		var lcp, inp, cls, fcp, ttfb any
		if v != nil {
			lcp, inp, cls, fcp, ttfb = v.LCP, v.INP, v.CLS, v.FCP, v.TTFB
		}
		if err := stmt.QueryRowContext(ctx, e.SiteID, ids.Format(e.At), e.Kind, e.Name, e.Path, e.RefHost, e.Country, e.Device, e.Browser, e.OS, e.Region,
			e.UTMSrc, e.UTMMed, e.UTMCamp, e.UTMCont, e.UTMTerm, e.Gclid, e.Fbclid, e.Msclkid, e.Ttclid, e.LiFatID, e.Twclid, e.Channel, e.Language, e.Title, e.Hostname, e.Tag, e.Visitor, e.VisitID, e.DistinctID,
			lcp, inp, cls, fcp, ttfb).Scan(&rowID); err != nil {
			return err
		}
		for _, p := range flattenProps(e.Props) {
			data = append(data, dataRow{siteID: e.SiteID, eventID: rowID, key: p.key, sval: p.sval, nval: p.nval})
		}
		if k := [2]string{e.SiteID, e.Visitor}; ids.Format(e.At) > touch[k] {
			touch[k] = ids.Format(e.At)
		}
	}
	if len(data) > 0 {
		dstmt, err := tx.PrepareContext(ctx, `INSERT INTO event_data (id, site_id, event_id, key, sval, nval) VALUES (?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer dstmt.Close()
		for _, d := range data {
			if _, err := dstmt.ExecContext(ctx, ids.New("ed"), d.siteID, d.eventID, d.key, d.sval, d.nval); err != nil {
				return err
			}
		}
	}
	if len(touch) > 0 {
		// Update-only: Visit creates the row; a missing row is recreated on
		// the visitor's next hit.
		for k, ts := range touch {
			if _, err := tx.ExecContext(ctx, `UPDATE visits SET last_at = ? WHERE site_id = ? AND visitor = ? AND last_at < ?`, ts, k[0], k[1], ts); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	w.Written.Add(int64(len(batch)))
	return nil
}

// Dropped returns how many events were discarded because the buffer was full.
func (w *Writer) Dropped() int64 { return w.dropped.Load() }

// Prune deletes raw events older than days, plus orphaned event data and
// stale visit state.
func Prune(ctx context.Context, db *database.DB, days int, now time.Time) (int64, error) {
	cutoff := ids.Format(now.AddDate(0, 0, -days))
	res, err := db.ExecContext(ctx, `DELETE FROM events WHERE ts < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	_, _ = db.ExecContext(ctx, `DELETE FROM event_data WHERE event_id NOT IN (SELECT id FROM events)`)
	_, _ = db.ExecContext(ctx, `DELETE FROM visits WHERE last_at < ?`, cutoff)
	return n, nil
}
