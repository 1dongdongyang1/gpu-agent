package tools

import (
	"fmt"
	"sort"

	"github.com/1dongdongyang1/gpu-agent/internal/model"
)

const (
	QueryGPUStatus        = "query_gpu_status"
	QueryGPUProcesses     = "query_gpu_processes"
	QueryDriverStatus     = "query_driver_status"
	QueryXIDEvents        = "query_xid_events"
	QueryRecentKernelLogs = "query_recent_kernel_logs"
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
		QueryGPUStatus:        {Name: QueryGPUStatus, ReadOnly: true},
		QueryGPUProcesses:     {Name: QueryGPUProcesses, ReadOnly: true},
		QueryDriverStatus:     {Name: QueryDriverStatus, ReadOnly: true},
		QueryXIDEvents:        {Name: QueryXIDEvents, ReadOnly: true},
		QueryRecentKernelLogs: {Name: QueryRecentKernelLogs, ReadOnly: true},
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
		if arguments.QueryGPUStatus == nil || argumentCount(arguments) != 1 {
			return model.ToolArguments{}, fmt.Errorf("query_gpu_status requires its dedicated empty arguments")
		}
		return model.ToolArguments{QueryGPUStatus: &model.QueryGPUStatusArgs{}}, nil
	case QueryGPUProcesses:
		if arguments.QueryGPUProcesses == nil || argumentCount(arguments) != 1 || arguments.QueryGPUProcesses.GPUID == "" {
			return model.ToolArguments{}, fmt.Errorf("query_gpu_processes requires gpu_id")
		}
		return model.ToolArguments{QueryGPUProcesses: &model.QueryGPUProcessesArgs{GPUID: arguments.QueryGPUProcesses.GPUID}}, nil
	case QueryDriverStatus:
		if arguments.QueryDriverStatus == nil || argumentCount(arguments) != 1 {
			return model.ToolArguments{}, fmt.Errorf("query_driver_status requires its dedicated empty arguments")
		}
		return model.ToolArguments{QueryDriverStatus: &model.QueryDriverStatusArgs{}}, nil
	case QueryXIDEvents:
		if arguments.QueryXIDEvents == nil || argumentCount(arguments) != 1 {
			return model.ToolArguments{}, fmt.Errorf("query_xid_events requires dedicated arguments")
		}
		args := *arguments.QueryXIDEvents
		if args.GPUID == "" || args.SinceMinutes < 1 || args.SinceMinutes > 1440 || args.Limit < 1 || args.Limit > 100 {
			return model.ToolArguments{}, fmt.Errorf("query_xid_events requires gpu_id, since_minutes in [1,1440], and limit in [1,100]")
		}
		return model.ToolArguments{QueryXIDEvents: &args}, nil
	case QueryRecentKernelLogs:
		if arguments.QueryRecentKernelLogs == nil || argumentCount(arguments) != 1 {
			return model.ToolArguments{}, fmt.Errorf("query_recent_kernel_logs requires dedicated arguments")
		}
		args := *arguments.QueryRecentKernelLogs
		if args.GPUID == "" || args.SinceMinutes < 1 || args.SinceMinutes > 1440 || args.Limit < 1 || args.Limit > 200 {
			return model.ToolArguments{}, fmt.Errorf("query_recent_kernel_logs requires gpu_id, since_minutes in [1,1440], and limit in [1,200]")
		}
		return model.ToolArguments{QueryRecentKernelLogs: &args}, nil
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
	case QueryDriverStatus:
		return name + "|" + targetID + "|status"
	case QueryXIDEvents:
		if arguments.QueryXIDEvents != nil {
			a := arguments.QueryXIDEvents
			return fmt.Sprintf("%s|%s|gpu_id=%s,since_minutes=%d,limit=%d", name, targetID, a.GPUID, a.SinceMinutes, a.Limit)
		}
	case QueryRecentKernelLogs:
		if arguments.QueryRecentKernelLogs != nil {
			a := arguments.QueryRecentKernelLogs
			return fmt.Sprintf("%s|%s|gpu_id=%s,since_minutes=%d,limit=%d", name, targetID, a.GPUID, a.SinceMinutes, a.Limit)
		}
	}
	return name + "|" + targetID + "|invalid"
}

func argumentCount(arguments model.ToolArguments) int {
	count := 0
	if arguments.QueryGPUStatus != nil {
		count++
	}
	if arguments.QueryGPUProcesses != nil {
		count++
	}
	if arguments.QueryDriverStatus != nil {
		count++
	}
	if arguments.QueryXIDEvents != nil {
		count++
	}
	if arguments.QueryRecentKernelLogs != nil {
		count++
	}
	return count
}
