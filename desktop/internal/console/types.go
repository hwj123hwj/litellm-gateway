package console

type Summary struct {
	TodayRequests int      `json:"today_requests"`
	SuccessRate   float64  `json:"success_rate"`
	ActiveModels  int      `json:"active_models"`
	AvgLatency    float64  `json:"avg_latency_ms"`
	Uptime        string   `json:"uptime"`
	CacheHitRate  *float64 `json:"cache_hit_rate"`
	CacheRead     int64    `json:"cache_read_input_tokens"`
	CacheCreated  int64    `json:"cache_creation_input_tokens"`
}
type Provider struct {
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	State       string  `json:"state"`
	Enabled     *bool   `json:"enabled"`
	Requests    int     `json:"requests"`
	Errors      int     `json:"errors"`
	AvgLatency  float64 `json:"avg_latency"`
	HasProbe    bool    `json:"has_probe"`
	ProbeStatus string  `json:"probe_status"`
	ProbeDetail string  `json:"probe_detail"`
	LastProbe   string  `json:"last_probe_at"`
	NextRetry   string  `json:"next_retry_at"`
}
type Model struct {
	Name         string   `json:"model"`
	Provider     string   `json:"provider"`
	Providers    []string `json:"providers"`
	Status       string   `json:"status"`
	Requests     int      `json:"requests"`
	Tokens       int64    `json:"total_tokens"`
	AvgLatency   float64  `json:"avg_latency"`
	Capabilities []string `json:"capabilities"`
	Modalities   []string `json:"input_modalities"`
	CacheHitRate *float64 `json:"cache_hit_rate"`
}
type Dashboard struct {
	Summary   Summary    `json:"summary"`
	Providers []Provider `json:"providers"`
	Models    []Model    `json:"models"`
}
type Route struct {
	Model     string `json:"model"`
	Providers []struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
		Status  string `json:"status"`
	} `json:"providers"`
}
type Attempt struct {
	Provider string  `json:"provider"`
	Status   string  `json:"status"`
	Code     int     `json:"status_code"`
	Latency  float64 `json:"latency_ms"`
	Error    string  `json:"error"`
}
type Log struct {
	Timestamp    string    `json:"timestamp"`
	RequestID    string    `json:"request_id"`
	Method       string    `json:"method"`
	Path         string    `json:"path"`
	Model        string    `json:"model"`
	Provider     string    `json:"provider"`
	Code         int       `json:"status_code"`
	Latency      float64   `json:"latency_ms"`
	Input        int64     `json:"input_tokens"`
	Output       int64     `json:"output_tokens"`
	CacheRead    int64     `json:"cache_read_input_tokens"`
	CacheCreated int64     `json:"cache_creation_input_tokens"`
	Stream       bool      `json:"is_stream"`
	Error        string    `json:"error"`
	Attempts     []Attempt `json:"provider_attempts"`
}
type ClientConfig struct {
	Path    string `json:"path"`
	Exists  bool   `json:"file_exists"`
	InSync  bool   `json:"in_sync"`
	Desired []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"desired"`
	Current        []string `json:"current_ids"`
	Missing        []string `json:"missing_ids"`
	Stale          []string `json:"stale_ids"`
	Skipped        []string `json:"skipped_entries"`
	MissingEntries []string `json:"missing_entries"`
	Error          string   `json:"error"`
}
type Skill struct {
	ID          string          `json:"id"`
	Name        string          `json:"displayName"`
	Description string          `json:"description"`
	Enabled     bool            `json:"enabled"`
	Installed   map[string]bool `json:"installed"`
}
type Skills struct {
	Configured bool     `json:"configured"`
	Hint       string   `json:"hint"`
	Repo       string   `json:"repo"`
	Manifest   string   `json:"manifest_path"`
	Targets    []string `json:"targets"`
	Enabled    []string `json:"enabled"`
	InSync     bool     `json:"in_sync"`
	Skills     []Skill  `json:"skills"`
	Local      []struct {
		ID     string `json:"id"`
		Path   string `json:"path"`
		Source string `json:"source"`
	} `json:"local_skills"`
	Sync *struct {
		Targets []struct {
			Target  string   `json:"target"`
			Linked  []string `json:"linked"`
			Removed []string `json:"removed"`
			Skipped []string `json:"skipped"`
			Errors  []string `json:"errors"`
		} `json:"targets"`
	} `json:"sync"`
	Error string `json:"error"`
}
type Memory struct {
	ID         int     `json:"id"`
	Scope      string  `json:"scope_type"`
	ScopeKey   string  `json:"scope_key"`
	Statement  string  `json:"statement"`
	Status     string  `json:"status"`
	Source     string  `json:"source"`
	Confidence float64 `json:"confidence"`
	Hits       int     `json:"hit_count"`
	Updated    string  `json:"updated_at"`
}
type Prompt struct {
	Prompt  string `json:"prompt"`
	Custom  string `json:"custom"`
	Default string `json:"default"`
}
type Feedback struct {
	Rating  string `json:"rating"`
	Excerpt string `json:"reply_excerpt"`
	Note    string `json:"note"`
	Created string `json:"created_at"`
}
type Event struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	Tool    string `json:"tool"`
}
