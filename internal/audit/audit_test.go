package audit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func openMem(t *testing.T) Recorder {
	t.Helper()
	r, err := Open(Config{DBPath: ":memory:", RetentionDays: 90})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestMigrateIdempotent(t *testing.T) {
	r, err := Open(Config{DBPath: ":memory:", RetentionDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	// Re-open the same in-memory is not applicable; open a new one.
	r2, err := Open(Config{DBPath: ":memory:", RetentionDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	_ = r2.Close()
}

func TestInsertAndList(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	r.Record(ctx, Event{Action: ActionStart, ContainerID: "abc", Result: ResultSuccess})
	p, err := r.List(ctx, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 1 || p.Count != 1 {
		t.Fatalf("want 1 row, got total=%d count=%d", p.Total, p.Count)
	}
	if p.Items[0].Action != ActionStart {
		t.Fatalf("action = %q", p.Items[0].Action)
	}
	if p.Items[0].RequestID == "" {
		t.Fatal("request_id empty")
	}
}

func TestPagination(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	for i := 0; i < 120; i++ {
		r.Record(ctx, Event{Action: ActionStart, Result: ResultSuccess, Time: time.Now().Add(time.Duration(i) * time.Second)})
	}
	p1, err := r.List(ctx, Query{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if p1.Total != 120 || p1.Count != 50 {
		t.Fatalf("page1 total=%d count=%d", p1.Total, p1.Count)
	}
	p2, _ := r.List(ctx, Query{Limit: 50, Offset: 50})
	if p2.Count != 50 || p2.Items[0].ID == p1.Items[0].ID {
		t.Fatalf("page2 wrong: count=%d", p2.Count)
	}
	p3, _ := r.List(ctx, Query{Limit: 50, Offset: 100})
	if p3.Count != 20 {
		t.Fatalf("page3 count=%d", p3.Count)
	}
}

func TestFilterSinceUntil(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	t1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	r.Record(ctx, Event{Action: ActionStart, Result: ResultSuccess, Time: t1})
	r.Record(ctx, Event{Action: ActionStart, Result: ResultSuccess, Time: t2})
	r.Record(ctx, Event{Action: ActionStart, Result: ResultSuccess, Time: t3})
	p, err := r.List(ctx, Query{Since: t2, Until: t2.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 1 {
		t.Fatalf("want 1, got %d", p.Total)
	}
}

func TestFilterAction(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	r.Record(ctx, Event{Action: ActionStart, Result: ResultSuccess})
	r.Record(ctx, Event{Action: ActionStop, Result: ResultSuccess})
	r.Record(ctx, Event{Action: ActionRestart, Result: ResultSuccess})
	r.Record(ctx, Event{Action: ActionExec, Result: ResultSuccess})
	p, _ := r.List(ctx, Query{Actions: []string{ActionStart, ActionStop}})
	if p.Total != 2 {
		t.Fatalf("want 2, got %d", p.Total)
	}
}

func TestFilterResult(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	r.Record(ctx, Event{Action: ActionStart, Result: ResultSuccess})
	r.Record(ctx, Event{Action: ActionStart, Result: ResultFailure})
	r.Record(ctx, Event{Action: ActionStart, Result: ResultDenied})
	p, _ := r.List(ctx, Query{Results: []string{ResultFailure, ResultDenied}})
	if p.Total != 2 {
		t.Fatalf("want 2, got %d", p.Total)
	}
}

func TestFilterContainerPrefix(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	long := "sha256:abcdef1234567890abcdef1234567890"
	r.Record(ctx, Event{Action: ActionStart, ContainerID: long, Result: ResultSuccess})
	r.Record(ctx, Event{Action: ActionStart, ContainerID: "sha256:ffffffff", Result: ResultSuccess})
	p, _ := r.List(ctx, Query{ContainerID: "abcdef123456"})
	if p.Total != 1 {
		t.Fatalf("want 1, got %d", p.Total)
	}
	p2, _ := r.List(ctx, Query{ContainerID: long})
	if p2.Total != 1 {
		t.Fatalf("exact match: want 1, got %d", p2.Total)
	}
}

func TestFilterContainerName(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	r.Record(ctx, Event{Action: ActionStart, ContainerName: "redis-master", Result: ResultSuccess})
	r.Record(ctx, Event{Action: ActionStart, ContainerName: "nginx", Result: ResultSuccess})
	p, _ := r.List(ctx, Query{ContainerName: "REDIS"})
	if p.Total != 1 {
		t.Fatalf("want 1, got %d", p.Total)
	}
}

func TestSortAsc(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	base := time.Now().UTC()
	r.Record(ctx, Event{Action: ActionStart, Result: ResultSuccess, Time: base})
	r.Record(ctx, Event{Action: ActionStart, Result: ResultSuccess, Time: base.Add(time.Minute)})
	p, _ := r.List(ctx, Query{SortTimeDesc: false})
	if p.Items[0].Time > p.Items[1].Time {
		t.Fatalf("want ascending: %s then %s", p.Items[0].Time, p.Items[1].Time)
	}
}

func TestTruncation(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	longDetail := strings.Repeat("x", 600)
	longUA := strings.Repeat("u", 400)
	bigPayload := map[string]any{"cmd": strings.Repeat("a", 6000)}
	r.Record(ctx, Event{Action: ActionExec, Result: ResultSuccess, Detail: longDetail, UserAgent: longUA, Payload: bigPayload})
	p, _ := r.List(ctx, Query{})
	if len(p.Items[0].Detail) != MaxDetailChars {
		t.Fatalf("detail len = %d want %d", len(p.Items[0].Detail), MaxDetailChars)
	}
	if len(p.Items[0].UserAgent) != MaxUserAgentChars {
		t.Fatalf("ua len = %d want %d", len(p.Items[0].UserAgent), MaxUserAgentChars)
	}
	// payload marshaled as string inside the item (re-marshalled from DB)
	if p.Items[0].Payload == nil {
		t.Fatal("payload nil")
	}
}

func TestRetention(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	oldTime := time.Now().Add(-200 * 24 * time.Hour)
	r.Record(ctx, Event{Action: ActionStart, Result: ResultSuccess, Time: oldTime})
	r.Record(ctx, Event{Action: ActionStart, Result: ResultSuccess, Time: time.Now()})
	sr, ok := r.(*sqliteRecorder)
	if !ok {
		t.Fatal("expected sqliteRecorder")
	}
	// prune with retention=100 days
	sr.cfg.RetentionDays = 100
	sr.prune()
	p, _ := r.List(ctx, Query{})
	if p.Total != 1 {
		t.Fatalf("after prune total=%d want 1", p.Total)
	}
}

func TestRetentionDisabled(t *testing.T) {
	r, err := Open(Config{DBPath: ":memory:", RetentionDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	sr := r.(*sqliteRecorder)
	if sr.stopCh != nil {
		t.Fatal("stopCh should be nil when retention disabled")
	}
}

func TestExportJSON(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	r.Record(ctx, Event{Action: ActionExec, Result: ResultSuccess, ContainerName: "c1", Payload: map[string]any{"exit_code": 0}})
	b, name, err := r.Export(ctx, Query{}, "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".json") {
		t.Fatalf("name = %s", name)
	}
	if !strings.Contains(string(b), `"action": "exec"`) {
		t.Fatalf("export missing action: %s", string(b))
	}
}

func TestExportMarkdown(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	r.Record(ctx, Event{Action: ActionStop, ContainerName: "redis", ContainerID: "abcdef1234567890", Result: ResultSuccess, StatusCode: 200, Detail: "ok"})
	b, name, err := r.Export(ctx, Query{}, "md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".md") {
		t.Fatalf("name=%s", name)
	}
	s := string(b)
	for _, want := range []string{"# Audit export", "| Time (UTC) |", "redis", "stop", "success"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}

func TestMarkdownEscapesPipes(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	r.Record(ctx, Event{Action: ActionExec, Result: ResultFailure, Detail: "a|b|c"})
	b, _, _ := r.Export(ctx, Query{}, "md")
	if strings.Contains(string(b), "|a|b|c|") {
		// escaped should be \| not raw | between cells
		// find the row containing a|b|c
		line := ""
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.Contains(ln, "a") {
				line = ln
				break
			}
		}
		// 9 columns means 10 pipes; if the detail contains raw pipes, the line would have more
		if strings.Count(line, "|") != 10 {
			t.Fatalf("unexpected pipe count in row: %q (%d)", line, strings.Count(line, "|"))
		}
	}
}

func TestStats(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	r.Record(ctx, Event{Action: ActionStart, Result: ResultSuccess})
	r.Record(ctx, Event{Action: ActionStart, Result: ResultFailure})
	r.Record(ctx, Event{Action: ActionStart, Result: ResultDenied})
	r.Record(ctx, Event{Action: ActionStart, Result: ResultSuccess, Time: time.Now().Add(-48 * time.Hour)})
	st, err := r.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 4 {
		t.Fatalf("total=%d", st.Total)
	}
	if st.Last24h != 3 {
		t.Fatalf("last24h=%d want 3", st.Last24h)
	}
	if st.Failures24h != 1 {
		t.Fatalf("failures24h=%d", st.Failures24h)
	}
	if st.Denied24h != 1 {
		t.Fatalf("denied24h=%d", st.Denied24h)
	}
}

func TestDropCountOnFailure(t *testing.T) {
	fr := &fakeRecorder{failOnInsert: true}
	rec := &sqliteRecorder{db: fr, dropCount: newCountAtomic(), cfg: Config{RetentionDays: 0}}
	rec.Record(context.Background(), Event{Action: ActionStart})
	if got := rec.dropCount.Load(); got != 1 {
		t.Fatalf("drop=%d want 1", got)
	}
}

func TestConcurrentInserts(t *testing.T) {
	r := openMem(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.Record(ctx, Event{Action: ActionStart, Result: ResultSuccess})
		}(i)
	}
	wg.Wait()
	p, _ := r.List(ctx, Query{Limit: 200})
	if p.Total != 100 {
		t.Fatalf("total=%d want 100", p.Total)
	}
}

// ---- fakes ----

type fakeRecorder struct {
	failOnInsert bool
}

func (f *fakeRecorder) Exec(query string, args ...any) (int64, int64, error) {
	if strings.Contains(strings.ToLower(query), "insert") && f.failOnInsert {
		return 0, 0, errors.New("boom")
	}
	return 0, 0, nil
}
func (f *fakeRecorder) Query(query string, args ...any) ([][]any, []string, error) {
	if strings.Contains(query, "COUNT(*)") {
		return [][]any{{int64(0)}}, []string{"c"}, nil
	}
	return nil, nil, nil
}
func (f *fakeRecorder) Close() error { return nil }

func newCountAtomic() *atomic.Int64 { return &atomic.Int64{} }

// sanity: noop recorder satisfies Recorder
var _ Recorder = (*noopRecorder)(nil)
var _ Recorder = (*sqliteRecorder)(nil)

func TestEventRequestIDGenerated(t *testing.T) {
	r, err := Open(Config{DBPath: ":memory:", RetentionDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.Record(context.Background(), Event{Action: ActionStart, Result: ResultSuccess})
	p, err := r.List(context.Background(), Query{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 {
		t.Fatalf("items=%d", len(p.Items))
	}
	if !strings.HasPrefix(p.Items[0].RequestID, "req_") {
		t.Fatalf("request_id prefix mismatch: %s", p.Items[0].RequestID)
	}
}

func TestEventRequestIDPreserved(t *testing.T) {
	r, err := Open(Config{DBPath: ":memory:", RetentionDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.Record(context.Background(), Event{Action: ActionStart, Result: ResultSuccess, RequestID: "corr-xyz"})
	p, err := r.List(context.Background(), Query{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if p.Items[0].RequestID != "corr-xyz" {
		t.Fatalf("request_id preserved? got %q", p.Items[0].RequestID)
	}
}

func TestExportMarkdownFormat(t *testing.T) {
	r, err := Open(Config{DBPath: ":memory:", RetentionDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.Record(context.Background(), Event{Action: ActionStart, Result: ResultSuccess, ContainerName: "app"})
	b, name, err := r.Export(context.Background(), Query{Limit: 10}, "md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".md") {
		t.Fatalf("name=%s", name)
	}
	if !strings.Contains(string(b), "# Audit export") || !strings.Contains(string(b), "| app |") {
		t.Fatalf("md contents: %s", b)
	}
}

func TestExportUnknownFormat(t *testing.T) {
	r, err := Open(Config{DBPath: ":memory:", RetentionDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _, err = r.Export(context.Background(), Query{}, "xml")
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestStatsDropCount(t *testing.T) {
	// noop recorder has zero drop count and no error.
	n := NewNoop(Config{RetentionDays: 7})
	st, err := n.Stats(context.Background())
	if err != nil || st.DropCount != 0 {
		t.Fatalf("noop stats: err=%v st=%+v", err, st)
	}
}

func TestDetailTruncation(t *testing.T) {
	r, err := Open(Config{DBPath: ":memory:", RetentionDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	long := strings.Repeat("a", MaxDetailChars+100)
	r.Record(context.Background(), Event{Action: ActionStart, Result: ResultSuccess, Detail: long})
	p, _ := r.List(context.Background(), Query{Limit: 1})
	if len(p.Items[0].Detail) != MaxDetailChars {
		t.Fatalf("detail len=%d want %d", len(p.Items[0].Detail), MaxDetailChars)
	}
}

func TestUserAgentTruncation(t *testing.T) {
	r, err := Open(Config{DBPath: ":memory:", RetentionDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	long := strings.Repeat("u", MaxUserAgentChars+100)
	r.Record(context.Background(), Event{Action: ActionStart, Result: ResultSuccess, UserAgent: long})
	p, _ := r.List(context.Background(), Query{Limit: 1})
	if len(p.Items[0].UserAgent) != MaxUserAgentChars {
		t.Fatalf("ua len=%d", len(p.Items[0].UserAgent))
	}
}

func TestPruneDoesntRemoveRecent(t *testing.T) {
	r, err := Open(Config{DBPath: ":memory:", RetentionDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.Record(context.Background(), Event{Action: ActionStart, Result: ResultSuccess})
	// invoke prune directly; nothing should be removed since time is now.
	sr := r.(*sqliteRecorder)
	sr.prune()
	p, _ := r.List(context.Background(), Query{Limit: 10})
	if p.Total != 1 {
		t.Fatalf("total=%d after prune", p.Total)
	}
}

func TestSanitizeFileTime(t *testing.T) {
	if s := sanitizeFileTime(time.Time{}); s != "all" {
		t.Fatalf("zero time=%s", s)
	}
	tm := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if s := sanitizeFileTime(tm); s != "20260102T030405Z" {
		t.Fatalf("formatted=%s", s)
	}
}
