package model

type DecisionType string

const (
	DecisionCallTool DecisionType = "call_tool"
	DecisionFinish   DecisionType = "finish"
	DecisionEscalate DecisionType = "escalate"
)

type ToolArguments struct {
	QueryGPUStatus    *QueryGPUStatusArgs
	QueryGPUProcesses *QueryGPUProcessesArgs
}

func (a ToolArguments) count() int {
	count := 0
	if a.QueryGPUStatus != nil {
		count++
	}
	if a.QueryGPUProcesses != nil {
		count++
	}
	return count
}

type QueryGPUStatusArgs struct{}

type QueryGPUProcessesArgs struct {
	GPUID string
}

type PlannerDecision struct {
	ID           string
	Type         DecisionType
	ToolName     string
	Arguments    ToolArguments
	Reason       string
	EvidenceRefs []EvidenceRef
	Unknowns     []string
}

func (d PlannerDecision) Validate() error {
	if d.ID == "" {
		return required("decision.id")
	}
	if d.Reason == "" {
		return required("decision.reason")
	}
	switch d.Type {
	case DecisionCallTool:
		if d.ToolName == "" {
			return required("decision.tool_name")
		}
		if d.Arguments.count() != 1 {
			return invalid("decision.arguments", "exactly one tool argument type must be set")
		}
		if len(d.EvidenceRefs) != 0 || len(d.Unknowns) != 0 {
			return invalid("decision", "call_tool must not include evidence refs or unknowns")
		}
		switch d.ToolName {
		case "query_gpu_status":
			if d.Arguments.QueryGPUStatus == nil {
				return invalid("decision.arguments", "does not match query_gpu_status")
			}
		case "query_gpu_processes":
			if d.Arguments.QueryGPUProcesses == nil || d.Arguments.QueryGPUProcesses.GPUID == "" {
				return invalid("decision.arguments", "query_gpu_processes requires gpu_id")
			}
		default:
			return invalid("decision.tool_name", "tool is not registered")
		}
	case DecisionFinish:
		if d.ToolName != "" || d.Arguments.count() != 0 || len(d.Unknowns) != 0 {
			return invalid("decision", "finish contains forbidden fields")
		}
		if len(d.EvidenceRefs) == 0 {
			return invalid("decision.evidence_refs", "finish requires evidence")
		}
	case DecisionEscalate:
		if d.ToolName != "" || d.Arguments.count() != 0 {
			return invalid("decision", "escalate contains tool fields")
		}
		if len(d.Unknowns) == 0 {
			return invalid("decision.unknowns", "escalate requires at least one unknown")
		}
	default:
		return invalid("decision.type", "unknown value")
	}
	for _, ref := range d.EvidenceRefs {
		if err := ref.Validate(); err != nil {
			return err
		}
	}
	return nil
}
