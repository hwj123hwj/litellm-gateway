package storage

import (
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/weijian/go-llm-gateway/internal/archive"
)

func TestSaveArchiveAndQueryArchives(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "archives.db"), log.Default())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ar := archive.NewArchive()
	ar.RequestID = "req-archive-1"
	ar.Timestamp = time.Now().UTC()
	ar.Protocol = archive.ProtocolMessages
	ar.Model = "coding"
	ar.Provider = "glm"
	ar.IsStream = false
	ar.Status = archive.StatusCompleted
	ar.StatusCode = 200
	ar.InputTokens = 10
	ar.OutputTokens = 20
	ar.RequestBody = `{"model":"coding","messages":[{"role":"user","content":"hi"}]}`
	ar.ResponseBody = `{"content":[{"type":"text","text":"hello"}]}`

	if err := store.SaveArchive(ar); err != nil {
		t.Fatalf("SaveArchive: %v", err)
	}

	records, total, err := store.QueryArchives(10, 0)
	if err != nil {
		t.Fatalf("QueryArchives: %v", err)
	}
	if total != 1 || len(records) != 1 {
		t.Fatalf("expected 1 record, got total=%d len=%d", total, len(records))
	}
	got := records[0]
	if got.RequestID != "req-archive-1" || got.Model != "coding" || got.Status != archive.StatusCompleted {
		t.Fatalf("queried record mismatch: %#v", got)
	}
}

func TestExportArchivesCursorContinuity(t *testing.T) {
	store, _ := NewSQLiteStore(filepath.Join(t.TempDir(), "export.db"), log.Default())
	defer store.Close()

	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		ar := archive.NewArchive()
		ar.RequestID = "batch-1-" + string(rune('A'+i))
		ar.Timestamp = base.Add(time.Duration(i) * time.Second)
		ar.Protocol = archive.ProtocolChatCompletions
		ar.Model = "coding"
		ar.Status = archive.StatusCompleted
		ar.StatusCode = 200
		ar.RequestBody = `{}`
		ar.ResponseBody = `{}`
		store.SaveArchive(ar)
	}

	// Page 1: limit 2, cursor at zero
	page1, err := store.ExportArchives(0, time.Time{}, 2)
	if err != nil {
		t.Fatalf("export page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}
	if page1[0].RequestID != "batch-1-A" {
		t.Errorf("page1[0] = %s, want batch-1-A", page1[0].RequestID)
	}

	// Page 2: resume from last of page1
	last := page1[len(page1)-1]
	page2, err := store.ExportArchives(last.ID, last.Timestamp, 2)
	if err != nil {
		t.Fatalf("export page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len = %d, want 2", len(page2))
	}
	// The first item of page2 must NOT be the same as last item of page1
	if page2[0].ID == last.ID {
		t.Errorf("cursor did not advance: page2[0].ID == page1[-1].ID")
	}
	if page2[0].Timestamp.Before(last.Timestamp) {
		t.Errorf("page2[0] timestamp before cursor")
	}

	// Page 3: only one left
	page3, err := store.ExportArchives(page2[1].ID, page2[1].Timestamp, 2)
	if err != nil {
		t.Fatalf("export page3: %v", err)
	}
	if len(page3) != 1 {
		t.Fatalf("page3 len = %d, want 1 (cursor continuity)", len(page3))
	}
}

func TestExportArchivesSameTimestampUsesID(t *testing.T) {
	store, _ := NewSQLiteStore(filepath.Join(t.TempDir(), "ts.db"), log.Default())
	defer store.Close()

	// Three records with identical timestamps — ordering must fall back to ID.
	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		store.SaveArchive(archive.Archive{
			RequestID: "same-ts-" + string(rune('A'+i)),
			Timestamp: ts,
			Protocol:  archive.ProtocolResponses,
			Status:    archive.StatusCompleted,
		})
	}

	all, err := store.ExportArchives(0, time.Time{}, 10)
	if err != nil {
		t.Fatalf("export all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3", len(all))
	}

	// Resume from the second record: must get exactly the third.
	page2, err := store.ExportArchives(all[1].ID, all[1].Timestamp, 10)
	if err != nil {
		t.Fatalf("export page2: %v", err)
	}
	if len(page2) != 1 || page2[0].ID != all[2].ID {
		t.Fatalf("same-timestamp cursor did not advance correctly: got %#v", page2)
	}
}

func TestDeleteArchivesBefore(t *testing.T) {
	store, _ := NewSQLiteStore(filepath.Join(t.TempDir(), "purge.db"), log.Default())
	defer store.Close()

	old := time.Now().AddDate(0, 0, -100)
	recent := time.Now()

	store.SaveArchive(archive.Archive{RequestID: "old", Timestamp: old, Protocol: archive.ProtocolMessages})
	store.SaveArchive(archive.Archive{RequestID: "recent", Timestamp: recent, Protocol: archive.ProtocolMessages})

	n, err := store.DeleteArchivesBefore(time.Now().AddDate(0, 0, -30))
	if err != nil {
		t.Fatalf("DeleteArchivesBefore: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted = %d, want 1", n)
	}

	_, total, _ := store.QueryArchives(10, 0)
	if total != 1 {
		t.Fatalf("after purge total = %d, want 1", total)
	}
}
