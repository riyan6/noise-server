package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"noise-server/internal/model"
	"noise-server/internal/repository"

	driver "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ db *sql.DB }

var _ repository.Store = (*Store)(nil)

func Open(path string) (*Store, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(absolute)
	if filepath.VolumeName(absolute) != "" {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath, RawQuery: "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	fail := func(e error) (*Store, error) { db.Close(); return nil, e }
	if _, err = db.Exec("PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL;"); err != nil {
		return fail(err)
	}
	if _, err = db.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)"); err != nil {
		return fail(err)
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return fail(err)
	}
	for _, file := range files {
		var exists int
		err = db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=?", file.Name()).Scan(&exists)
		if err != nil {
			return fail(err)
		}
		if exists != 0 {
			continue
		}
		contents, e := migrations.ReadFile("migrations/" + file.Name())
		if e != nil {
			return fail(e)
		}
		tx, e := db.Begin()
		if e != nil {
			return fail(e)
		}
		if _, e = tx.Exec(string(contents)); e == nil {
			_, e = tx.Exec("INSERT INTO schema_migrations(version) VALUES(?)", file.Name())
		}
		if e != nil {
			tx.Rollback()
			return fail(e)
		}
		if e = tx.Commit(); e != nil {
			return fail(e)
		}
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func mapped(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return model.ErrNotFound
	}
	var e *driver.Error
	if errors.As(err, &e) && e.Code()&255 == 19 {
		return model.ErrConflict
	}
	return err
}
func encoded(value any) (string, error) { b, e := json.Marshal(value); return string(b), e }
func one[T any](row *sql.Row) (T, error) {
	var value T
	var raw string
	if err := row.Scan(&raw); err != nil {
		return value, mapped(err)
	}
	return value, json.Unmarshal([]byte(raw), &value)
}
func many[T any](db *sql.DB, ctx context.Context, query string, args ...any) ([]T, error) {
	rows, e := db.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	values := []T{}
	for rows.Next() {
		var raw string
		var value T
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &value); e != nil {
			return nil, e
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func changed(result sql.Result, err error) error {
	if err != nil {
		return mapped(err)
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}

func (s *Store) Admin(ctx context.Context) (string, []byte, error) {
	var name string
	var hash []byte
	e := s.db.QueryRowContext(ctx, "SELECT username,password_hash FROM admin WHERE id=1").Scan(&name, &hash)
	return name, hash, mapped(e)
}
func (s *Store) CreateAdmin(ctx context.Context, name string, hash []byte) error {
	_, e := s.db.ExecContext(ctx, "INSERT INTO admin(id,username,password_hash) VALUES(1,?,?)", name, hash)
	return mapped(e)
}
func (s *Store) PutSession(ctx context.Context, hash string, expires int64) error {
	_, e := s.db.ExecContext(ctx, "INSERT INTO sessions(token_hash,expires_at) VALUES(?,?)", hash, expires)
	return e
}
func (s *Store) HasSession(ctx context.Context, hash string, now int64) (bool, error) {
	var n int
	e := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE token_hash=? AND expires_at>?", hash, now).Scan(&n)
	return n == 1, e
}
func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	_, e := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=?", hash)
	return e
}

func scanNode(row interface{ Scan(...any) error }) (model.Node, error) {
	var n model.Node
	var raw string
	err := row.Scan(&raw, &n.Token, &n.BillingAnchor)
	if err != nil {
		return n, mapped(err)
	}
	return n, json.Unmarshal([]byte(raw), &n)
}
func (s *Store) Nodes(ctx context.Context) ([]model.Node, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT payload,token,billing_anchor FROM nodes ORDER BY created_at,id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	nodes := []model.Node{}
	for rows.Next() {
		n, e := scanNode(rows)
		if e != nil {
			return nil, e
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}
func (s *Store) Node(ctx context.Context, id string) (model.Node, error) {
	return scanNode(s.db.QueryRowContext(ctx, "SELECT payload,token,billing_anchor FROM nodes WHERE id=?", id))
}
func (s *Store) NodeByToken(ctx context.Context, token string) (model.Node, error) {
	return scanNode(s.db.QueryRowContext(ctx, "SELECT payload,token,billing_anchor FROM nodes WHERE token=?", token))
}
func (s *Store) SaveNode(ctx context.Context, n model.Node) error {
	raw, e := encoded(n)
	if e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, "INSERT INTO nodes(id,token,payload,billing_anchor,created_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,billing_anchor=excluded.billing_anchor", n.ID, n.Token, raw, n.BillingAnchor, n.CreatedAt)
	if e != nil {
		return mapped(e)
	}
	for table, value := range map[string]string{"brands": n.Brand, "groups": n.Group} {
		if value != "" {
			if _, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO "+table+"(name) VALUES(?)", value); e != nil {
				return e
			}
		}
	}
	return tx.Commit()
}
func (s *Store) DeleteNode(ctx context.Context, id string) error {
	return changed(s.db.ExecContext(ctx, "DELETE FROM nodes WHERE id=?", id))
}
func (s *Store) Labels(ctx context.Context, kind string) ([]string, error) {
	if kind != "brands" && kind != "groups" {
		return nil, model.ValidationError("invalid label kind")
	}
	rows, e := s.db.QueryContext(ctx, "SELECT name FROM "+kind+" ORDER BY name COLLATE NOCASE")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var v string
		if e = rows.Scan(&v); e != nil {
			return nil, e
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (s *Store) Tasks(ctx context.Context) ([]model.LatencyTask, error) {
	tasks, e := many[model.LatencyTask](s.db, ctx, "SELECT payload FROM latency_tasks ORDER BY created_at,id")
	if e != nil {
		return nil, e
	}
	byID := map[string]int{}
	for i := range tasks {
		tasks[i].NodeIDs = []string{}
		byID[tasks[i].ID] = i
	}
	rows, e := s.db.QueryContext(ctx, "SELECT task_id,node_id FROM node_tasks ORDER BY node_id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var task, node string
		if e = rows.Scan(&task, &node); e != nil {
			return nil, e
		}
		tasks[byID[task]].NodeIDs = append(tasks[byID[task]].NodeIDs, node)
	}
	return tasks, rows.Err()
}
func (s *Store) SaveTask(ctx context.Context, t model.LatencyTask) error {
	nodeIDs := t.NodeIDs
	t.NodeIDs = nil
	raw, e := encoded(t)
	if e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "INSERT INTO latency_tasks(id,payload,created_at) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", t.ID, raw, t.CreatedAt); e != nil {
		return mapped(e)
	}
	rows, e := tx.QueryContext(ctx, "SELECT node_id FROM node_tasks WHERE task_id=?", t.ID)
	if e != nil {
		return e
	}
	existing := map[string]bool{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		existing[id] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	wanted := map[string]bool{}
	for _, id := range nodeIDs {
		wanted[id] = true
	}
	for id := range existing {
		if !wanted[id] {
			if _, e = tx.ExecContext(ctx, "DELETE FROM node_tasks WHERE node_id=? AND task_id=?", id, t.ID); e != nil {
				return e
			}
		}
	}
	for _, id := range nodeIDs {
		if !existing[id] {
			if _, e = tx.ExecContext(ctx, "INSERT INTO node_tasks(node_id,task_id,position) SELECT ?,?,COALESCE(MAX(position),-1)+1 FROM node_tasks WHERE node_id=?", id, t.ID, id); e != nil {
				return mapped(e)
			}
		}
	}
	return tx.Commit()
}
func (s *Store) DeleteTask(ctx context.Context, id string) error {
	return changed(s.db.ExecContext(ctx, "DELETE FROM latency_tasks WHERE id=?", id))
}
func (s *Store) NodeTasks(ctx context.Context, id string) ([]model.LatencyTask, error) {
	return many[model.LatencyTask](s.db, ctx, "SELECT t.payload FROM latency_tasks t JOIN node_tasks b ON b.task_id=t.id WHERE b.node_id=? ORDER BY b.position,t.id", id)
}
func (s *Store) AssignTasks(ctx context.Context, node string, ids []string) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var exists int
	if e = tx.QueryRowContext(ctx, "SELECT 1 FROM nodes WHERE id=?", node).Scan(&exists); e != nil {
		return mapped(e)
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM node_tasks WHERE node_id=?", node); e != nil {
		return e
	}
	for i, id := range ids {
		if _, e = tx.ExecContext(ctx, "INSERT INTO node_tasks(node_id,task_id,position) VALUES(?,?,?)", node, id, i); e != nil {
			return mapped(e)
		}
	}
	return tx.Commit()
}
func (s *Store) Channels(ctx context.Context) ([]model.Channel, error) {
	return many[model.Channel](s.db, ctx, "SELECT payload FROM channels ORDER BY created_at,id")
}
func (s *Store) SaveChannel(ctx context.Context, c model.Channel) error {
	raw, e := encoded(c)
	if e != nil {
		return e
	}
	_, e = s.db.ExecContext(ctx, "INSERT INTO channels(id,payload,created_at) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", c.ID, raw, c.CreatedAt)
	return mapped(e)
}
func (s *Store) DeleteChannel(ctx context.Context, id string) error {
	return changed(s.db.ExecContext(ctx, "DELETE FROM channels WHERE id=?", id))
}
func (s *Store) Settings(ctx context.Context) (model.Settings, error) {
	return one[model.Settings](s.db.QueryRowContext(ctx, "SELECT payload FROM settings WHERE id=1"))
}
func (s *Store) SaveSettings(ctx context.Context, v model.Settings) error {
	raw, e := encoded(v)
	if e != nil {
		return e
	}
	_, e = s.db.ExecContext(ctx, "INSERT INTO settings(id,payload) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", raw)
	return e
}
func (s *Store) Runtimes(ctx context.Context) (map[string]model.Runtime, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT node_id,payload FROM runtimes")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := map[string]model.Runtime{}
	for rows.Next() {
		var id, raw string
		var runtime model.Runtime
		if e = rows.Scan(&id, &raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &runtime); e != nil {
			return nil, e
		}
		result[id] = runtime
	}
	return result, rows.Err()
}

func (s *Store) SaveReport(ctx context.Context, node string, report model.Report, now int64, period string) (model.Runtime, bool, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return model.Runtime{}, false, e
	}
	defer tx.Rollback()
	var previous model.Runtime
	var raw string
	e = tx.QueryRowContext(ctx, "SELECT payload FROM runtimes WHERE node_id=?", node).Scan(&raw)
	hasPrevious := e == nil
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return previous, false, e
	}
	if hasPrevious {
		if e = json.Unmarshal([]byte(raw), &previous); e != nil {
			return previous, false, e
		}
	}
	if hasPrevious && report.BootID == previous.Report.BootID {
		if report.Sequence == previous.Report.Sequence {
			return previous, false, nil
		}
		if report.Sequence < previous.Report.Sequence {
			return previous, false, model.ErrConflict
		}
	} else {
		var seen int
		if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM seen_boots WHERE node_id=? AND boot_id=?", node, report.BootID).Scan(&seen); e != nil {
			return previous, false, e
		}
		if seen != 0 {
			return previous, false, model.ErrConflict
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO seen_boots(node_id,boot_id) VALUES(?,?)", node, report.BootID); e != nil {
			return previous, false, mapped(e)
		}
	}
	var up, down int64
	if hasPrevious {
		up = report.TotalUp - previous.Report.TotalUp
		down = report.TotalDown - previous.Report.TotalDown
		if up < 0 {
			up = report.TotalUp
		}
		if down < 0 {
			down = report.TotalDown
		}
		if report.Uptime < previous.Report.Uptime {
			up, down = report.TotalUp, report.TotalDown
		}
	}
	// Overflow would otherwise silently wrap a lifetime counter or SQLite's integer sum.
	const max = int64(^uint64(0) >> 1)
	if up > max-previous.TotalUp || down > max-previous.TotalDown || previous.TotalUp+up > max-(previous.TotalDown+down) {
		return previous, false, model.ValidationError("traffic counters exceed supported range")
	}
	current := model.Runtime{Report: report, ReceivedAt: now, TotalUp: previous.TotalUp + up, TotalDown: previous.TotalDown + down}
	encodedRuntime, e := encoded(current)
	if e != nil {
		return previous, false, e
	}
	encodedReport, e := encoded(report)
	if e != nil {
		return previous, false, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO runtimes(node_id,payload) VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET payload=excluded.payload", node, encodedRuntime); e != nil {
		return previous, false, mapped(e)
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO reports(node_id,time,payload) VALUES(?,?,?)", node, now, encodedReport); e != nil {
		return previous, false, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO traffic_periods(node_id,period,upload,download) VALUES(?,?,?,?) ON CONFLICT(node_id,period) DO UPDATE SET upload=upload+excluded.upload,download=download+excluded.download", node, period, up, down); e != nil {
		return previous, false, e
	}
	if e = tx.Commit(); e != nil {
		return previous, false, e
	}
	return current, true, nil
}
func (s *Store) SaveLatency(ctx context.Context, node string, results []model.LatencyResult) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, r := range results {
		var assigned int
		if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM node_tasks WHERE node_id=? AND task_id=?", node, r.TaskID).Scan(&assigned); e != nil {
			return e
		}
		if assigned == 0 {
			return model.ValidationError("latency task is not assigned to this node")
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO latency_results(node_id,task_id,run_id,time,status,value) VALUES(?,?,?,?,?,?) ON CONFLICT(node_id,task_id,run_id) DO NOTHING", node, r.TaskID, r.RunID, r.Time, r.Status, r.Value); e != nil {
			return mapped(e)
		}
	}
	return tx.Commit()
}
func scanLatency(row interface{ Scan(...any) error }) (model.LatencyResult, error) {
	var r model.LatencyResult
	var value sql.NullFloat64
	e := row.Scan(&r.TaskID, &r.RunID, &r.Time, &r.Status, &value)
	if value.Valid {
		r.Value = &value.Float64
	}
	return r, mapped(e)
}
func (s *Store) LatestLatency(ctx context.Context, node, task string) (*model.LatencyResult, error) {
	r, e := scanLatency(s.db.QueryRowContext(ctx, "SELECT task_id,run_id,time,status,value FROM latency_results WHERE node_id=? AND task_id=? ORDER BY time DESC,run_id DESC LIMIT 1", node, task))
	if errors.Is(e, model.ErrNotFound) {
		return nil, nil
	}
	return &r, e
}
func (s *Store) Latency(ctx context.Context, node, task string, from, to, step int64) ([]model.LatencyResult, error) {
	// Include the point immediately preceding the window so RTT samples can be carried forward.
	rows, e := s.db.QueryContext(ctx, `WITH ranked AS (SELECT task_id,run_id,time,status,value,ROW_NUMBER() OVER(PARTITION BY CAST(time / ? AS INTEGER) ORDER BY time DESC,run_id DESC) AS rank FROM latency_results WHERE node_id=? AND task_id=? AND time>=COALESCE((SELECT MAX(time) FROM latency_results WHERE node_id=? AND task_id=? AND time<?),?) AND time<=?) SELECT task_id,run_id,time,status,value FROM ranked WHERE rank=1 ORDER BY time`, step, node, task, node, task, from, from, to)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	results := []model.LatencyResult{}
	for rows.Next() {
		r, e := scanLatency(rows)
		if e != nil {
			return nil, e
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
func (s *Store) History(ctx context.Context, node string, from, to, step int64) ([]model.Sample, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT MAX(time),AVG(json_extract(payload,'$.cpu')),AVG(json_extract(payload,'$.mem')),AVG(json_extract(payload,'$.up')),AVG(json_extract(payload,'$.down')) FROM reports WHERE node_id=? AND time>=? AND time<=? GROUP BY CAST(time / ? AS INTEGER) ORDER BY MAX(time)`, node, from, to, step)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []model.Sample{}
	for rows.Next() {
		var p model.Sample
		var cpu, mem, up, down float64
		if e = rows.Scan(&p.Time, &cpu, &mem, &up, &down); e != nil {
			return nil, e
		}
		p.CPU = &cpu
		p.Mem = &mem
		p.Up = &up
		p.Down = &down
		result = append(result, p)
	}
	return result, rows.Err()
}
func (s *Store) PeriodTraffic(ctx context.Context, node, period string) (int64, int64, error) {
	var up, down int64
	e := s.db.QueryRowContext(ctx, "SELECT upload,download FROM traffic_periods WHERE node_id=? AND period=?", node, period).Scan(&up, &down)
	if errors.Is(e, sql.ErrNoRows) {
		e = nil
	}
	return up, down, e
}
func (s *Store) Cleanup(ctx context.Context, cutoff, now int64, oldPeriod string) error {
	// Bounded batches keep deletion transactions short even with a full seven days of metrics.
	for _, table := range []string{"reports", "latency_results"} {
		for {
			result, e := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE rowid IN (SELECT rowid FROM %s WHERE time<? LIMIT 5000)", table, table), cutoff)
			if e != nil {
				return e
			}
			n, e := result.RowsAffected()
			if e != nil {
				return e
			}
			if n < 5000 {
				break
			}
		}
	}
	if _, e := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at<=?", now); e != nil {
		return e
	}
	_, e := s.db.ExecContext(ctx, "DELETE FROM traffic_periods WHERE period<?", oldPeriod)
	return e
}
