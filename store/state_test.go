package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWindowTimeOrder pins the one property the index depends on and cannot
// check for itself: these strings are compared and ordered as text, by SQLite,
// in every query that asks which window came first or last. If the encoding
// ever stops being fixed width, text order stops being time order and the
// resume point starts moving backwards - silently, and only for some pairs.
func TestWindowTimeOrder(t *testing.T) {
	base := time.Date(2023, 6, 1, 12, 0, 0, 0, time.UTC)
	instants := []time.Time{
		base.AddDate(0, -1, 0),
		base.Add(-time.Second),
		base,
		base.Add(time.Nanosecond),             // the pair RFC3339Nano inverted:
		base.Add(999999999 * time.Nanosecond), // a fraction against a whole second
		base.Add(time.Second),
		base.AddDate(0, 0, 1),
	}
	for i := 1; i < len(instants); i++ {
		prev, cur := ts(instants[i-1]), ts(instants[i])
		if !(prev < cur) {
			t.Errorf("%s sorts at or after %s, but happens before it", prev, cur)
		}
		if len(prev) != len(cur) {
			t.Errorf("width differs: %q is %d bytes, %q is %d", prev, len(prev), cur, len(cur))
		}
	}
	// What is written has to come back as what went in, since Resume hands the
	// parsed value straight to the next harvest as a request bound.
	got, err := parseWindowTime(ts(instants[3]))
	if err != nil {
		t.Fatalf("parseWindowTime: %v", err)
	}
	if !got.Equal(instants[3]) {
		t.Errorf("round trip: got %v, want %v", got, instants[3])
	}
}

// openRaw opens a database without the version machinery, so a test can put a
// file into a shape prepareSchemaTo has to have an opinion about.
func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestSchemaStamp: a shard says what it is and which shape it has, in the two
// header integers sqlite keeps for the application and never reads itself.
func TestSchemaStamp(t *testing.T) {
	w, err := OpenWriter(t.TempDir(), Identity{BaseURL: "http://example.com", Format: "oai_dc"})
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	defer w.Close()
	var appID, version int
	if err := w.st.db.QueryRow(`SELECT * FROM pragma_application_id`).Scan(&appID); err != nil {
		t.Fatalf("application_id: %v", err)
	}
	if err := w.st.db.QueryRow(`SELECT * FROM pragma_user_version`).Scan(&version); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if appID != applicationID {
		t.Errorf("application_id = %#x, want %#x", appID, applicationID)
	}
	if version != schemaVersion {
		t.Errorf("user_version = %d, want %d", version, schemaVersion)
	}
}

// TestSchemaRefusesUnknown: the point of the stamp is the two files this code
// must not write into - one from before the stamp existed, whose shape is
// anybody's guess, and one from a metha that knows something this one does not.
func TestSchemaRefusesUnknown(t *testing.T) {
	t.Run("unversioned", func(t *testing.T) {
		db := openRaw(t, filepath.Join(t.TempDir(), "state.sqlite"))
		if _, err := db.Exec(schema); err != nil {
			t.Fatalf("schema: %v", err)
		}
		err := prepareSchemaTo(db, schemaVersion, migrations)
		if !errors.Is(err, errUnversioned) {
			t.Errorf("opening a shard with no stamp: got %v, want %v", err, errUnversioned)
		}
	})
	t.Run("from the future", func(t *testing.T) {
		db := openRaw(t, filepath.Join(t.TempDir(), "state.sqlite"))
		if err := prepareSchemaTo(db, schemaVersion, migrations); err != nil {
			t.Fatalf("first open: %v", err)
		}
		if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion+9)); err != nil {
			t.Fatalf("stamp: %v", err)
		}
		err := prepareSchemaTo(db, schemaVersion, migrations)
		if err == nil || !strings.Contains(err.Error(), "upgrade metha") {
			t.Errorf("opening a newer shard: got %v, want it to say so", err)
		}
	})
}

// TestSchemaMigrates walks the ladder, which is the whole reason the stamp is
// worth having: a shard written by an older metha is brought up to the shape
// this one writes, in one transaction with the stamp that records it, and a gap
// in the steps is refused rather than skipped over.
func TestSchemaMigrates(t *testing.T) {
	db := openRaw(t, filepath.Join(t.TempDir(), "state.sqlite"))
	if err := prepareSchemaTo(db, schemaVersion, migrations); err != nil {
		t.Fatalf("first open: %v", err)
	}
	steps := map[int][]string{
		schemaVersion:     {`ALTER TABLE windows ADD COLUMN probe INTEGER NOT NULL DEFAULT 0`},
		schemaVersion + 1: {`CREATE TABLE probe_table (x INTEGER)`},
	}
	if err := prepareSchemaTo(db, schemaVersion+2, steps); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	var version int
	if err := db.QueryRow(`SELECT * FROM pragma_user_version`).Scan(&version); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if version != schemaVersion+2 {
		t.Errorf("after upgrading: user_version = %d, want %d", version, schemaVersion+2)
	}
	if _, err := db.Exec(`INSERT INTO probe_table (x) VALUES (1)`); err != nil {
		t.Errorf("the second step did not run: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('windows') WHERE name = 'probe'`).Scan(&n); err != nil {
		t.Fatalf("table_info: %v", err)
	}
	if n != 1 {
		t.Errorf("the first step did not run: windows has no probe column")
	}
	// Re-opening at the version it now carries is a no-op, so an upgrade is not
	// something a shard can be walked through twice.
	if err := prepareSchemaTo(db, schemaVersion+2, steps); err != nil {
		t.Errorf("reopening an upgraded shard: %v", err)
	}
	// A version with no step to leave it stops the upgrade instead of pretending.
	err := prepareSchemaTo(db, schemaVersion+4, steps)
	if err == nil || !strings.Contains(err.Error(), "no way to upgrade") {
		t.Errorf("upgrading across a missing step: got %v, want it refused", err)
	}
}
