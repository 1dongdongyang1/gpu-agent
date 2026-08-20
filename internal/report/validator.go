package report

import (
	"fmt"
	"strings"

	"github.com/1dongdongyang1/gpu-agent/internal/model"
)

var forbiddenClaims = []string{
	"memory leak confirmed",
	"process terminated",
	"gpu reset completed",
	"driver reloaded",
	"host rebooted",
	"permanently failed",
	"hardware replacement required",
	"node isolated",
}

func Validate(state model.DiagnosisState, report model.DiagnosisReport) error {
	if report.DiagnosisID == "" || report.DiagnosisID != state.DiagnosisID {
		return fmt.Errorf("report diagnosis ID does not match state")
	}
	if state.Termination == nil || report.Termination.Reason != state.Termination.Reason {
		return fmt.Errorf("report termination does not match state")
	}
	switch report.Outcome {
	case model.OutcomeIssueIdentified, model.OutcomeNoIssueFound, model.OutcomeInconclusive, model.OutcomeEscalated:
	default:
		return fmt.Errorf("unknown report outcome %q", report.Outcome)
	}
	if report.Outcome == model.OutcomeIssueIdentified && state.Termination.Reason != model.StopEvidenceSufficient {
		return fmt.Errorf("issue_identified conflicts with termination %s", state.Termination.Reason)
	}
	if report.Outcome == model.OutcomeEscalated && state.Termination.Reason != model.StopEscalated {
		return fmt.Errorf("escalated outcome requires escalated termination")
	}

	for _, finding := range report.ConfirmedFindings {
		if finding.Text == "" || len(finding.EvidenceRefs) == 0 {
			return fmt.Errorf("confirmed finding requires text and evidence")
		}
		if err := validateEvidence(state, finding.EvidenceRefs); err != nil {
			return fmt.Errorf("confirmed finding: %w", err)
		}
		if err := rejectForbiddenClaim(finding.Text); err != nil {
			return err
		}
	}
	for _, inference := range report.Inferences {
		if inference.Text == "" || len(inference.EvidenceRefs) == 0 {
			return fmt.Errorf("inference requires text and evidence")
		}
		switch inference.Confidence {
		case model.ConfidenceLow, model.ConfidenceMedium, model.ConfidenceHigh:
		default:
			return fmt.Errorf("inference has invalid confidence")
		}
		if err := validateEvidence(state, inference.EvidenceRefs); err != nil {
			return fmt.Errorf("inference: %w", err)
		}
		if err := rejectForbiddenClaim(inference.Text); err != nil {
			return err
		}
	}
	for _, unknown := range report.Unknowns {
		if unknown.Text == "" || unknown.Reason == "" {
			return fmt.Errorf("unknown requires text and reason")
		}
	}
	for _, recommendation := range report.Recommendations {
		if recommendation.Text == "" || recommendation.Reason == "" || len(recommendation.EvidenceRefs) == 0 {
			return fmt.Errorf("recommendation requires text, reason, and evidence")
		}
		if err := validateEvidence(state, recommendation.EvidenceRefs); err != nil {
			return fmt.Errorf("recommendation: %w", err)
		}
		if err := rejectForbiddenClaim(recommendation.Text); err != nil {
			return err
		}
	}
	return nil
}

func validateEvidence(state model.DiagnosisState, refs []model.EvidenceRef) error {
	for _, ref := range refs {
		found := false
		for _, observation := range state.Observations {
			if observation.ID != ref.ObservationID || observation.Status == model.ObservationFailed {
				continue
			}
			for _, fact := range observation.Facts {
				if fact.ID == ref.FactID {
					found = true
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("evidence %s/%s does not resolve to an observed fact", ref.ObservationID, ref.FactID)
		}
	}
	return nil
}

func rejectForbiddenClaim(text string) error {
	lower := strings.ToLower(text)
	for _, forbidden := range forbiddenClaims {
		if strings.Contains(lower, forbidden) {
			return fmt.Errorf("report contains forbidden unsupported claim %q", forbidden)
		}
	}
	return nil
}
