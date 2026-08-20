package model

import "time"

type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityCritical AlertSeverity = "critical"
)

type Alert struct {
	ID       string
	TargetID string
	GPUID    string
	Type     string
	Severity AlertSeverity
	Message  string
}

func (a Alert) Validate() error {
	if a.ID == "" {
		return required("alert.id")
	}
	if a.TargetID == "" {
		return required("alert.target_id")
	}
	if a.Type == "" {
		return required("alert.type")
	}
	if a.Message == "" {
		return required("alert.message")
	}
	switch a.Severity {
	case AlertSeverityInfo, AlertSeverityWarning, AlertSeverityCritical:
		return nil
	default:
		return invalid("alert.severity", "unknown value")
	}
}

type DiagnosisModeType string

const (
	DiagnosisModeGeneralAgent DiagnosisModeType = "general_agent"
	DiagnosisModeSOP          DiagnosisModeType = "sop"
)

type DiagnosisMode struct {
	Type    DiagnosisModeType
	SOPName string
}

func (m DiagnosisMode) Validate() error {
	switch m.Type {
	case DiagnosisModeGeneralAgent:
		if m.SOPName != "" {
			return invalid("diagnosis_mode.sop_name", "must be empty in general agent mode")
		}
	case DiagnosisModeSOP:
		if m.SOPName == "" {
			return required("diagnosis_mode.sop_name")
		}
	default:
		return invalid("diagnosis_mode.type", "unknown value")
	}
	return nil
}

type Limits struct {
	MaxPlannerRounds            int
	MaxExecutionAttempts        int
	MaxConsecutiveRejectedCalls int
	MaxConsecutiveFailures      int
	MaxDuration                 time.Duration
}

func DefaultLimits() Limits {
	return Limits{
		MaxPlannerRounds:            6,
		MaxExecutionAttempts:        4,
		MaxConsecutiveRejectedCalls: 2,
		MaxConsecutiveFailures:      2,
		MaxDuration:                 30 * time.Second,
	}
}

func (l Limits) Validate() error {
	if l.MaxPlannerRounds <= 0 {
		return invalid("limits.max_planner_rounds", "must be positive")
	}
	if l.MaxExecutionAttempts <= 0 {
		return invalid("limits.max_execution_attempts", "must be positive")
	}
	if l.MaxConsecutiveRejectedCalls <= 0 {
		return invalid("limits.max_consecutive_rejected_calls", "must be positive")
	}
	if l.MaxConsecutiveFailures <= 0 {
		return invalid("limits.max_consecutive_failures", "must be positive")
	}
	if l.MaxDuration <= 0 {
		return invalid("limits.max_duration", "must be positive")
	}
	return nil
}

type DiagnosisStatus string

const (
	DiagnosisInitialized DiagnosisStatus = "initialized"
	DiagnosisRunning     DiagnosisStatus = "running"
	DiagnosisReporting   DiagnosisStatus = "reporting"
	DiagnosisFinished    DiagnosisStatus = "finished"
)

type StopReason string

const (
	StopEvidenceSufficient       StopReason = "evidence_sufficient"
	StopNoIssueFound             StopReason = "no_issue_found"
	StopEscalated                StopReason = "escalated"
	StopPlannerRoundsExhausted   StopReason = "planner_rounds_exhausted"
	StopExecutionBudgetExhausted StopReason = "execution_budget_exhausted"
	StopRepeatedPolicyRejection  StopReason = "repeated_policy_rejection"
	StopConsecutiveFailures      StopReason = "consecutive_failures"
	StopTimeout                  StopReason = "timeout"
	StopPlannerInvalidOutput     StopReason = "planner_invalid_output"
)

type Termination struct {
	Reason StopReason `json:"reason"`
	Detail string     `json:"detail"`
}

func (t Termination) Validate() error {
	switch t.Reason {
	case StopEvidenceSufficient,
		StopNoIssueFound,
		StopEscalated,
		StopPlannerRoundsExhausted,
		StopExecutionBudgetExhausted,
		StopRepeatedPolicyRejection,
		StopConsecutiveFailures,
		StopTimeout,
		StopPlannerInvalidOutput:
		return nil
	default:
		return invalid("termination.reason", "unknown value")
	}
}

