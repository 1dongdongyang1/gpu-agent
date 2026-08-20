package report

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/1dongdongyang1/gpu-agent/internal/diagnosis"
	"github.com/1dongdongyang1/gpu-agent/internal/idgen"
	"github.com/1dongdongyang1/gpu-agent/internal/mock"
	"github.com/1dongdongyang1/gpu-agent/internal/model"
	"github.com/1dongdongyang1/gpu-agent/internal/planner"
	"github.com/1dongdongyang1/gpu-agent/internal/tools"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func runHighMemory(t *testing.T) (model.DiagnosisState, model.DiagnosisReport) {
	t.Helper()
	scenario := mock.HighMemoryScenario()
	state, err := model.NewDiagnosisState("diagnosis-001", scenario.Alert, scenario.Scope, model.DiagnosisMode{Type: model.DiagnosisModeGeneralAgent}, model.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	ids := idgen.NewSequential()
	registry := tools.NewRegistry()
	orchestrator := diagnosis.NewOrchestrator(planner.NewDeterministic(ids), tools.NewPolicy(registry), tools.NewExecutor(scenario.Machine, ids), NewBuilder(), ids, fixedClock{now: time.Unix(0, 0)})
	finalState, diagnosisReport, err := orchestrator.Run(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	return finalState, diagnosisReport
}

func TestBuilderProducesEvidenceBoundedHighMemoryReport(t *testing.T) {
	state, diagnosisReport := runHighMemory(t)
	if diagnosisReport.Outcome != model.OutcomeIssueIdentified {
		t.Fatalf("outcome = %s", diagnosisReport.Outcome)
	}
	if len(diagnosisReport.ConfirmedFindings) != 2 || len(diagnosisReport.Inferences) != 2 || len(diagnosisReport.Unknowns) != 2 || len(diagnosisReport.Recommendations) != 1 {
		t.Fatalf("unexpected report shape: %+v", diagnosisReport)
	}
	combined := strings.ToLower(allReportText(diagnosisReport))
	for _, forbidden := range forbiddenClaims {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("report contains forbidden claim %q", forbidden)
		}
	}
	if !strings.Contains(combined, "pid-4321") || !strings.Contains(combined, "23500 mib") {
		t.Fatalf("report omitted bounded high-memory evidence: %s", combined)
	}
	if err := Validate(state, diagnosisReport); err != nil {
		t.Fatalf("generated report failed validation: %v", err)
	}
}

func TestValidatorRejectsMissingEvidenceAndRecoveryClaims(t *testing.T) {
	state, diagnosisReport := runHighMemory(t)
	diagnosisReport.ConfirmedFindings[0].EvidenceRefs[0].FactID = "fact-missing"
	if err := Validate(state, diagnosisReport); err == nil {
		t.Fatal("missing evidence was accepted")
	}

	_, diagnosisReport = runHighMemory(t)
	diagnosisReport.Recommendations[0].Text = "GPU reset completed"
	if err := Validate(state, diagnosisReport); err == nil {
		t.Fatal("claim that a recovery action completed was accepted")
	}
}

func allReportText(report model.DiagnosisReport) string {
	var parts []string
	for _, finding := range report.ConfirmedFindings {
		parts = append(parts, finding.Text)
	}
	for _, inference := range report.Inferences {
		parts = append(parts, inference.Text)
	}
	for _, unknown := range report.Unknowns {
		parts = append(parts, unknown.Text, unknown.Reason)
	}
	for _, recommendation := range report.Recommendations {
		parts = append(parts, recommendation.Text, recommendation.Reason)
	}
	return strings.Join(parts, " ")
}
