package handlers

import (
	"encoding/json"
	"io"
	"log"
	"testing"
)

func TestInstallJobReadKeepsProgressSnapshot(t *testing.T) {
	h := NewSkillsInstallJobsHandler(nil, log.New(io.Discard, "", 0))
	job := &installJob{ID: "snapshot", Phase: "downloading", BytesDone: 10, BytesTotal: 100}
	h.record(job)
	read, ok := h.lookup(job.ID)
	if !ok {
		t.Fatal("job missing")
	}
	// 后台进度可以在查询和响应编码之间推进；已取得的响应仍应是一致快照。
	h.update(job, func() {
		job.Phase, job.BytesDone, job.DisplayName = "done", 100, "installed"
	})
	body, err := json.Marshal(read)
	if err != nil {
		t.Fatal(err)
	}
	var response installJob
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	if response.Phase != "downloading" || response.BytesDone != 10 || response.DisplayName != "" {
		t.Fatalf("read changed after progress update: %s", body)
	}
	latest, ok := h.lookup(job.ID)
	if !ok || latest.Phase != "done" || latest.BytesDone != 100 {
		t.Fatalf("latest progress missing: %+v", latest)
	}
}