type DiagnosisState struct {
	DiagnosisID  string
	Alert        Alert
	Scope        Scope
	Mode         DiagnosisMode
	Limits       Limits
	Status       DiagnosisStatus
	Decisions    []PlannerDecision
	ToolCalls    []ToolCall
	Observations []Observation
	Termination  *Termination
}

func NewDiagnosisState(id string, alert Alert, scope Scope, mode DiagnosisMode, limits Limits) (DiagnosisState, error) {
	state := DiagnosisState{
		DiagnosisID: id,
		Alert:       alert,
		Scope:       scope,
		Mode:        mode,
		Limits:      limits,
		Status:      DiagnosisInitialized,
	}
	if err := state.Validate(); err != nil {
		return DiagnosisState{}, err
	}
	return state, nil
}

func (s DiagnosisState) Validate() error {
	if s.DiagnosisID == "" {
		return required("diagnosis_state.diagnosis_id")
	}
	if err := s.Alert.Validate(); err != nil {
		return err
	}
	if err := s.Scope.Validate(); err != nil {
		return err
	}
	if s.Alert.TargetID != s.Scope.TargetID {
		return invalid("diagnosis_state.scope", "target does not match alert target")
	}
	if s.Alert.GPUID != "" && !s.Scope.AllowsGPU(s.Alert.GPUID) {
		return invalid("diagnosis_state.scope", "alert GPU is outside scope")
	}
	if err := s.Mode.Validate(); err != nil {
		return err
	}
	if err := s.Limits.Validate(); err != nil {
		return err
	}
	switch s.Status {
	case DiagnosisInitialized, DiagnosisRunning:
		if s.Termination != nil {
			return invalid("diagnosis_state.termination", "must be empty before reporting")
		}
	case DiagnosisReporting, DiagnosisFinished:
		if s.Termination == nil {
			return required("diagnosis_state.termination")
		}
		if err := s.Termination.Validate(); err != nil {
			return err
		}
	default:
		return invalid("diagnosis_state.status", "unknown value")
	}
	return s.validateHistory()
}

func (s DiagnosisState) validateHistory() error {
	decisionIDs := make(map[string]struct{}, len(s.Decisions))
	for _, decision := range s.Decisions {
		if err := decision.Validate(); err != nil {
			return err
		}
		if _, exists := decisionIDs[decision.ID]; exists {
			return invalid("diagnosis_state.decisions", "duplicate decision ID "+decision.ID)
		}
		decisionIDs[decision.ID] = struct{}{}
	}

	toolCallIDs := make(map[string]struct{}, len(s.ToolCalls))
	for _, call := range s.ToolCalls {
		if err := call.Validate(); err != nil {
			return err
		}
		if _, exists := decisionIDs[call.DecisionID]; !exists {
			return invalid("diagnosis_state.tool_calls", "unknown decision ID "+call.DecisionID)
		}
		if _, exists := toolCallIDs[call.ID]; exists {
			return invalid("diagnosis_state.tool_calls", "duplicate tool call ID "+call.ID)
		}
		toolCallIDs[call.ID] = struct{}{}
	}

	observationIDs := make(map[string]struct{}, len(s.Observations))
	for _, observation := range s.Observations {
		if err := observation.Validate(); err != nil {
			return err
		}
		if _, exists := toolCallIDs[observation.ToolCallID]; !exists {
			return invalid("diagnosis_state.observations", "unknown tool call ID "+observation.ToolCallID)
		}
		if _, exists := observationIDs[observation.ID]; exists {
			return invalid("diagnosis_state.observations", "duplicate observation ID "+observation.ID)
		}
		observationIDs[observation.ID] = struct{}{}
	}
	return nil
}

func (s DiagnosisState) PlannerRounds() int { return len(s.Decisions) }

func (s DiagnosisState) ExecutionAttempts() int {
	count := 0
	for _, call := range s.ToolCalls {
		switch call.Status {
		case ToolCallExecuting, ToolCallSucceeded, ToolCallFailed, ToolCallTimeout:
			count++
		}
	}
	return count
}

func (s DiagnosisState) RejectedCalls() int {
	count := 0
	for _, call := range s.ToolCalls {
		if call.Status == ToolCallRejected {
			count++
		}
	}
	return count
}

func (s DiagnosisState) RemainingExecutionBudget() int {
	remaining := s.Limits.MaxExecutionAttempts - s.ExecutionAttempts()
	if remaining < 0 {
		return 0
	}
	return remaining
}
