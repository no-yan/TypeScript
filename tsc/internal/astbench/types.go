package astbench

import "time"

const SchemaVersion = 1

// Identity records the inputs which make a run comparable. Hashes are SHA-256
// hex strings. A nil TSGolint revision is intentional for this campaign.
type Identity struct {
	SchemaVersion      int               `json:"schema_version"`
	RepoRoot           string            `json:"repo_root"`
	BeforeRef          string            `json:"before_ref,omitempty"`
	AfterRef           string            `json:"after_ref,omitempty"`
	BeforeRevision     string            `json:"before_revision,omitempty"`
	AfterRevision      string            `json:"after_revision,omitempty"`
	TypescriptGoGitRev string            `json:"typescript_go_git_rev,omitempty"`
	TSGolintGitRev     *string           `json:"tsgolint_git_rev"`
	Dirty              bool              `json:"dirty"`
	AfterWorkingTree   bool              `json:"after_working_tree"`
	SourceHash         string            `json:"source_hash,omitempty"`
	FixtureHash        string            `json:"fixture_hash,omitempty"`
	DirtyPatchHash     string            `json:"dirty_patch_hash,omitempty"`
	SourceSnapshot     string            `json:"source_snapshot,omitempty"`
	FixtureSnapshot    string            `json:"fixture_snapshot,omitempty"`
	Binary             map[string]Binary `json:"binary,omitempty"`
	CreatedAt          time.Time         `json:"created_at"`
}

type Binary struct {
	Label     string `json:"label"`
	Path      string `json:"path"`
	Snapshot  string `json:"snapshot,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	BuildHash string `json:"build_hash,omitempty"`
}

type Plan struct {
	SchemaVersion int             `json:"schema_version"`
	RunID         string          `json:"run_id"`
	Seed          int64           `json:"seed"`
	RepoRoot      string          `json:"repo_root"`
	Identity      IdentityRef     `json:"identity"`
	Lanes         map[string]Lane `json:"lanes"`
	Cells         []Cell          `json:"cells"`
	Order         []Slot          `json:"order"`
	Binaries      []BinarySpec    `json:"binaries,omitempty"`
	Exclude       []string        `json:"exclude,omitempty"`
	Metrics       []string        `json:"metrics"`
	Timeout       time.Duration   `json:"timeout_ns"`
	CreatedAt     time.Time       `json:"created_at"`
}

type BinarySpec struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

type IdentityRef struct {
	BeforeRevision string `json:"before_revision,omitempty"`
	AfterRevision  string `json:"after_revision,omitempty"`
	SourceHash     string `json:"source_hash,omitempty"`
	FixtureHash    string `json:"fixture_hash,omitempty"`
}

type Lane struct {
	Name       string   `json:"name"`
	Kind       string   `json:"kind"` // daily or decision
	Backend    string   `json:"backend,omitempty"`
	Batches    int      `json:"batches"`
	Warmup     int      `json:"warmup"`
	Directions []string `json:"directions"` // AB, BA, AA
	Repeat     int      `json:"repeat"`
}

type Cell struct {
	ID                  string         `json:"id"`
	Case                string         `json:"case"`
	Shape               string         `json:"shape,omitempty"`
	Nodes               int            `json:"nodes"`
	Seed                int64          `json:"seed"`
	LayoutSeed          int64          `json:"layout_seed"`
	Layout              string         `json:"layout,omitempty"`
	Representation      string         `json:"representation,omitempty"`
	AfterRepresentation string         `json:"after_representation,omitempty"`
	GC                  string         `json:"gc,omitempty"`
	P                   int            `json:"p,omitempty"`
	Backend             string         `json:"backend,omitempty"`
	Batch               int            `json:"batch,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
}

type Slot struct {
	ID      string `json:"id"`
	CellID  string `json:"cell_id"`
	Lane    string `json:"lane"`
	Block   int    `json:"block"`
	Pair    int    `json:"pair"`
	Label   string `json:"label"` // before, after, A1, or A2 control labels
	Variant string `json:"variant"`
	Seed    int64  `json:"seed"`
	Attempt int    `json:"attempt,omitempty"`
}

type AttemptMetadata struct {
	SchemaVersion int          `json:"schema_version"`
	AttemptID     string       `json:"attempt_id"`
	SlotID        string       `json:"slot_id"`
	CellID        string       `json:"cell_id"`
	Lane          string       `json:"lane"`
	Pair          int          `json:"pair"`
	Block         int          `json:"block"`
	Label         string       `json:"label"`
	Variant       string       `json:"variant"`
	Binary        string       `json:"binary"`
	Argv          []string     `json:"argv"`
	Config        SampleConfig `json:"config"`
	Env           []string     `json:"env,omitempty"`
	StartedAt     time.Time    `json:"started_at"`
	FinishedAt    time.Time    `json:"finished_at,omitempty"`
	ExitCode      int          `json:"exit_code,omitempty"`
	Timeout       bool         `json:"timeout,omitempty"`
	Cancelled     bool         `json:"cancelled,omitempty"`
}

type Sample struct {
	NSPerOp        float64 `json:"ns_per_op"`
	BytesPerOp     float64 `json:"bytes_per_op"`
	AllocsPerOp    float64 `json:"allocs_per_op"`
	Visits         uint64  `json:"visits"`
	Checksum       uint64  `json:"checksum"`
	LogicalNodes   uint64  `json:"logical_nodes"`
	EdgeReads      uint64  `json:"edge_reads"`
	AttributeReads uint64  `json:"attribute_reads"`
	Revisits       uint64  `json:"revisits"`
	GCCycles       uint64  `json:"gc_cycles"`
	AllocatedBytes uint64  `json:"allocated_bytes"`
	Allocations    uint64  `json:"allocations"`
	Valid          bool    `json:"valid"`
	Reason         string  `json:"reason,omitempty"`
}

type AttemptResult struct {
	SchemaVersion int      `json:"schema_version"`
	Sample        Sample   `json:"sample"`
	Counters      Counters `json:"counters,omitempty"`
	Error         string   `json:"error,omitempty"`
	Complete      bool     `json:"complete"`
}

type Counters struct {
	Start  []uint64 `json:"start,omitempty"`
	End    []uint64 `json:"end,omitempty"`
	Delta  []uint64 `json:"delta,omitempty"`
	Valid  bool     `json:"valid"`
	Reason string   `json:"reason,omitempty"`
}

type Collection struct {
	SchemaVersion int         `json:"schema_version"`
	RunDir        string      `json:"run_dir"`
	Complete      bool        `json:"complete"`
	Valid         bool        `json:"valid"`
	Reason        string      `json:"reason,omitempty"`
	Attempts      []Collected `json:"attempts"`
}

type Collected struct {
	Metadata AttemptMetadata `json:"metadata"`
	Result   AttemptResult   `json:"result"`
}
