package model

import "time"

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
	Availability  GPUAvailability
	GPUID         string
	MemoryTotalMB int64
	MemoryUsedMB  int64
	Utilization   float64
	TemperatureC  float64
}

type GPUAvailability string

const (
	GPUOnline      GPUAvailability = "online"
	GPUUnavailable GPUAvailability = "unavailable"
)

func (s GPUStatus) Validate() error {
	if s.GPUID == "" {
		return required("gpu_status.gpu_id")
	}
	switch s.Availability {
	case GPUOnline:
		if s.MemoryTotalMB <= 0 || s.MemoryUsedMB < 0 || s.MemoryUsedMB > s.MemoryTotalMB {
			return invalid("gpu_status.memory", "online GPU memory is outside valid range")
		}
		if s.Utilization < 0 || s.Utilization > 100 {
			return invalid("gpu_status.utilization", "must be in [0,100]")
		}
	case GPUUnavailable:
		if s.MemoryTotalMB != 0 || s.MemoryUsedMB != 0 || s.Utilization != 0 || s.TemperatureC != 0 {
			return invalid("gpu_status", "unavailable GPU must not contain live metrics")
		}
	default:
		return invalid("gpu_status.availability", "unknown value")
	}
	return nil
}

type GPUStatusData struct{ GPUs []GPUStatus }

type GPUProcess struct {
	PID          int
	GPUID        string
	ProcessName  string
	MemoryUsedMB int64
}

type GPUProcessesData struct{ Processes []GPUProcess }

type DriverStatusData struct {
	Loaded        bool
	Version       string
	NVMLAvailable bool
}

func (d DriverStatusData) Validate() error {
	if !d.Loaded && (d.Version != "" || d.NVMLAvailable) {
		return invalid("driver_status", "unloaded driver cannot have a version or available NVML")
	}
	if d.Loaded && d.Version == "" {
		return required("driver_status.version")
	}
	return nil
}

type XIDEvent struct {
	ID         string
	GPUID      string
	Code       int64
	OccurredAt time.Time
	Summary    string
}

func (e XIDEvent) Validate() error {
	if e.ID == "" || e.GPUID == "" || e.Summary == "" || e.OccurredAt.IsZero() {
		return invalid("xid_event", "id, gpu_id, occurred_at, and summary are required")
	}
	if e.Code <= 0 {
		return invalid("xid_event.code", "must be positive")
	}
	return nil
}

type XIDEventsData struct {
	GPUID        string
	SinceMinutes int
	Events       []XIDEvent
}

type KernelLogSeverity string

const (
	KernelLogInfo    KernelLogSeverity = "info"
	KernelLogWarning KernelLogSeverity = "warning"
	KernelLogError   KernelLogSeverity = "error"
)

type KernelLogEntry struct {
	ID             string
	GPUID          string
	OccurredAt     time.Time
	Severity       KernelLogSeverity
	Component      string
	Message        string
	RelatedXIDCode *int64
}

func (e KernelLogEntry) Validate() error {
	if e.ID == "" || e.GPUID == "" || e.Component == "" || e.Message == "" || e.OccurredAt.IsZero() {
		return invalid("kernel_log", "id, gpu_id, occurred_at, component, and message are required")
	}
	switch e.Severity {
	case KernelLogInfo, KernelLogWarning, KernelLogError:
	default:
		return invalid("kernel_log.severity", "unknown value")
	}
	if e.RelatedXIDCode != nil && *e.RelatedXIDCode <= 0 {
		return invalid("kernel_log.related_xid_code", "must be positive")
	}
	return nil
}

type KernelLogsData struct {
	GPUID        string
	SinceMinutes int
	Entries      []KernelLogEntry
}

type ObservationDataType string

const (
	ObservationDataGPUStatus    ObservationDataType = "gpu_status"
	ObservationDataGPUProcesses ObservationDataType = "gpu_processes"
	ObservationDataDriverStatus ObservationDataType = "driver_status"
	ObservationDataXIDEvents    ObservationDataType = "xid_events"
	ObservationDataKernelLogs   ObservationDataType = "kernel_logs"
)

type ObservationData struct {
	Type         ObservationDataType
	GPUStatus    *GPUStatusData
	GPUProcesses *GPUProcessesData
	DriverStatus *DriverStatusData
	XIDEvents    *XIDEventsData
	KernelLogs   *KernelLogsData
}

func (d ObservationData) Validate() error {
	set := 0
	if d.GPUStatus != nil {
		set++
	}
	if d.GPUProcesses != nil {
		set++
	}
	if d.DriverStatus != nil {
		set++
	}
	if d.XIDEvents != nil {
		set++
	}
	if d.KernelLogs != nil {
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
		for _, status := range d.GPUStatus.GPUs {
			if err := status.Validate(); err != nil {
				return err
			}
		}
	case ObservationDataGPUProcesses:
		if d.GPUProcesses == nil {
			return invalid("observation.data.type", "does not match payload")
		}
	case ObservationDataDriverStatus:
		if d.DriverStatus == nil {
			return invalid("observation.data.type", "does not match payload")
		}
		if err := d.DriverStatus.Validate(); err != nil {
			return err
		}
	case ObservationDataXIDEvents:
		if d.XIDEvents == nil || d.XIDEvents.GPUID == "" || d.XIDEvents.SinceMinutes <= 0 {
			return invalid("observation.data.xid_events", "gpu_id and positive since_minutes are required")
		}
		for _, event := range d.XIDEvents.Events {
			if err := event.Validate(); err != nil || event.GPUID != d.XIDEvents.GPUID {
				return invalid("observation.data.xid_events", "contains an invalid or mismatched event")
			}
		}
	case ObservationDataKernelLogs:
		if d.KernelLogs == nil || d.KernelLogs.GPUID == "" || d.KernelLogs.SinceMinutes <= 0 {
			return invalid("observation.data.kernel_logs", "gpu_id and positive since_minutes are required")
		}
		for _, entry := range d.KernelLogs.Entries {
			if err := entry.Validate(); err != nil || entry.GPUID != d.KernelLogs.GPUID {
				return invalid("observation.data.kernel_logs", "contains an invalid or mismatched entry")
			}
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
