// Package store persists samples, disk scans and events in a single
// SQLite file. It is the only package that talks to the database.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/ricardotech/minipulse/internal/model"
)

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS samples (
  ts INTEGER PRIMARY KEY,
  cpu_pct REAL, mem_used INTEGER, mem_total INTEGER, mem_pct REAL,
  swap_used INTEGER, load1 REAL,
  net_rx_bps REAL, net_tx_bps REAL, disk_read_bps REAL, disk_write_bps REAL,
  wh_total REAL,
  sys_w REAL, soc_w REAL, cpu_w REAL, gpu_w REAL, ane_w REAL,
  cpu_temp_c REAL, gpu_temp_c REAL, fan_rpm INTEGER
);
CREATE TABLE IF NOT EXISTS volume_samples (
  ts INTEGER, mount TEXT, used INTEGER, total INTEGER,
  PRIMARY KEY (ts, mount)
);
CREATE TABLE IF NOT EXISTS disk_scans (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER, root TEXT, duration_ms INTEGER,
  total_bytes INTEGER, file_count INTEGER, err_count INTEGER
);
CREATE INDEX IF NOT EXISTS idx_scans_root_ts ON disk_scans(root, ts);
CREATE TABLE IF NOT EXISTS disk_entries (
  scan_id INTEGER, path TEXT, parent TEXT, name TEXT,
  is_dir INTEGER, size INTEGER,
  PRIMARY KEY (scan_id, path)
);
CREATE INDEX IF NOT EXISTS idx_entries_parent ON disk_entries(scan_id, parent, size DESC);
CREATE INDEX IF NOT EXISTS idx_entries_size ON disk_entries(scan_id, size DESC);
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER, kind TEXT, title TEXT, detail TEXT
);
CREATE TABLE IF NOT EXISTS process_samples (
  ts INTEGER, pid INTEGER, name TEXT,
  cpu_pct REAL, mem_pct REAL, rss INTEGER,
  PRIMARY KEY (ts, pid)
);
CREATE INDEX IF NOT EXISTS idx_ps_name_ts ON process_samples(name, ts);
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);
`

// Open creates (or opens) the database inside dir. The directory is
// owner-only: the history contains process names and your file
// paths, which are not other local users' business.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(dir, 0o700) // tighten dirs created by older versions
	path := filepath.Join(dir, "minipulse.db")
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: one writer, no lock contention
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// InsertSample stores one sample and its volume rows.
func (s *Store) InsertSample(sm model.Sample) error {
	ts := sm.Time.Unix()
	_, err := s.db.Exec(`INSERT OR REPLACE INTO samples VALUES
		(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ts, sm.CPUPct, sm.MemUsed, sm.MemTotal, sm.MemPct, sm.SwapUsed, sm.Load1,
		sm.NetRxBps, sm.NetTxBps, sm.DiskReadBps, sm.DiskWritBps, sm.WhTotal,
		sm.Power.SysW, sm.Power.SoCW, sm.Power.CPUW, sm.Power.GPUW, sm.Power.ANEW,
		sm.Power.CPUTempC, sm.Power.GPUTempC, sm.Power.FanRPM)
	if err != nil {
		return err
	}
	for _, v := range sm.Volumes {
		if _, err := s.db.Exec(`INSERT OR REPLACE INTO volume_samples VALUES (?,?,?,?)`,
			ts, v.Mount, v.Used, v.Total); err != nil {
			return err
		}
	}
	return nil
}

// LastWhTotal returns the cumulative watt-hours from the newest sample,
// so the counter survives restarts.
func (s *Store) LastWhTotal() (float64, error) {
	var wh sql.NullFloat64
	err := s.db.QueryRow(`SELECT wh_total FROM samples ORDER BY ts DESC LIMIT 1`).Scan(&wh)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return wh.Float64, err
}

// Point is one bucketed history row returned to the charts.
type Point struct {
	Time         time.Time `json:"time"`
	CPUPct       float64   `json:"cpu_pct"`
	MemPct       float64   `json:"mem_pct"`
	SysW         float64   `json:"sys_w"`
	SoCW         float64   `json:"soc_w"`
	CPUTempC     float64   `json:"cpu_temp_c"`
	NetRxBps     float64   `json:"net_rx_bps"`
	NetTxBps     float64   `json:"net_tx_bps"`
	DiskReadBps  float64   `json:"disk_read_bps"`
	DiskWriteBps float64   `json:"disk_write_bps"`
}

