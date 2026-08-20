package tools

import "github.com/1dongdongyang1/gpu-agent/internal/model"

type Policy struct {
	registry Registry
}

func NewPolicy(registry Registry) Policy { return Policy{registry: registry} }

func (p Policy) Check(state model.DiagnosisState, call model.ToolCall) (model.ToolArguments, string, *model.RuntimeError) {
	definition, registered := p.registry.Lookup(call.ToolName)
	if !registered {
		return model.ToolArguments{}, "", policyError(model.ErrorToolNotRegistered, "requested tool is not registered")
	}
	if !definition.ReadOnly {
		return model.ToolArguments{}, "", policyError(model.ErrorToolNotReadOnly, "requested tool is not read-only")
	}
	if call.TargetID != state.Scope.TargetID {
		return model.ToolArguments{}, "", policyError(model.ErrorTargetOutOfScope, "tool target is outside diagnosis scope")
	}
	if state.ExecutionAttempts() >= state.Limits.MaxExecutionAttempts {
		return model.ToolArguments{}, "", policyError(model.ErrorExecutionBudgetExhausted, "tool execution budget is exhausted")
	}
	normalized, err := p.registry.Normalize(call.ToolName, call.RequestedArguments)
	if err != nil {
		return model.ToolArguments{}, "", policyError(model.ErrorInvalidArguments, err.Error())
	}
	gpuID := scopedGPUID(call.ToolName, normalized)
	if gpuID != "" && !state.Scope.AllowsGPU(gpuID) {
		return model.ToolArguments{}, "", policyError(model.ErrorGPUOutOfScope, "requested GPU is outside diagnosis scope")
	}
	fingerprint := Fingerprint(call.ToolName, call.TargetID, normalized)
	failures := 0
	for _, previous := range state.ToolCalls {
		if previous.Fingerprint != fingerprint {
			continue
		}
		if previous.Status == model.ToolCallSucceeded {
			return model.ToolArguments{}, "", policyError(model.ErrorDuplicateCall, "an identical call already succeeded")
		}
		if previous.Status == model.ToolCallFailed || previous.Status == model.ToolCallTimeout {
			failures++
		}
	}
	if failures >= 2 {
		return model.ToolArguments{}, "", policyError(model.ErrorDuplicateCall, "an identical call already failed twice")
	}
	return normalized, fingerprint, nil
}

func scopedGPUID(toolName string, arguments model.ToolArguments) string {
	switch toolName {
	case QueryGPUProcesses:
		return arguments.QueryGPUProcesses.GPUID
	case QueryXIDEvents:
		return arguments.QueryXIDEvents.GPUID
	case QueryRecentKernelLogs:
		return arguments.QueryRecentKernelLogs.GPUID
	default:
		return ""
	}
}

func policyError(code model.ErrorCode, message string) *model.RuntimeError {
	return &model.RuntimeError{Code: code, Message: message}
}
