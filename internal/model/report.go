package model

type DiagnosisOutcome string

const (
	OutcomeIssueIdentified DiagnosisOutcome = "issue_identified"
	OutcomeNoIssueFound    DiagnosisOutcome = "no_issue_found"
	OutcomeInconclusive    DiagnosisOutcome = "inconclusive"
	OutcomeEscalated       DiagnosisOutcome = "escalated"
)

type ConfirmedFinding struct {
	Text         string        `json:"text"`
	EvidenceRefs []EvidenceRef `json:"evidence_refs"`
}

type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

type Inference struct {
	Text         string        `json:"text"`
	Confidence   Confidence    `json:"confidence"`
	EvidenceRefs []EvidenceRef `json:"evidence_refs"`
}

type Unknown struct {
	Text                  string   `json:"text"`
	Reason                string   `json:"reason"`
	RelatedToolCallIDs    []string `json:"related_tool_call_ids,omitempty"`
	RelatedObservationIDs []string `json:"related_observation_ids,omitempty"`
}

type Recommendation struct {
	Text         string        `json:"text"`
	Reason       string        `json:"reason"`
	EvidenceRefs []EvidenceRef `json:"evidence_refs"`
}

type DiagnosisReport struct {
	DiagnosisID       string             `json:"diagnosis_id"`
	Outcome           DiagnosisOutcome   `json:"outcome"`
	ConfirmedFindings []ConfirmedFinding `json:"confirmed_findings"`
	Inferences        []Inference        `json:"inferences"`
	Unknowns          []Unknown          `json:"unknowns"`
	Recommendations   []Recommendation   `json:"recommendations"`
	Termination       Termination        `json:"termination"`
}
