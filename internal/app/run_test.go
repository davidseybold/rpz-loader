package app

import (
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/davidseybold/rpz-loader/internal/rpz"
	"github.com/prometheus/client_golang/prometheus"
)

type fakeStore struct {
	loadErr    error
	loaded     []string
	alsoNotify []string
	notified   []string
}

func (f *fakeStore) Load(zone, _ string) error {
	if f.loadErr != nil {
		return f.loadErr
	}
	f.loaded = append(f.loaded, zone)
	return nil
}
func (f *fakeStore) SetAlsoNotify(_ string, hosts []string) error { f.alsoNotify = hosts; return nil }
func (f *fakeStore) Notify(zone string) error                     { f.notified = append(f.notified, zone); return nil }

func gauge(t *testing.T, name, zone string) (float64, bool) {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["zone"] == zone {
				if labels["type"] != "managed" {
					t.Errorf("%s{zone=%q} type = %q, want managed", name, zone, labels["type"])
				}
				return m.GetGauge().GetValue(), true
			}
		}
	}
	return 0, false
}

func newTestSyncer(store zoneStore) *syncer {
	sy := newSyncer(slog.New(slog.NewTextHandler(io.Discard, nil)), store, []string{"192.168.5.21:53", "192.168.5.22:53"}, false)
	sy.now = func() time.Time { return time.Unix(1790000000, 0) }
	return sy
}

func TestRunRecordsSuccess(t *testing.T) {
	store := &fakeStore{}
	sy := newTestSyncer(store)
	o := rpz.Opts{ZoneName: "rpz.ok", Type: "managed", Filename: filepath.Join(t.TempDir(), "rpz.ok.zone")}
	build := func(rpz.Opts, int) (int, error) { return 42, nil }

	if err := sy.run(build, o); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.alsoNotify, []string{"192.168.5.21:53", "192.168.5.22:53"}) {
		t.Errorf("also-notify = %v", store.alsoNotify)
	}
	if len(store.loaded) != 1 || len(store.notified) != 1 {
		t.Errorf("loaded %v, notified %v", store.loaded, store.notified)
	}
	if v, ok := gauge(t, "rpz_loader_zone_last_success_timestamp_seconds", "rpz.ok"); !ok || v != 1790000000 {
		t.Errorf("last success = %v (%v)", v, ok)
	}
	if v, ok := gauge(t, "rpz_loader_zone_rules", "rpz.ok"); !ok || v != 42 {
		t.Errorf("rules = %v (%v)", v, ok)
	}

	// The next build sees 42 as the previous size (for the shrink check).
	var seen int
	_ = sy.run(func(_ rpz.Opts, previous int) (int, error) { seen = previous; return 40, nil }, o)
	if seen != 42 {
		t.Errorf("previous = %d, want 42", seen)
	}
}

// Failures must reach the scheduler (it counts result="fail") and leave the
// success metrics alone.
func TestRunReturnsFailures(t *testing.T) {
	for name, c := range map[string]struct {
		store *fakeStore
		build fileBuilder
	}{
		"download": {&fakeStore{}, func(rpz.Opts, int) (int, error) { return 0, errors.New("503") }},
		"load":     {&fakeStore{loadErr: errors.New("pdnsutil failed")}, func(rpz.Opts, int) (int, error) { return 5, nil }},
	} {
		t.Run(name, func(t *testing.T) {
			sy := newTestSyncer(c.store)
			zone := "rpz.fail-" + name
			err := sy.run(c.build, rpz.Opts{ZoneName: zone, Type: "managed", Filename: filepath.Join(t.TempDir(), "z.zone")})
			if err == nil {
				t.Fatal("want an error")
			}
			if _, ok := gauge(t, "rpz_loader_zone_last_success_timestamp_seconds", zone); ok {
				t.Error("a failed sync must not record a success")
			}
			if len(c.store.notified) != 0 {
				t.Error("a failed sync must not notify")
			}
		})
	}
}