// History returns samples in [from,to) averaged into step-second buckets.
func (s *Store) History(from, to time.Time, step int64) ([]Point, error) {
	if step < 1 {
		step = 1
	}
	rows, err := s.db.Query(`
		SELECT (ts/?)*? AS bucket,
			AVG(cpu_pct), AVG(mem_pct), AVG(sys_w), AVG(soc_w), AVG(cpu_temp_c),
			AVG(net_rx_bps), AVG(net_tx_bps), AVG(disk_read_bps), AVG(disk_write_bps)
		FROM samples WHERE ts >= ? AND ts < ?
		GROUP BY bucket ORDER BY bucket`, step, step, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var p Point
		var bucket int64
		if err := rows.Scan(&bucket, &p.CPUPct, &p.MemPct, &p.SysW, &p.SoCW,
			&p.CPUTempC, &p.NetRxBps, &p.NetTxBps, &p.DiskReadBps, &p.DiskWriteBps); err != nil {
			return nil, err
		}
		p.Time = time.Unix(bucket, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}

// Energy is integrated energy use over a window.
type Energy struct {
	Wh      float64 `json:"wh"`
	AvgW    float64 `json:"avg_w"`
	PeakW   float64 `json:"peak_w"`
	Samples int     `json:"samples"`
}

// EnergyBetween integrates the cumulative counter between two times.
func (s *Store) EnergyBetween(from, to time.Time) (Energy, error) {
	var e Energy
	var whStart, whEnd sql.NullFloat64
	err := s.db.QueryRow(`SELECT wh_total FROM samples WHERE ts < ? ORDER BY ts DESC LIMIT 1`, from.Unix()).Scan(&whStart)
	if err != nil && err != sql.ErrNoRows {
		return e, err
	}
	err = s.db.QueryRow(`SELECT wh_total FROM samples WHERE ts < ? ORDER BY ts DESC LIMIT 1`, to.Unix()).Scan(&whEnd)
	if err != nil && err != sql.ErrNoRows {
		return e, err
	}
	if whEnd.Valid {
		e.Wh = whEnd.Float64 - whStart.Float64
		if e.Wh < 0 {
			e.Wh = 0
		}
	}
	err = s.db.QueryRow(`SELECT COALESCE(AVG(sys_w),0), COALESCE(MAX(sys_w),0), COUNT(*)
		FROM samples WHERE ts >= ? AND ts < ?`, from.Unix(), to.Unix()).
		Scan(&e.AvgW, &e.PeakW, &e.Samples)
	return e, err
}

// SaveDiskScan stores one scan and all its entries in a transaction.
func (s *Store) SaveDiskScan(scan model.DiskScan, entries []model.DiskEntry) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO disk_scans (ts,root,duration_ms,total_bytes,file_count,err_count)
		VALUES (?,?,?,?,?,?)`, scan.Time.Unix(), scan.Root, scan.DurationMs,
		scan.TotalBytes, scan.FileCount, scan.ErrCount)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	stmt, err := tx.Prepare(`INSERT INTO disk_entries VALUES (?,?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	for _, e := range entries {
		isDir := 0
		if e.IsDir {
			isDir = 1
		}
		if _, err := stmt.Exec(id, e.Path, e.Parent, e.Name, isDir, e.Size); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// LatestScan returns the newest scan for a root, or (nil,nil) if none.
func (s *Store) LatestScan(root string) (*model.DiskScan, error) {
	return s.scanQuery(`WHERE root=? ORDER BY ts DESC LIMIT 1`, root)
}

// ScanBefore returns the newest scan strictly before t (for growth diffs).
func (s *Store) ScanBefore(root string, t time.Time) (*model.DiskScan, error) {
	return s.scanQuery(`WHERE root=? AND ts<? ORDER BY ts DESC LIMIT 1`, root, t.Unix())
}

// ScannedRoots lists every root that has at least one scan.
func (s *Store) ScannedRoots() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT root FROM disk_scans ORDER BY root`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) scanQuery(where string, args ...any) (*model.DiskScan, error) {
	var sc model.DiskScan
	var ts int64
	err := s.db.QueryRow(`SELECT id,ts,root,duration_ms,total_bytes,file_count,err_count
		FROM disk_scans `+where, args...).
		Scan(&sc.ID, &ts, &sc.Root, &sc.DurationMs, &sc.TotalBytes, &sc.FileCount, &sc.ErrCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sc.Time = time.Unix(ts, 0)
	return &sc, nil
}

// Children returns the direct children of parent in a scan, biggest first.
func (s *Store) Children(scanID int64, parent string, limit int) ([]model.DiskEntry, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(`SELECT path,parent,name,is_dir,size FROM disk_entries
		WHERE scan_id=? AND parent=? ORDER BY size DESC LIMIT ?`, scanID, parent, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

// BiggestFiles returns the largest files in a scan.
func (s *Store) BiggestFiles(scanID int64, limit int) ([]model.DiskEntry, error) {
	rows, err := s.db.Query(`SELECT path,parent,name,is_dir,size FROM disk_entries
		WHERE scan_id=? AND is_dir=0 ORDER BY size DESC LIMIT ?`, scanID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

// Growth diffs two scans path-by-path, biggest absolute change first.
// Done in Go rather than SQL: the entry count per scan is already
// bounded by the size filter, and the code stays obvious.
func (s *Store) Growth(oldID, newID int64, limit int) ([]model.Growth, error) {
	load := func(id int64) (map[string]model.DiskEntry, error) {
		rows, err := s.db.Query(`SELECT path,parent,name,is_dir,size FROM disk_entries WHERE scan_id=?`, id)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		entries, err := scanEntries(rows)
		if err != nil {
			return nil, err
		}
		m := make(map[string]model.DiskEntry, len(entries))
		for _, e := range entries {
			m[e.Path] = e
		}
		return m, nil
	}
	oldM, err := load(oldID)
	if err != nil {
		return nil, err
	}
	newM, err := load(newID)
	if err != nil {
		return nil, err
	}
	var out []model.Growth
	for path, n := range newM {
		was := oldM[path].Size // 0 when the path is new
		if d := n.Size - was; d != 0 {
			out = append(out, model.Growth{Path: path, Name: n.Name, IsDir: n.IsDir, Was: was, Now: n.Size, Delta: d})
		}
	}
	for path, o := range oldM {
		if _, ok := newM[path]; !ok && o.Size != 0 {
			out = append(out, model.Growth{Path: path, Name: o.Name, IsDir: o.IsDir, Was: o.Size, Now: 0, Delta: -o.Size})
		}
	}
	sort.Slice(out, func(i, j int) bool { return abs(out[i].Delta) > abs(out[j].Delta) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func scanEntries(rows *sql.Rows) ([]model.DiskEntry, error) {
	var out []model.DiskEntry
	for rows.Next() {
		var e model.DiskEntry
		var isDir int
		if err := rows.Scan(&e.Path, &e.Parent, &e.Name, &isDir, &e.Size); err != nil {
			return nil, err
		}
		e.IsDir = isDir == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

// AddEvent appends one event to the log.
func (s *Store) AddEvent(kind, title, detail string) error {
	_, err := s.db.Exec(`INSERT INTO events (ts,kind,title,detail) VALUES (?,?,?,?)`,
		time.Now().Unix(), kind, title, detail)
	return err
}

// Events returns the most recent events, newest first.
func (s *Store) Events(limit int) ([]model.Event, error) {
	rows, err := s.db.Query(`SELECT id,ts,kind,title,detail FROM events ORDER BY ts DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Event
	for rows.Next() {
		var e model.Event
		var ts int64
		if err := rows.Scan(&e.ID, &ts, &e.Kind, &e.Title, &e.Detail); err != nil {
			return nil, err
		}
		e.Time = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// InsertProcessSample stores one top-processes snapshot.
func (s *Store) InsertProcessSample(ts time.Time, procs []model.Process) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO process_samples VALUES (?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range procs {
		if _, err := stmt.Exec(ts.Unix(), p.PID, p.Name, p.CPUPct, p.MemPct, p.RSS); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ProcessLeader aggregates one process name over a window.
type ProcessLeader struct {
	Name      string    `json:"name"`
	AvgCPU    float64   `json:"avg_cpu"`
	PeakCPU   float64   `json:"peak_cpu"`
	PeakCPUAt time.Time `json:"peak_cpu_at"`
	AvgRSS    int64     `json:"avg_rss"`
	PeakRSS   int64     `json:"peak_rss"`
	Samples   int       `json:"samples"`
}

// ProcessLeaders ranks process names by average CPU (or peak RSS when
// byMem) over [from,to). Instances sharing a name (Chrome helpers,
// several node processes…) are summed per snapshot first, so a name's
// figures mean "all of Chrome", not "one average helper".
func (s *Store) ProcessLeaders(ctx context.Context, from, to time.Time, byMem bool, limit int) ([]ProcessLeader, error) {
	order := "avg_cpu DESC"
	if byMem {
		order = "peak_rss DESC"
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, AVG(cpu) AS avg_cpu, MAX(cpu), CAST(AVG(rss) AS INTEGER), MAX(rss) AS peak_rss, COUNT(*),
			(SELECT ts FROM (SELECT ts, name, SUM(cpu_pct) AS c FROM process_samples
				WHERE ts>=? AND ts<? GROUP BY ts, name) q
				WHERE q.name=p.name ORDER BY q.c DESC LIMIT 1)
		FROM (SELECT ts, name, SUM(cpu_pct) AS cpu, SUM(rss) AS rss FROM process_samples
			WHERE ts>=? AND ts<? GROUP BY ts, name) p
		GROUP BY name ORDER BY `+order+` LIMIT ?`,
		from.Unix(), to.Unix(), from.Unix(), to.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProcessLeader
	for rows.Next() {
		var l ProcessLeader
		var peakAt int64
		if err := rows.Scan(&l.Name, &l.AvgCPU, &l.PeakCPU, &l.AvgRSS, &l.PeakRSS, &l.Samples, &peakAt); err != nil {
			return nil, err
		}
		l.PeakCPUAt = time.Unix(peakAt, 0)
		out = append(out, l)
	}
	return out, rows.Err()
}

// ProcPoint is one bucketed per-process history point.
type ProcPoint struct {
	Time time.Time `json:"time"`
	CPU  float64   `json:"cpu"`
	RSS  int64     `json:"rss"`
}

// ProcessSeries returns bucketed averages per process name, aligned to
// the same buckets as History so the UI can stack them.
func (s *Store) ProcessSeries(ctx context.Context, from, to time.Time, step int64, names []string) (map[string][]ProcPoint, error) {
	if step < 1 {
		step = 1
	}
	out := map[string][]ProcPoint{}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT (ts/?)*? AS bucket, AVG(cpu), CAST(AVG(rss) AS INTEGER)
			FROM (SELECT ts, SUM(cpu_pct) AS cpu, SUM(rss) AS rss FROM process_samples
				WHERE name=? AND ts>=? AND ts<? GROUP BY ts)
			GROUP BY bucket ORDER BY bucket`, step, step, name, from.Unix(), to.Unix())
		if err != nil {
			return nil, err
		}
		var pts []ProcPoint
		for rows.Next() {
			var p ProcPoint
			var bucket int64
			if err := rows.Scan(&bucket, &p.CPU, &p.RSS); err != nil {
				rows.Close()
				return nil, err
			}
			p.Time = time.Unix(bucket, 0)
			pts = append(pts, p)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		out[name] = pts
	}
	return out, nil
}

// ProcessesAt returns the stored snapshot nearest to (not after) t:
// "what was eating the machine at 14:03?".
func (s *Store) ProcessesAt(ctx context.Context, t time.Time) ([]model.Process, time.Time, error) {
	var ts sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(ts) FROM process_samples WHERE ts<=?`, t.Unix()).Scan(&ts)
	if err != nil {
		return nil, time.Time{}, err
	}
	if !ts.Valid {
		return nil, time.Time{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT pid,name,cpu_pct,mem_pct,rss FROM process_samples
		WHERE ts=? ORDER BY cpu_pct DESC`, ts.Int64)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer rows.Close()
	var out []model.Process
	for rows.Next() {
		var p model.Process
		if err := rows.Scan(&p.PID, &p.Name, &p.CPUPct, &p.MemPct, &p.RSS); err != nil {
			return nil, time.Time{}, err
		}
		out = append(out, p)
	}
	return out, time.Unix(ts.Int64, 0), rows.Err()
}

// Prune deletes samples older than retentionDays, process samples
// older than processRetentionDays, and keeps at most keepScans disk
// scans per root.
func (s *Store) Prune(retentionDays, processRetentionDays, keepScans int) error {
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Unix()
	if _, err := s.db.Exec(`DELETE FROM samples WHERE ts < ?`, cutoff); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM volume_samples WHERE ts < ?`, cutoff); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM events WHERE ts < ?`, cutoff); err != nil {
		return err
	}
	procCutoff := time.Now().AddDate(0, 0, -processRetentionDays).Unix()
	if _, err := s.db.Exec(`DELETE FROM process_samples WHERE ts < ?`, procCutoff); err != nil {
		return err
	}
	_, err := s.db.Exec(`
		DELETE FROM disk_entries WHERE scan_id IN (
			SELECT id FROM disk_scans s WHERE
			(SELECT COUNT(*) FROM disk_scans s2 WHERE s2.root=s.root AND s2.ts>s.ts) >= ?)`, keepScans)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		DELETE FROM disk_scans WHERE id IN (
			SELECT id FROM disk_scans s WHERE
			(SELECT COUNT(*) FROM disk_scans s2 WHERE s2.root=s.root AND s2.ts>s.ts) >= ?)`, keepScans)
	return err
}
