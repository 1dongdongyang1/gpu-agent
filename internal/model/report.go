package model

type DiagnosisOutcome string

const (
	OutcomeIssueIdentified DiagnosisOutcome = "issue_identified"
	OutcomeNoIssueFound    DiagnosisOutcome = "no_issue_found"
	OutcomeInconclusive    DiagnosisOutcome = "inconclusive"
	OutcomeEscalated       DiagnosisOutcome = "escalated"
)

type ConfirmedFinding struct {
	Text         string
	EvidenceRefs []EvidenceRef
}

type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

type Inference struct {
	Text         string
	Confidence   Confidence
	EvidenceRefs []EvidenceRef
}

type Unknown struct {
	Text                  string
	Reason                string
	RelatedToolCallIDs    []string
	RelatedObservationIDs []string
}

type Recommendation struct {
	Text         string
	Reason       string
	EvidenceRefs []EvidenceRef
}

type DiagnosisReport struct {
	DiagnosisID       string
	Outcome           DiagnosisOutcome
	ConfirmedFindings []ConfirmedFinding
	Inferences        []Inference
	Unknowns          []Unknown
	Recommendations   []Recommendation
	Termination       Termination
}
