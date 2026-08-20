package model

type ErrorCode string

const (
	ErrorToolNotRegistered        ErrorCode = "tool_not_registered"
	ErrorToolNotReadOnly          ErrorCode = "tool_not_read_only"
	ErrorInvalidArguments         ErrorCode = "invalid_arguments"
	ErrorTargetOutOfScope         ErrorCode = "target_out_of_scope"
	ErrorGPUOutOfScope            ErrorCode = "gpu_out_of_scope"
	ErrorExecutionBudgetExhausted ErrorCode = "execution_budget_exhausted"
	ErrorDuplicateCall            ErrorCode = "duplicate_call"
	ErrorToolExecutionFailed      ErrorCode = "tool_execution_failed"
	ErrorParseFailed              ErrorCode = "parse_failed"
)

type RuntimeError struct {
	Code    ErrorCode
	Message string
}

func (e *RuntimeError) Validate() error {
	if e == nil {
		return required("error")
	}
	if e.Code == "" {
		return required("error.code")
	}
	if e.Message == "" {
		return required("error.message")
	}
	return nil
}

type ToolCallStatus string

const (
	ToolCallPending   ToolCallStatus = "pending"
	ToolCallRejected  ToolCallStatus = "rejected"
	ToolCallExecuting ToolCallStatus = "executing"
	ToolCallSucceeded ToolCallStatus = "succeeded"
	ToolCallFailed    ToolCallStatus = "failed"
	ToolCallTimeout   ToolCallStatus = "timeout"
)

type ToolCall struct {
	ID                 string
	DecisionID         string
	ToolName           string
	TargetID           string
	RequestedArguments ToolArguments
	ExecutedArguments  ToolArguments
	Fingerprint        string
	Status             ToolCallStatus
	Error              *RuntimeError
}

func (c ToolCall) Validate() error {
	if c.ID == "" || c.DecisionID == "" || c.ToolName == "" || c.TargetID == "" {
		return invalid("tool_call", "id, decision_id, tool_name, and target_id are required")
	}
	switch c.Status {
	case ToolCallPending, ToolCallExecuting, ToolCallSucceeded:
		if c.Error != nil {
			return invalid("tool_call.error", "must be empty for current status")
		}
	case ToolCallRejected, ToolCallFailed, ToolCallTimeout:
		if err := c.Error.Validate(); err != nil {
			return err
		}
	default:
		return invalid("tool_call.status", "unknown value")
	}
	return nil
}

type ObservationStatus string

const (
	ObservationSucceeded ObservationStatus = "succeeded"
	ObservationPartial   ObservationStatus = "partial"
	ObservationFailed    ObservationStatus = "failed"
)

type GPUStatus struct {
	GPUID         string
	MemoryTotalMB int64
	MemoryUsedMB  int64
	Utilization   float64
	TemperatureC  float64
}

type GPUStatusData struct{ GPUs []GPUStatus }

type GPUProcess struct {
	PID          int
	GPUID        string
	ProcessName  string
	MemoryUsedMB int64
}

type GPUProcessesData struct{ Processes []GPUProcess }

type ObservationDataType string

const (
	ObservationDataGPUStatus    ObservationDataType = "gpu_status"
	ObservationDataGPUProcesses ObservationDataType = "gpu_processes"
)

type ObservationData struct {
	Type         ObservationDataType
	GPUStatus    *GPUStatusData
	GPUProcesses *GPUProcessesData
}

func (d ObservationData) Validate() error {
	set := 0
	if d.GPUStatus != nil {
		set++
	}
	if d.GPUProcesses != nil {
		set++
	}
	if set != 1 {
		return invalid("observation.data", "exactly one typed payload must be set")
	}
	switch d.Type {
	case ObservationDataGPUStatus:
		if d.GPUStatus == nil {
			return invalid("observation.data.type", "does not match payload")
		}
	case ObservationDataGPUProcesses:
		if d.GPUProcesses == nil {
			return invalid("observation.data.type", "does not match payload")
		}
	default:
		return invalid("observation.data.type", "unknown value")
	}
	return nil
}

type RawResult struct {
	Content           string
	Truncated         bool
	OriginalSizeBytes int
	Redacted          bool
	Digest            string
}

type Observation struct {
	ID         string
	ToolCallID string
	Status     ObservationStatus
	Data       *ObservationData
	Facts      []ObservedFact
	Raw        RawResult
	Error      *RuntimeError
}

func (o Observation) Validate() error {
	if o.ID == "" || o.ToolCallID == "" {
		return invalid("observation", "id and tool_call_id are required")
	}
	switch o.Status {
	case ObservationSucceeded:
		if o.Error != nil {
			return invalid("observation.error", "must be empty when succeeded")
		}
		if o.Data == nil {
			return required("observation.data")
		}
	case ObservationPartial:
		if err := o.Error.Validate(); err != nil {
			return err
		}
		if o.Data == nil {
			return required("observation.data")
		}
	case ObservationFailed:
		if err := o.Error.Validate(); err != nil {
			return err
		}
	default:
		return invalid("observation.status", "unknown value")
	}
	if o.Data != nil {
		if err := o.Data.Validate(); err != nil {
			return err
		}
	}
	seen := make(map[string]struct{}, len(o.Facts))
	for _, fact := range o.Facts {
		if err := fact.Validate(); err != nil {
			return err
		}
		if _, ok := seen[fact.ID]; ok {
			return invalid("observation.facts", "duplicate fact ID "+fact.ID)
		}
		seen[fact.ID] = struct{}{}
	}
	return nil
}
