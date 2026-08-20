package tools

import (
	"fmt"
	"sort"

	"github.com/1dongdongyang1/gpu-agent/internal/model"
)

const (
	QueryGPUStatus    = "query_gpu_status"
	QueryGPUProcesses = "query_gpu_processes"
)

type Definition struct {
	Name     string
	ReadOnly bool
}

type Registry struct {
	definitions map[string]Definition
}

func NewRegistry() Registry {
	return Registry{definitions: map[string]Definition{
		QueryGPUStatus:    {Name: QueryGPUStatus, ReadOnly: true},
		QueryGPUProcesses: {Name: QueryGPUProcesses, ReadOnly: true},
	}}
}

func (r Registry) Lookup(name string) (Definition, bool) {
	definition, ok := r.definitions[name]
	return definition, ok
}

func (r Registry) Definitions() []Definition {
	result := make([]Definition, 0, len(r.definitions))
	for _, definition := range r.definitions {
		result = append(result, definition)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (r Registry) Normalize(name string, arguments model.ToolArguments) (model.ToolArguments, error) {
	switch name {
	case QueryGPUStatus:
		if arguments.QueryGPUStatus == nil || arguments.QueryGPUProcesses != nil {
			return model.ToolArguments{}, fmt.Errorf("query_gpu_status requires its dedicated empty arguments")
		}
		return model.ToolArguments{QueryGPUStatus: &model.QueryGPUStatusArgs{}}, nil
	case QueryGPUProcesses:
		if arguments.QueryGPUProcesses == nil || arguments.QueryGPUStatus != nil || arguments.QueryGPUProcesses.GPUID == "" {
			return model.ToolArguments{}, fmt.Errorf("query_gpu_processes requires gpu_id")
		}
		return model.ToolArguments{QueryGPUProcesses: &model.QueryGPUProcessesArgs{GPUID: arguments.QueryGPUProcesses.GPUID}}, nil
	default:
		return model.ToolArguments{}, fmt.Errorf("tool %s is not registered", name)
	}
}

func Fingerprint(name, targetID string, arguments model.ToolArguments) string {
	switch name {
	case QueryGPUStatus:
		return name + "|" + targetID + "|all"
	case QueryGPUProcesses:
		if arguments.QueryGPUProcesses != nil {
			return name + "|" + targetID + "|gpu_id=" + arguments.QueryGPUProcesses.GPUID
		}
	}
	return name + "|" + targetID + "|invalid"
}
