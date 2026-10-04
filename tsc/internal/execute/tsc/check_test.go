package tsc

import (
	"context"
	"slices"
	"testing"
	"time"
)

type clockSys struct{ System }

func (clockSys) Now() time.Time { return time.Time{} }

// The order and the short-circuits of CollectCheckDiagnostics
// (store-cli-verification-instructions-20261004.md 5): each stage gives the
// diagnostics of the case, and the record is the stages it asked.
func TestCollectCheckDiagnostics(t *testing.T) {
	type input struct {
		config, syntactic, program, global, semantic, globalAfter []string
	}
	for _, tc := range []struct {
		name   string
		in     input
		stages []CheckStage
		want   []string
	}{
		{"syntax", input{syntactic: []string{"s"}, program: []string{"p"}}, []CheckStage{StageConfig, StageSyntactic}, []string{"s"}},
		{"program", input{program: []string{"p"}, semantic: []string{"x"}}, []CheckStage{StageConfig, StageSyntactic, StageProgram, StageBind, StageGlobal}, []string{"p"}},
		{"global", input{global: []string{"g"}, semantic: []string{"x"}}, []CheckStage{StageConfig, StageSyntactic, StageProgram, StageBind, StageGlobal}, []string{"g"}},
		{"none", input{semantic: []string{"x"}, globalAfter: []string{"g2"}}, []CheckStage{StageConfig, StageSyntactic, StageProgram, StageBind, StageGlobal, StageSemantic, StageGlobal}, []string{"g2", "x"}},
		// The config's count is the base: config diagnostics alone do not
		// stop the check.
		{"config", input{config: []string{"c"}, semantic: []string{"x"}}, []CheckStage{StageConfig, StageSyntactic, StageProgram, StageBind, StageGlobal, StageSemantic, StageGlobal}, []string{"c", "x"}},
		{"config and syntax", input{config: []string{"c"}, syntactic: []string{"s"}}, []CheckStage{StageConfig, StageSyntactic}, []string{"c", "s"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var events []CheckStage
			globals := 0
			stages := CheckStages[string]{
				Config:    func() []string { events = append(events, StageConfig); return tc.in.config },
				Syntactic: func(context.Context) []string { events = append(events, StageSyntactic); return tc.in.syntactic },
				Program:   func() []string { events = append(events, StageProgram); return tc.in.program },
				Bind:      func(context.Context) { events = append(events, StageBind) },
				Global: func(context.Context) []string {
					events = append(events, StageGlobal)
					globals++
					if globals == 2 {
						return tc.in.globalAfter
					}
					return tc.in.global
				},
				Semantic:           func(context.Context) []string { events = append(events, StageSemantic); return tc.in.semantic },
				SortAndDeduplicate: func(ds []string) []string { slices.Sort(ds); return slices.Compact(ds) },
			}
			got, run := CollectCheckDiagnostics(t.Context(), clockSys{}, &CompileTimes{}, stages)
			if !slices.Equal(events, tc.stages) || !slices.Equal(run.Stages, tc.stages) {
				t.Errorf("stages: called %v, recorded %v, want %v", events, run.Stages, tc.stages)
			}
			if run.CheckExecuted != slices.Contains(tc.stages, StageSemantic) {
				t.Errorf("CheckExecuted = %v", run.CheckExecuted)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("diagnostics %v, want %v", got, tc.want)
			}
		})
	}
}
