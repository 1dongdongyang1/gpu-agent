package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/1dongdongyang1/gpu-agent/internal/model"
	"github.com/1dongdongyang1/gpu-agent/internal/report"
	"github.com/1dongdongyang1/gpu-agent/internal/tools"
)

func TestHighMemoryScenarioEndToEndAcceptance(t *testing.T) {
	state, diagnosisReport, err := Run(context.Background(), HighMemoryScenario)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != model.DiagnosisFinished || state.Termination == nil || state.Termination.Reason != model.StopEvidenceSufficient {
		t.Fatalf("unexpected lifecycle result: status=%s termination=%+v", state.Status, state.Termination)
	}
	if diagnosisReport.Outcome != model.OutcomeIssueIdentified {
		t.Fatalf("outcome = %s, want issue_identified", diagnosisReport.Outcome)
	}
	if state.PlannerRounds() != 3 || state.ExecutionAttempts() != 2 || state.RejectedCalls() != 0 {
		t.Fatalf("unexpected counts: rounds=%d attempts=%d rejected=%d", state.PlannerRounds(), state.ExecutionAttempts(), state.RejectedCalls())
	}
	if len(state.ToolCalls) != 2 || state.ToolCalls[0].ToolName != tools.QueryGPUStatus || state.ToolCalls[1].ToolName != tools.QueryGPUProcesses {
		t.Fatalf("unexpected tool path: %+v", state.ToolCalls)
	}
	if state.ToolCalls[1].ExecutedArguments.QueryGPUProcesses.GPUID != "GPU-0" {
		t.Fatalf("process query did not target GPU-0: %+v", state.ToolCalls[1])
	}
	if err := report.Validate(state, diagnosisReport); err != nil {
		t.Fatalf("final report evidence is invalid: %v", err)
	}
	assertEveryEvidenceBelongsToObservation(t, state, diagnosisReport)
	assertReportBoundaries(t, diagnosisReport)
}

func TestHighMemoryScenarioIsDeterministic(t *testing.T) {
	firstState, firstReport, err := Run(context.Background(), HighMemoryScenario)
	if err != nil {
		t.Fatal(err)
	}
	secondState, secondReport, err := Run(context.Background(), HighMemoryScenario)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstState, secondState) {
		t.Fatalf("same scenario produced different states:\nfirst=%+v\nsecond=%+v", firstState, secondState)
	}
	if !reflect.DeepEqual(firstReport, secondReport) {
		t.Fatalf("same scenario produced different reports:\nfirst=%+v\nsecond=%+v", firstReport, secondReport)
	}
}

func TestUnsupportedScenarioFailsClosed(t *testing.T) {
	if _, _, err := Run(context.Background(), "arbitrary-shell"); err == nil {
		t.Fatal("unsupported scenario was accepted")
	}
}

func assertEveryEvidenceBelongsToObservation(t *testing.T, state model.DiagnosisState, diagnosisReport model.DiagnosisReport) {
	t.Helper()
	refs := make([]model.EvidenceRef, 0)
	for _, finding := range diagnosisReport.ConfirmedFindings {
		refs = append(refs, finding.EvidenceRefs...)
	}
	for _, inference := range diagnosisReport.Inferences {
		refs = append(refs, inference.EvidenceRefs...)
	}
	for _, recommendation := range diagnosisReport.Recommendations {
		refs = append(refs, recommendation.EvidenceRefs...)
	}
	for _, ref := range refs {
		matched := false
		for _, observation := range state.Observations {
			if observation.ID != ref.ObservationID {
				continue
			}
			for _, fact := range observation.Facts {
				if fact.ID == ref.FactID {
					matched = true
				}
			}
		}
		if !matched {
			t.Fatalf("evidence does not belong to observation: %+v", ref)
		}
	}
}

func assertReportBoundaries(t *testing.T, diagnosisReport model.DiagnosisReport) {
	t.Helper()
	var text []string
	for _, finding := range diagnosisReport.ConfirmedFindings {
		text = append(text, finding.Text)
	}
	for _, inference := range diagnosisReport.Inferences {
		text = append(text, inference.Text)
	}
	for _, recommendation := range diagnosisReport.Recommendations {
		text = append(text, recommendation.Text)
	}
	combined := strings.ToLower(strings.Join(text, " "))
	for _, forbidden := range []string{"memory leak confirmed", "process terminated", "gpu reset completed", "gpu-1 has no issue"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("report crossed evidence boundary with %q", forbidden)
		}
	}
}
