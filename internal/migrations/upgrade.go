package migrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const MarkerCollection = "_wekan_migration"
const MarkerID = "schema-upgrade"

type Result struct {
	Fixed      int64 `json:"fixed"`
	Unresolved int64 `json:"unresolved"`
}
type Step struct {
	Name  string
	Check func(context.Context, *mongo.Database) (bool, error)
	Run   func(context.Context, *mongo.Database) (Result, error)
}
type StepState struct {
	Status     string `json:"status"`
	Fixed      int64  `json:"fixed"`
	Unresolved int64  `json:"unresolved"`
	Error      string `json:"error,omitempty"`
}
type LastCheck struct {
	Version string    `bson:"version" json:"version"`
	At      time.Time `bson:"at" json:"at"`
}

func (c LastCheck) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Version string `json:"version"`
		At      string `json:"at"`
	}{c.Version, isoTime(c.At)})
}

type State struct {
	Running     bool                 `json:"running"`
	Product     string               `json:"product"`
	AppVersion  string               `json:"appVersion"`
	StartedAt   *string              `json:"startedAt"`
	FinishedAt  *string              `json:"finishedAt"`
	CurrentStep string               `json:"currentStep"`
	Gated       bool                 `json:"gated"`
	Steps       map[string]StepState `json:"steps"`
	LastCheck   *LastCheck           `json:"lastCheck"`
}
type UpgradeResult struct {
	Ran     []string       `json:"ran"`
	Skipped []string       `json:"skipped"`
	Results map[string]any `json:"results"`
	Gated   bool           `json:"gated"`
}
type UpgradeOptions struct {
	AppVersion string
	Force      bool
	Log        func(string)
}

// Runner preserves WeKan's version gate and per-step markers. A runner owns one
// database's live progress state and serializes its runs; unrelated installations
// never share a package-global dashboard or lock.
type Runner struct {
	steps []Step
	runMu sync.Mutex
	mu    sync.RWMutex
	state State
}

// NewSchemaRunner registers the complete current WeKan schema-upgrade pipeline.
// Individual migration functions never stamp the complete pipeline marker.
func NewSchemaRunner(files FilesystemOptions) *Runner {
	return newRunner([]Step{
		{"archived-flag-backfill", CheckArchivedFlagBackfill, RunArchivedFlagBackfill},
		{"swimlane-structure", CheckSwimlaneStructure, RunSwimlaneStructure},
		{"merge-per-swimlane-lists", CheckMergePerSwimlaneLists, RunMergePerSwimlaneLists},
		{"checklist-items-embedded", CheckChecklistItemsEmbedded, RunChecklistItemsEmbedded},
		{"customfields-boardIds", CheckCustomFieldsBoardIDs, RunCustomFieldsBoardIDs},
		{"board-allows-defaults", CheckBoardAllowsDefaults, RunBoardAllowsDefaults},
		{"attachment-kind-flags", CheckAttachmentKindFlags, RunAttachmentKindFlags},
		{"board-members-isactive", CheckBoardMembersIsActive, RunBoardMembersIsActive},
		{"board-permission-lowercase", CheckBoardPermissionLowercase, RunBoardPermissionLowercase},
		{"nonfinite-sort-repair", CheckNonfiniteSortRepair, RunNonfiniteSortRepair},
		{"checklist-minicard-unset", CheckChecklistMinicard, func(ctx context.Context, db *mongo.Database) (Result, error) {
			n, err := RunChecklistMinicard(ctx, db)
			return Result{Fixed: n}, err
		}},
		{"fs-path-heal", func(ctx context.Context, db *mongo.Database) (bool, error) {
			return CheckFilesystemPaths(ctx, db, files)
		},
			func(ctx context.Context, db *mongo.Database) (Result, error) {
				return RunFilesystemPaths(ctx, db, files)
			}},
	})
}

