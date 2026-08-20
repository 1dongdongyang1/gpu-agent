package model

type GPUAccessMode string

const (
	GPUAccessAll      GPUAccessMode = "all"
	GPUAccessSelected GPUAccessMode = "selected"
)

type Scope struct {
	TargetID      string
	GPUAccessMode GPUAccessMode
	AllowedGPUs   []string
}

func (s Scope) Validate() error {
	if s.TargetID == "" {
		return required("scope.target_id")
	}
	switch s.GPUAccessMode {
	case GPUAccessAll:
		if len(s.AllowedGPUs) != 0 {
			return invalid("scope.allowed_gpus", "must be empty when access mode is all")
		}
	case GPUAccessSelected:
		if len(s.AllowedGPUs) == 0 {
			return invalid("scope.allowed_gpus", "must not be empty when access mode is selected")
		}
		seen := make(map[string]struct{}, len(s.AllowedGPUs))
		for _, gpuID := range s.AllowedGPUs {
			if gpuID == "" {
				return invalid("scope.allowed_gpus", "contains an empty GPU ID")
			}
			if _, ok := seen[gpuID]; ok {
				return invalid("scope.allowed_gpus", "contains duplicate GPU ID "+gpuID)
			}
			seen[gpuID] = struct{}{}
		}
	default:
		return invalid("scope.gpu_access_mode", "unknown value")
	}
	return nil
}

func (s Scope) AllowsGPU(gpuID string) bool {
	if gpuID == "" {
		return false
	}
	if s.GPUAccessMode == GPUAccessAll {
		return true
	}
	for _, allowed := range s.AllowedGPUs {
		if allowed == gpuID {
			return true
		}
	}
	return false
}