func newRunner(steps []Step) *Runner {
	seen := map[string]bool{}
	for _, step := range steps {
		if step.Name == "" || seen[step.Name] || step.Check == nil || step.Run == nil {
			panic("invalid migration step registry")
		}
		seen[step.Name] = true
	}
	return &Runner{steps: append([]Step(nil), steps...), state: State{Product: "WeKan", Steps: map[string]StepState{}}}
}
func (r *Runner) Snapshot() State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := r.state
	s.Steps = make(map[string]StepState, len(r.state.Steps))
	for k, v := range r.state.Steps {
		s.Steps[k] = v
	}
	if s.LastCheck != nil {
		v := *s.LastCheck
		s.LastCheck = &v
	}
	if s.StartedAt != nil {
		v := *s.StartedAt
		s.StartedAt = &v
	}
	if s.FinishedAt != nil {
		v := *s.FinishedAt
		s.FinishedAt = &v
	}
	return s
}
func isoTime(t time.Time) string         { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
func (r *Runner) change(fn func(*State)) { r.mu.Lock(); defer r.mu.Unlock(); fn(&r.state) }
func (r *Runner) Run(ctx context.Context, db *mongo.Database, opts UpgradeOptions) (UpgradeResult, error) {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	out := UpgradeResult{Ran: []string{}, Skipped: []string{}, Results: map[string]any{}}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	log := opts.Log
	if log == nil {
		log = func(string) {}
	}
	var marker struct {
		LastCheck *LastCheck `bson:"lastCheck"`
	}
	err := db.Collection(MarkerCollection).FindOne(ctx, bson.M{"_id": MarkerID}).Decode(&marker)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return out, err
	}
	start := isoTime(time.Now())
	r.change(func(s *State) {
		*s = State{Running: true, Product: "WeKan", AppVersion: opts.AppVersion, StartedAt: &start, Steps: map[string]StepState{}, LastCheck: marker.LastCheck}
		for _, step := range r.steps {
			s.Steps[step.Name] = StepState{Status: "pending"}
		}
	})
	defer r.change(func(s *State) { s.Running = false; s.CurrentStep = ""; end := isoTime(time.Now()); s.FinishedAt = &end })
	var setting struct {
		ProductName any `bson:"productName"`
	}
	if db.Collection("settings").FindOne(ctx, bson.M{"productName": bson.M{"$exists": true}}, options.FindOne().SetProjection(bson.M{"productName": 1})).Decode(&setting) == nil {
		if name, ok := setting.ProductName.(string); ok && strings.TrimSpace(name) != "" {
			r.change(func(s *State) { s.Product = strings.TrimSpace(name) })
		}
	}
	if !opts.Force && marker.LastCheck != nil && marker.LastCheck.Version == opts.AppVersion {
		out.Gated = true
		r.change(func(s *State) {
			s.Gated = true
			for name, v := range s.Steps {
				v.Status = "skipped"
				s.Steps[name] = v
			}
		})
		for _, step := range r.steps {
			out.Skipped = append(out.Skipped, step.Name)
		}
		return out, nil
	}
	clean := true
	for _, step := range r.steps {
		r.change(func(s *State) { s.CurrentStep = step.Name; s.Steps[step.Name] = StepState{Status: "checking"} })
		needed, stepErr := step.Check(ctx, db)
		if stepErr == nil && !needed {
			out.Skipped = append(out.Skipped, step.Name)
			r.change(func(s *State) { s.Steps[step.Name] = StepState{Status: "skipped"} })
			continue
		}
		if stepErr == nil {
			log("running " + step.Name + " ...")
			r.change(func(s *State) { s.Steps[step.Name] = StepState{Status: "running"} })
			var result Result
			result, stepErr = step.Run(ctx, db)
			if stepErr == nil {
				out.Ran = append(out.Ran, step.Name)
				out.Results[step.Name] = result
				r.change(func(s *State) {
					s.Steps[step.Name] = StepState{Status: "done", Fixed: result.Fixed, Unresolved: result.Unresolved}
				})
				if result.Unresolved != 0 {
					clean = false
				}
				log(fmt.Sprintf("%s: fixed %d, unresolved %d", step.Name, result.Fixed, result.Unresolved))
				// The JavaScript orchestrator deliberately tolerates history-write failures.
				_, _ = db.Collection(MarkerCollection).UpdateOne(ctx, bson.M{"_id": MarkerID}, bson.M{"$set": bson.M{"steps." + step.Name: bson.M{"doneAt": time.Now().UTC(), "fixed": result.Fixed, "unresolved": result.Unresolved}}}, options.UpdateOne().SetUpsert(true))
			}
		}
		if stepErr != nil {
			clean = false
			out.Results[step.Name] = map[string]string{"error": stepErr.Error()}
			r.change(func(s *State) { s.Steps[step.Name] = StepState{Status: "error", Error: stepErr.Error()} })
			log(step.Name + " FAILED (will retry next start): " + stepErr.Error())
		}
	}
	if clean {
		last := LastCheck{Version: opts.AppVersion, At: time.Now().UTC()}
		r.change(func(s *State) { s.LastCheck = &last })
		_, err = db.Collection(MarkerCollection).UpdateOne(ctx, bson.M{"_id": MarkerID}, bson.M{"$set": bson.M{"lastCheck": last}}, options.UpdateOne().SetUpsert(true))
		if err != nil {
			log("marker lastCheck update failed: " + err.Error())
		}
	}
	return out, nil
}
