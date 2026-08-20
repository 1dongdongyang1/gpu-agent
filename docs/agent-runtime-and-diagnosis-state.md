# Agent 运行机制、证据链、报告与 Go 工程设计总结

## 1. 文档目的

本文档整理当前阶段已经讨论清楚的内容，作为后续设计与实现的共同起点，主要包括：

- 通用 Agent 的程序运行逻辑；
- GPU 只读诊断 Agent 的完整循环；
- `DiagnosisState`、`PlannerContext`、`PlannerDecision` 的责任边界；
- `PlannerDecision`、`ToolCall`、`Observation` 的区别和关联；
- 经过校验的诊断 `Scope` 如何限制 LLM；
- 工具原始输出如何转换为结构化 `Observation`；
- 被 Policy Checker 拒绝、工具失败和解析失败分别如何记录；
- `ObservedFact`、`EvidenceRef` 与 `DiagnosisReport` 如何关联；
- 报告生成、确定性校验、LLM 语义推断与人工决策的边界；
- `DiagnosisState` 的生命周期和第一版 Go 包结构；
- 高显存与 Xid / 掉卡两条闭环的已实现状态；
- 下一阶段进入高温场景、最小 SOP Router 和受限 LLM Planner 前的门槛。

本文档记录当前架构共识和已经进入 Go 编码验证的第一版协议。高显存与 Xid / 掉卡两条确定性闭环均已通过 CLI、单元测试、race 检查和静态检查；后续新增工具和场景应复用现有协议。字段名和文件拆分仍可根据编译与测试结果小幅调整，但安全边界、证据语义、Planner 权限和报告责任不能作为普通实现细节随意改变。

## 2. 项目边界

本项目是一个面向秋招展示的本地 Go + Mock GPU 只读诊断 Agent。它自动完成安全但重复的现场调查，不自动修复故障，也不替代运维人员做高风险决定。

核心闭环是：

```text
Mock Alert
→ 匹配结构化 SOP 或进入受限通用调查
→ SOP / Planner 选择只读语义化工具
→ 程序校验并执行工具
→ 从统一 Mock Machine State 获得 Observation
→ 根据 Observation 继续调查或终止
→ 输出事实、推断、未知项、Evidence 和人工建议
```

LLM 没有任意 Shell、SSH 凭据或状态修改工具。主机、GPU、时间范围、工具白名单、调用预算和终止条件均由确定性程序控制。

## 3. 对 Agent 的核心理解

Agent 不是一个特殊的“大模型结构体”，而是一个由普通程序控制的循环：程序向决策模块提供当前任务视图，决策模块提出下一步，程序校验并执行动作，将新观察写回状态，然后继续决策或终止。

```text
State
→ Decision
→ Action
→ Observation
→ New State
```

普通问答模型通常根据已有文本直接回答；Agent 能通过工具取得原本不存在的新信息，并在“决策—行动—观察—再决策”的闭环中推进任务。

本项目采用混合架构：

```text
已知问题：结构化 SOP 按确定性规则决定下一步
未知问题：受限 LLM Planner 根据当前 Observation 建议下一步
共同部分：工具、Policy、State、Loop Guard、Evidence 和 Report
```

## 4. 各模块的职责

### 4.1 Diagnosis Orchestrator

Orchestrator 推进整个诊断程序：

- 初始化 `DiagnosisState`；
- 调用 SOP Runner 或 Planner；
- 调用 Decision Validator 和 Policy Checker；
- 创建并执行 `ToolCall`；
- 将 `Observation` 写回状态；
- 调用 Loop Guard；
- 进入下一轮或终止并生成报告。

### 4.2 Planner

Planner 根据当前一轮的 `PlannerContext` 建议下一步。第一版只允许三种决策：

```text
call_tool：建议调用一项白名单只读工具
finish：认为现有证据足以支持有限结论
escalate：证据不足、工具受阻或无法继续，建议人工接手
```

Planner 只能提出建议，不能执行工具、修改状态、扩大权限、重置预算或决定外部强制终止条件。

### 4.3 Policy Checker

Policy Checker 在每次工具执行前进行确定性校验：

- 工具是否注册且只读；
- 参数是否符合 Schema；
- Target、GPU 和查询范围是否属于本次 `Scope`；
- 是否超出预算；
- 是否属于禁止的重复调用；
- 是否违反其他固定策略。

Prompt 和模型结构化输出只是第一层约束，不能代替 Policy Checker。

### 4.4 Tool Executor 与 Adapter

Executor 负责真正调用 Mock 工具或未来的真实只读适配器。Adapter 负责把底层返回转换为统一的结构化结果。

### 4.5 Loop Guard

Loop Guard 强制控制：

- 最大 Planner 轮次；
- 最大实际执行次数；
- 连续策略拒绝；
- 连续工具失败；
- 重复调用；
- 总体超时；
- 预算耗尽；
- 是否还有合法且有价值的调查方向。

合法终止结果不只有“找到根因”，还包括状态正常、证据不足、工具失败、预算耗尽和需要人工接手。

### 4.6 Human Operator

人工负责复核报告、判断业务影响和决定所有状态变更动作，例如终止进程、GPU reset、重载驱动或重启主机。

## 5. 完整运行链路

```text
Alert
  ↓ 校验输入并初始化
DiagnosisState
  ↓ Context Builder
PlannerContext
  ↓ Planner Instructions + 输出 Schema
LLM Planner
  ↓ 解析与格式校验
PlannerDecision
  ↓ Decision Validator
  ├── call_tool
  │     ↓ 创建 ToolCall(pending)
  │   Policy Checker
  │     ├── rejected → 写回 ToolCall，进入下一轮或触发终止
  │     └── 通过 → Executor → Raw Result → Parser / Adapter
  │                                      ↓
  │                                  Observation
  │                                      ↓
  │                              写回 DiagnosisState
  ├── finish → Evidence / 结束条件校验 → Report 阶段
  └── escalate → 升级条件校验 → Report 阶段
```

这里有两个不能混淆的结构化转换：

```text
LLM 输出 → PlannerDecision
工具原始输出 → Observation
```

Planner 的输出不会直接变成 `Observation`。

## 6. Alert 与经过校验的 Scope

`Alert` 是外部输入，不能直接等同于系统授权。程序根据 Alert 和固定策略完成校验后，为本次诊断生成 `Scope`。

```text
Alert.TargetID：外部输入声称发生告警的目标
Scope.TargetID：本次诊断真正被允许调查的目标
```

第一版 `Scope` 包含：

```text
Scope
├── TargetID
├── GPUAccessMode: all | selected
└── AllowedGPUs
```

`GPUAccessMode=all` 时，`AllowedGPUs` 必须为空，表示允许调查该 Target 下所有已经登记的 GPU；`GPUAccessMode=selected` 时，`AllowedGPUs` 必须至少包含一个合法 GPU。禁止让空数组同时表达“全部 GPU”和“没有授权”，也禁止 `all` 与非空列表并存。

初始化时需要检查 Target 是否存在、是否允许诊断、GPU 是否属于该 Target。非法 Target 或 GPU 直接导致初始化失败，不能用空 Scope 继续调查。每次 `ToolCall` 执行前仍需重新对照 `Scope` 校验，不能只在初始化时校验一次。

第一版一次诊断只绑定一个 Target，因此 Planner 不填写 `TargetID`。Orchestrator 从 `Scope.TargetID` 注入 `ToolCall.TargetID`；Planner 只选择工具及真正需要决策的受限参数，例如 GPU 和时间范围。Policy Checker 在执行前仍需再次确认 ToolCall 与 Scope 一致，以覆盖 SOP、测试、状态恢复或未来其他调用入口。

准确的安全表述是：

> LLM 本身没有登录机器的权限；经过校验的 Scope 和每次调用前的 Policy Checker，防止模型建议的工具调用越过本次诊断被授权的主机与 GPU 范围。

## 7. DiagnosisState 的责任边界

### 7.1 核心定义

`DiagnosisState` 是整次诊断截至当前的累计结构化存档，不是只保存当前一步的临时变量，也不主动控制流程。

```text
DiagnosisState：描述整次诊断目前是什么状态
Orchestrator：根据状态推进程序
Planner：建议下一步调查什么
PlannerContext：当前一轮提供给 Planner 的只读视图
```

它可以类比为游戏存档；Orchestrator 是推进游戏的程序，Planner 是提出下一步行动的角色。

### 7.2 第一版最小内容

```text
DiagnosisState
├── DiagnosisID
├── Alert
├── Scope
├── Mode
├── Limits
├── Status
├── Decisions []PlannerDecision
├── ToolCalls []ToolCall
├── Observations []Observation
└── Termination
```

其中：

- `Alert` 保存诊断为何启动；
- `Scope` 保存经过校验的执行边界；
- `Mode` 表示某个 SOP 或通用 Agent；
- `Limits` 保存本次最初被授予的预算；
- `Status` 保存生命周期状态；
- 三类历史记录原则上只追加，不随意覆盖；
- `Termination` 保存是否终止以及终止原因。

第一版生命周期状态确定为：

```text
initialized
running
reporting
finished
```

它们只回答程序当前运行到哪个阶段：

```text
initialized：Alert、Scope 和初始状态已经创建
running：SOP 或 Planner 正在推进只读调查
reporting：调查已经停止，正在生成并校验报告
finished：最终报告已经形成，本次诊断结束
```

`Status`、`Termination` 和 `DiagnosisReport.Outcome` 必须分开：

```text
Status：程序当前运行到哪个阶段
Termination.Reason：为什么停止继续调查
DiagnosisReport.Outcome：调查最终得到什么结果
```

### 7.3 必须保存的源数据与动态派生数据

必须持久保存的源数据包括：

```text
DiagnosisID、Alert、Scope、Mode、Limits、Status、
Decisions、ToolCalls、Observations、Termination
```

以下内容可以从历史记录动态计算，不应成为相互冲突的第二份权威数据：

```text
PlannerRounds
ExecutionAttempts
RejectedCalls
ConsecutiveFailures
RemainingToolBudget
LastObservation
PreviouslyRequestedCalls
```

例如：

```text
PlannerRounds = Decisions 数量
RemainingToolBudget = Limits.MaxExecutionAttempts - ExecutionAttempts
```

第一版数据量很小，直接计算足够。未来可以缓存派生计数，但历史记录仍是事实来源。

### 7.4 当前候选原因与未知项

候选原因是 LLM 推断，不是机器事实。第一版暂不把“当前候选原因”设为 `DiagnosisState` 中的权威字段，避免它与 `Observation` 混淆。

每个 `PlannerDecision` 可以保存当轮 `Reason`，而本轮候选方向和未知项可由 Context Builder 根据 Alert、Observation 和近期决策构造。`escalate` 决策中的 `Unknowns` 用于说明为何需要人工接手，但不能伪装成已确认事实。

## 8. PlannerDecision、ToolCall 与 Observation

只保存 `Observation` 不足以还原一次诊断。三类记录分别回答不同问题：

```text
PlannerDecision：LLM 或 SOP 想做什么，以及为什么
ToolCall：程序最终允许并实际尝试做了什么
Observation：工具真正观察到了什么
```

最小关联方式是：

```text
PlannerDecision.ID
        ↓
ToolCall.DecisionID
        ↓
Observation.ToolCallID
        ↓
Observation.Facts[].ID
        ↓
DiagnosisReport.EvidenceRefs
```

通过这条链可以追溯：

```text
为什么查
→ 是否获准
→ 实际查了什么
→ 返回了什么
→ 产生了哪些最小事实
→ 最终报告引用了什么
```

三类记录建议分别保存在数组中，并通过 ID 关联，而不是强行嵌套。因为一个 Decision 可能被拒绝而没有 Observation，未来也可能出现一个 Decision 对应多个实际调用。

## 9. ToolCall 的状态和策略拒绝

`ToolCall` 更准确地代表一次“工具调用尝试”。即使 Policy Checker 拒绝，仍然创建记录并标记 `rejected`。

状态确定为：

```text
pending：已创建，尚未校验
rejected：策略拒绝，工具未执行
executing：已经通过校验并进入 Executor
succeeded：工具执行成功
failed：工具执行失败
timeout：工具执行超时
```

示例：

```text
ToolCallID: call-003
DecisionID: decision-003
ToolName: query_gpu_processes
RequestedArguments:
  GPUID: GPU-2
TargetID: host-01  # 由 Orchestrator 从 Scope 注入
Status: rejected
Error:
  Code: gpu_out_of_scope
```

被拒绝的 `ToolCall` 不产生正常 `Observation`，因为工具根本没有读取机器：

```text
rejected：不允许观察，因此没有执行，也没有 Observation
timeout：允许且尝试观察，但未取得结果，可以产生失败 Observation
```

每个语义化工具使用专属参数类型，不使用 `map[string]any`。例如 `QueryGPUProcessesArgs` 明确要求 `GPUID`，`QueryXIDEventsArgs` 明确限制 `SinceMinutes` 和 `Limit`。Planner 不能提供 Shell、SSH 地址、任意日志路径或底层命令参数。

参数通过类型、范围和 Scope 校验后可规范化为内部标准形式，例如将允许的 `0` 或 `gpu-0` 统一为 `GPU-0`。ToolCall 同时保存请求参数和最终执行参数，不能静默改写而不留记录。

状态与错误必须满足：`pending`、`executing`、`succeeded` 时 Error 为空；`rejected`、`failed`、`timeout` 时 Error 必须存在。Error 使用固定 `ErrorCode` 和经过脱敏的人类可读 `Message`，是否允许重试由程序根据 ErrorCode 计算，不再保存可能与 Code 冲突的 `Retryable` 字段。

### 9.1 重复调用指纹

通过校验和规范化后，程序使用以下内容生成稳定调用指纹：

```text
CallFingerprint = ToolName + TargetID + NormalizedArguments
```

同一指纹已经成功时禁止再次执行；上一次为 `failed` 或 `timeout` 时最多允许重试一次；同一指纹连续失败两次后停止重试并形成 Unknown。Policy 拒绝不计入工具失败重试，但受连续拒绝上限约束。

## 10. Planner 轮次与执行预算

第一版应区分：

```text
PlannerRounds：Planner 做出决策的次数
ExecutionAttempts：通过 Policy、真正进入 Executor 的次数
RejectedCalls：被 Policy 拒绝的尝试次数
```

一次被拒绝的请求：

```text
PlannerRounds +1
ExecutionAttempts 不增加
RejectedCalls +1
```

拒绝虽然不消耗实际工具执行预算，但仍消耗模型调用和运行时间。第一条高显存演示路径采用以下默认 Limits：

```text
MaxPlannerRounds = 6
MaxExecutionAttempts = 4
MaxConsecutiveRejectedCalls = 2
MaxConsecutiveFailures = 2
MaxDuration = 30s
```

这些数字是本地 MVP 的初始默认值，可在测试后调整；关键约束是不同计数必须分开，由外部程序强制执行，Planner 只能查看剩余预算，不能修改、增加或重置预算。

连续越界或无效请求可触发：

```text
StopReason = repeated_policy_rejection
```

## 11. Raw Result 到 Observation

工具的底层原始结果不能直接由 LLM 自由改写成事实。推荐链路是：

```text
固定只读命令或 Mock 查询
→ Raw Result
→ Go 确定性 Parser / Adapter
→ 类型和范围校验
→ Observation.Facts
```

例如固定的 `nvidia-smi` CSV 输出，应由确定性 Go 解析器转换成带类型的进程数据，而不是让 LLM 从命令文本中自行提取事实。

第一版 `Observation` 同时保留三类不同用途的结果：

```text
Observation
├── Data   # 工具专属的强类型结构，供程序判断和裁剪后供 Planner 使用
├── Facts  # 可被报告精确引用的最小事实
└── Raw    # 供人工复核和解析器排错的受限原始输出
```

`ObservationData` 采用显式受限类型，而不是 `any`：

```text
ObservationData
├── Type
├── GPUStatusData
├── GPUProcessesData
└── 后续白名单工具的专属 Data
```

任何时刻只能有一个专属 Data 非空，且必须与 Type 对应。新增白名单工具时显式增加它的参数类型和结果类型。

工具结果进一步拆成可独立引用的 `ObservedFact`：

```text
ObservedFact
├── ID
├── SubjectType
├── SubjectID
├── Key
├── Value
└── Unit
```

例如：

```text
FactID: fact-002
SubjectType: gpu
SubjectID: GPU-0
Key: memory_used_mb
Value: integer(23500)
Unit: MiB
```

`ObservedFact.Value` 也采用显式受限类型：`integer`、`decimal`、`text`、`boolean`。四种值字段只能有一个非空并必须与 Kind 对应；通过构造函数创建，并在 JSON、Mock 或工具边界再次 `Validate()`。Unit 单独保存在 Fact 中。不同工具可以定义不同的 `Key`，但 Key 必须由工具的 Go 类型或 Schema 约束，不能让 LLM 临时创造。

三类结果分别服务于不同对象：

```text
Data：供程序判断，并由 Context Builder 裁剪后供 Planner 使用
Facts：供测试、Evidence 和报告引用
Raw：供人工复核和解析器排错
```

Raw 结构确定为 `Content`、`Truncated`、`OriginalSizeBytes`、`Redacted` 和 `Digest`。第一版执行器最多接收 64 KiB，Observation 最多保存 8 KiB 脱敏 Raw；Digest 针对脱敏后、截断前的安全内容计算。完整 Raw 默认不进入 `PlannerContext`，报告不能直接引用 Raw 行文本。

处理顺序为：执行器限制接收量 → Parser 生成 Data → Adapter 生成 Facts → Raw 脱敏 → 计算 Digest → 截断保存。超过执行器硬上限时必须显式形成 partial 或 failed，不能静默丢弃后假装完整成功。

工具执行成功不等于解析成功：

```text
命令成功执行
≠
系统成功理解命令结果
```

如果命令成功但解析失败，可以记录：

```text
ToolCall.Status = succeeded
Observation.Status = failed 或 partial
Observation.Error.Code = parse_failed
Observation.Facts = 空或部分结构化结果
Observation.Raw = 受限原始输出
```

一次失败或超时仍可形成失败 `Observation`，它表达“本次观察没有成功”，而不是“机器已经确认存在故障”。

`ObservationStatus` 确定为 `succeeded`、`partial`、`failed`。`succeeded` 时 Error 必须为空；`partial` 和 `failed` 时 Error 必须存在。执行成功但解析失败可以表现为 `ToolCall.Status=succeeded`、`Observation.Status=failed`、`Observation.Error.Code=parse_failed`。

## 12. PlannerContext

### 12.1 核心定义

`PlannerContext` 是 Context Builder 从完整 `DiagnosisState` 中筛选、裁剪和计算出的本轮只读决策视图，不是第二份诊断存档。

```text
DiagnosisState
→ Context Builder
→ PlannerContext
→ Planner
```

### 12.2 Planner 应该看到的信息

第一版最小内容可以是：

```text
PlannerContext
├── DiagnosisID
├── AlertSummary
├── Scope
├── Mode
├── CurrentRound
├── RemainingBudget
├── Observations
├── PreviousCallSummaries
├── AvailableTools
├── ForbiddenDuplicateCalls
└── Instructions
```

Planner 需要看到：

- 当前任务目标和经过校验的调查范围；
- 已有结构化 Observation；
- 之前哪些调用成功、失败、超时或被拒绝；
- 当前可用的白名单工具及参数 Schema；
- 当前轮次、剩余预算和禁止重复的调用；
- 只允许 `call_tool`、`finish`、`escalate` 的输出约束。

让 LLM 知道约束不等于把控制权交给 LLM。它可以看到 `RemainingBudget = 1`，但不能修改预算。

### 12.3 应该隐藏的信息

Planner 不需要看到或不能提前看到：

- Orchestrator、Loop Guard 和 Policy Checker 的内部实现；
- 状态转移函数和 Executor 连接池；
- SSH 地址、账号、凭据等敏感执行细节；
- 未经工具查询的完整 Mock Machine State；
- 其他主机或 Scope 之外的信息；
- 无限制的 Raw 输出；
- 与本轮决策无关的系统日志；
- 完整 LLM 聊天历史。

尤其不能把完整 Mock 真相提前放进 PlannerContext，否则 Agent 无需调用工具就知道答案，诊断演示和评测都会失真。

## 13. PlannerDecision 最小输出协议

`PlannerDecision` 只负责建议下一步，不等同于最终 `DiagnosisReport`。它采用一个 `DecisionType` 区分三种不同形状：

```text
DecisionType = call_tool | finish | escalate
```

### 13.1 call_tool

```json
{
  "decision_type": "call_tool",
  "tool_name": "query_gpu_processes",
  "arguments": {
    "target_id": "host-01",
    "gpu_id": "GPU-0"
  },
  "reason": "obs-001 显示 GPU-0 显存接近上限，需要确认主要占用进程"
}
```

必填：`tool_name`、`arguments`、`reason`。

### 13.2 finish

```json
{
  "decision_type": "finish",
  "reason": "高显存现象及主要直接占用来源已经定位，继续查询其他信息的价值较低",
  "evidence_refs": ["obs-001", "obs-002"]
}
```

`finish` 不能是无依据的空结束，至少需要结束理由和已有 Observation 引用。程序仍需校验引用是否存在、是否属于本次诊断以及是否能支持有限结论。

### 13.3 escalate

```json
{
  "decision_type": "escalate",
  "reason": "进程查询连续超时，当前无法确认高显存的直接占用来源",
  "evidence_refs": ["obs-001", "obs-002"],
  "unknowns": [
    "GPU-0 的主要显存占用进程",
    "当前占用是否符合任务预期"
  ]
}
```

`escalate` 主要说明为什么无法继续、已有何种依据以及还缺什么，不应把模型自己的猜测写成事实。第一版暂不加入独立的 `hypotheses` 字段，以控制复杂度。

### 13.4 条件校验

概念上三种动作是三种不同结构。即使 Go 第一版为了简单使用一个结构体，也必须根据 `DecisionType` 做条件校验：

```text
call_tool：
  必须有 ToolName、Arguments、Reason
  禁止填写 EvidenceRefs、Unknowns

finish：
  必须有 Reason、EvidenceRefs
  禁止填写 ToolName、Arguments、Unknowns

escalate：
  必须有 Reason、Unknowns
  EvidenceRefs 可以有
  禁止填写 ToolName、Arguments
```

Planner 可以通过 Prompt、原生 Tool Calling 或结构化输出 Schema 尽量生成正确格式；外部 Decision Validator 仍必须检查能否解析、必填字段、枚举、字段组合和引用合法性。有限重试后仍无法解析，应安全终止并记录 `planner_invalid_output`。

## 14. 三轮模糊告警示例

输入：

```text
Alert ID: alert-001
Target: host-01
GPU: 未指定
Type: gpu_abnormal
Severity: warning
Message: GPU resource usage appears abnormal
```

Mock 真相是 GPU 0 显存接近上限，PID 4321 占用其中绝大部分，但 Planner 起初看不到 Mock 真相。

第一轮：

```text
PlannerDecision(call_tool: query_gpu_status)
→ ToolCall(call-001)
→ Observation(obs-001: GPU 0 显存接近上限)
```

第二轮：

```text
Planner 根据 obs-001 选择 query_gpu_processes
→ ToolCall(call-002)
→ Observation(obs-002: PID 4321 占用约 22 GB)
```

第三轮：

```text
PlannerDecision(
  finish,
  evidence_refs=[obs-001, obs-002]
)
```

系统可以确认高显存现象和主要直接占用来源，但不能据此确认内存泄漏，也不能判断是否应该终止进程。

## 15. ObservedFact 与 EvidenceRef

### 15.1 为什么不使用通用 FactPath

只引用 `ObservationID` 无法说明报告具体使用了 Observation 中哪个字段；通用 JSONPath 或数组下标虽然表达能力强，但会引入路径解析和数组顺序稳定性问题。

第一版不实现自由形式的 `FactPath`，而是在 Observation 生成时，由确定性 Adapter 把可引用结果拆成带唯一 ID 的最小事实：

```text
Observation
└── Facts []ObservedFact
    ├── FactID
    ├── SubjectType
    ├── SubjectID
    ├── Key
    ├── Value
    └── Unit
```

可以把它理解为：

```text
SubjectType + SubjectID：哪个对象
Key：对象的哪个属性
Value + Unit：本次观察到的值
```

Evidence 不再解析行列或路径，而是直接引用 Fact：

```text
EvidenceRef
├── ObservationID
└── FactID
```

`ObservationID + FactID` 是引用身份；`SubjectType + SubjectID + Key + Value + Unit` 是被引用事实的内容。

### 15.2 事实、确定性计算和推断

必须区分：

```text
工具观察：GPU-0 memory_used_mb = 23500 MiB
确定性计算：23500 / 24576 ≈ 95.6%
报告推断：GPU-0 存在显存压力
```

确定性派生值必须由程序根据明确输入和规则计算，不能由 LLM 伪装成机器观察。没有阈值规则时，“过热”“异常”等判断只能进入推断，不能进入已确认事实。

## 16. DiagnosisReport 最小结构

第一版报告结构为：

```text
DiagnosisReport
├── DiagnosisID
├── Outcome
├── ConfirmedFindings
├── Inferences
├── Unknowns
├── Recommendations
└── Termination
```

候选 Outcome：

```text
issue_identified
no_issue_found
inconclusive
escalated
```

各部分职责如下：

```text
ObservedFact：工具产生的最小机器事实
ConfirmedFinding：报告对一个或多个 Fact 的确定性、可读表达
Inference：基于一个或多个 Fact 的有限推断，并携带置信度
Unknown：当前无法确认的事项及原因
Recommendation：供人工考虑的下一步，不能伪装成已执行操作
Termination：程序停止调查的原因
```

`ConfirmedFinding` 和 `Inference` 都必须携带 `EvidenceRef`。没有合法证据引用的自然语言不能进入 ConfirmedFindings。

第一版各子结构确定为：

```text
ConfirmedFinding
├── Text
└── EvidenceRefs

Inference
├── Text
├── Confidence: low | medium | high
└── EvidenceRefs

Unknown
├── Text
├── Reason
├── RelatedToolCallIDs
└── RelatedObservationIDs

Recommendation
├── Text
├── Reason
└── EvidenceRefs
```

Unknown 不强制使用 `EvidenceRef`，因为失败 Observation 可能没有 Fact，Policy 拒绝甚至不会产生 Observation；它通过 ToolCallID 和 ObservationID 精确说明“为什么无法确认”。Recommendation 只能描述供人工考虑的下一步，必须说明依据，不能声称系统已经执行状态修改。

报告只辅助人工理解调查结果。终止进程、GPU reset、重载驱动、隔离节点或重启主机仍全部由人工决定和执行。

## 17. 报告生成和证据校验边界

报告链路为：

```text
DiagnosisState
→ ReportContext Builder
→ ReportContext
→ 生成 DraftReport
→ 确定性校验
→ 可选的 LLM 语义复核
→ Final DiagnosisReport
→ Human Operator
```

`PlannerContext` 回答“下一步查什么”；`ReportContext` 回答“已经查完，现有证据能够写出什么报告”。两者不是同一份上下文。

确定性程序负责检查：

- Observation 和 Fact 是否存在且属于本次诊断；
- Fact 是否属于被引用的 Observation；
- Observation 是否成功或部分成功；
- Target、GPU 等实体是否位于 Scope 内；
- Value、Unit 和引用组合是否合法；
- Outcome 与 Termination 是否冲突；
- 报告是否声称执行了系统不具备的恢复动作。

第一版尽量由程序根据 Fact 和固定模板生成 `ConfirmedFindings`，避免 LLM 抄错数值。LLM 主要负责关联多条事实形成有限 `Inferences`，以及组织 Unknowns 和人工建议。

“一句自然语言是否真正被证据支持”属于语义问题。后续可以增加独立 Evidence Reviewer，让 LLM 返回：

```text
supported
partially_supported
unsupported
```

但 LLM 复核只是软校验，不是确定性证明，也不能替代人工对高风险操作的最终判断。为了保持 MVP 主线清晰，第二次 LLM 复核不是第一版必做内容。

## 18. DiagnosisState 状态转移

状态生命周期确定为：

```text
initialized
    ↓ StartDiagnosis
running
    ↓ StopDiagnosis(reason)
reporting
    ↓ FinalizeReport
finished
```

只有 Orchestrator 可以真正修改生命周期：

```text
Planner：建议 call_tool / finish / escalate
Policy Checker：接受或拒绝 ToolCall
Loop Guard：提供强制停止原因
Orchestrator：校验结果并推进或终止 DiagnosisState
```

Planner 返回 `finish` 不会直接把状态改成 `finished`。Orchestrator 必须先校验证据和结束条件，再进入 `reporting`，生成并校验报告后才进入 `finished`。

第一版显式操作候选为：

```text
StartDiagnosis
RecordDecision
RecordToolCall
RecordObservation
StopDiagnosis
FinalizeReport
```

## 19. 第一版 Go 包结构

推荐保持小而清晰：

```text
cmd/gpu-agent/       CLI 入口和依赖组装
internal/model/      共同数据结构和枚举
internal/diagnosis/  Orchestrator、状态转移、Loop Guard 和接口
internal/planner/    确定性 Planner；后续增加受限 LLM Planner
internal/tools/      Registry、Policy、Executor 和语义化工具
internal/mock/       统一 Mock Machine State、场景和工具适配器
internal/report/     ReportContext、报告构建和确定性校验
scenarios/           可重复运行的 Mock 场景数据
```

共同结构放在中立的 `internal/model`，避免 `diagnosis`、`tools`、`planner`、`report` 之间循环导入。`model` 只保存数据和枚举，不控制流程。

Orchestrator 依赖 Planner、ToolExecutor 和 ReportBuilder 接口，不绑定具体 Mock 或 LLM 实现。`main.go` 负责创建具体实现并注入 Orchestrator。

第一条高显存路径已经使用 Deterministic Planner 验证状态循环、工具调用、证据链、报告和终止机制。下一阶段先扩充少量只读工具、统一 Mock 场景和结构化 SOP；出现多个合法调查方向后，再在同一 Planner 接口后增加受限 LLM Planner。

## 20. 本轮确定的可编码协议

当前讨论已经形成以下可直接进入编码验证的共识：

1. `DiagnosisState` 保存整次诊断的累计结构化状态，不主动推进流程。
2. Alert 是外部输入，经过校验的 `Scope` 才是本次诊断的执行边界。
3. LLM 只提出 `PlannerDecision`，不能直接执行工具或产生机器事实。
4. `PlannerDecision`、`ToolCall`、`Observation` 分别保存意图、执行尝试和现场观察。
5. 被 Policy 拒绝的请求仍创建 `ToolCall(rejected)`，但不产生正常 Observation。
6. 通过 Policy 但执行失败或超时，可以产生失败 Observation，表示状态无法确认。
7. Raw Result 由确定性 Parser / Adapter 转换为 `ObservedFact`；LLM 不负责把命令文本改写成事实。
8. Evidence 通过 `ObservationID + FactID` 精确引用事实，第一版不实现通用 FactPath。
9. 已确认事实尽量由程序生成；LLM 负责有限推断和解释；程序校验引用，人工决定高风险操作。
10. `Status`、`Termination.Reason` 和 `DiagnosisReport.Outcome` 分别表示生命周期、停止原因和调查结果。
11. Planner、Policy 和 Loop Guard 都不能直接修改生命周期，只有 Orchestrator 可以推进状态。
12. 第一版使用确定性 Planner 跑通闭环，再接入受限 LLM Planner。
13. `FactValue` 只允许 `integer`、`decimal`、`text`、`boolean`，值字段必须与 Kind 唯一对应。
14. `ObservationData` 显式列出白名单工具的专属强类型 Data，任何时刻只能有一个类型与内容匹配的 Data。
15. `ObservationStatus` 为 `succeeded | partial | failed`；`ToolCallStatus` 为 `pending | rejected | executing | succeeded | failed | timeout`，Status 与 Error 组合必须通过校验。
16. `GPUAccessMode` 为 `all | selected`，禁止用空数组隐式表示两种相反权限；第一版 TargetID 由 Orchestrator 从 Scope 注入。
17. 每个工具使用专属参数类型；调用指纹由工具名、TargetID 和规范化参数生成。成功调用禁止重复，失败或超时最多重试一次。
18. 第一版默认预算为 6 个 Planner 轮次、4 次实际执行、连续 2 次策略拒绝、连续 2 次工具失败和 30 秒总时长。
19. Error 使用固定 Code 和安全 Message，重试策略由 Code 派生；Raw 执行接收上限 64 KiB、保存上限 8 KiB，并经过脱敏、摘要和截断标记。
20. 系统 ID 由程序生成且创建后不可修改；`Decision → ToolCall → Observation → Fact → Report` 通过 ID 串联，EvidenceRef 同时保存 ObservationID 和 FactID。
21. DiagnosisState 保存源数据和追加式历史；PlannerRounds、ExecutionAttempts、RejectedCalls、连续失败和剩余预算均从历史动态计算。
22. 报告严格区分 ConfirmedFinding、Inference、Unknown 和 Recommendation；固定模板优先生成已确认事实，未知项可以引用失败 ToolCall 和 Observation。

## 21. 已实现的第一条高显存编码验收场景

第一条路径使用模糊主机级 GPU 告警：

```text
AlertID: alert-001
TargetID: host-01
GPUID: 未指定
Type: gpu_abnormal
Severity: warning
Message: GPU resource usage appears abnormal
```

校验后生成 `Scope(TargetID=host-01, GPUAccessMode=all)`。统一 Mock Machine State 至少包含：

```text
GPU-0: total=24576 MiB, used=23500 MiB, utilization=92%, temperature=72 C
GPU-1: total=24576 MiB, used=1200 MiB, utilization=15%, temperature=46 C
PID-4321: GPU-0, python, 22000 MiB
PID-5678: GPU-0, monitor, 300 MiB
```

Mock 必须满足：同一 GPU 的进程显存总和不大于 GPU 已用显存，GPU 已用显存不大于总显存。进程总和无需等于已用显存，因为驱动与上下文也可能占用显存。

确定性 Planner 使用演示规则 `memory_used / memory_total >= 90%` 选择进程查询。该阈值只用于稳定演示分支，不宣称为所有生产环境的通用标准。

预期路径严格为：

```text
Round 1: query_gpu_status(all registered GPUs)
Round 2: query_gpu_processes(GPU-0)
Round 3: finish
```

不查询 GPU-1 的进程，因为已有 Observation 没有显示该检查具有价值。最终状态为：

```text
Status = finished
TerminationReason = evidence_sufficient
Outcome = issue_identified
PlannerRounds = 3
ExecutionAttempts = 2
RejectedCalls = 0
```

ConfirmedFindings 只确认 GPU-0 的已用/总显存以及 PID-4321 的显存占用；Inferences 可以说明 GPU-0 存在显存压力、PID-4321 是本次观察中的主要直接占用来源；Unknowns 必须说明是否符合任务预期、是否存在持续增长或内存泄漏仍无法确认；Recommendations 只要求人工确认任务合理性。

报告禁止声称 `memory leak confirmed`、`process terminated`、`GPU reset completed`，也不能声称 GPU-1 绝对没有任何问题。系统只调查和建议，不执行任何修改操作。

自动测试至少覆盖：

1. 工具调用顺序和精确次数；
2. 状态、Termination 和 Outcome；
3. EvidenceRef 的 Observation/Fact 归属；
4. Scope、白名单和无状态修改工具的安全边界；
5. Mock 多工具结果的一致性；
6. 报告不得越过证据边界；
7. 同一 Alert 和 Mock State 产生相同路径、Facts 和报告结果。

测试通过注入固定 Clock 和 ID Generator 消除时间与 ID 的不确定性。

截至当前版本，这条路径已经在 Go 中跑通，并通过单元测试、race 检查、`go vet`、CLI 构建和端到端确定性测试。它是后续场景复用的基线，不应在扩展其他场景时重写。

## 22. 已实现的第二条 Xid / 掉卡诊断切片

### 22.1 为什么先实现这一条

实现 Xid / 掉卡场景时没有直接接入 LLM，因为当时只有两个工具和一个高显存分支，LLM 即使接入也几乎只能复述固定路径，无法证明动态调查的价值。

Xid 切片可以在不改变现有架构的情况下验证：

- 同一个统一 Mock 中，设备状态、驱动、Xid 事件和内核日志能否保持一致；
- Planner 能否根据前一项 Observation 决定后续检查，而不是固定采集所有信息；
- 多个 Observation 产生的 Fact 能否共同支持一个有限推断；
- 系统能否确认可观察事件，同时拒绝推断永久硬件损坏或自动执行恢复动作。

本切片已经使用 Deterministic Planner 跑通。它稳定了底层能力和分支测试，没有在同一切片中同时引入 LLM 不确定性。

### 22.2 端到端运行路径

第二条演示仍从模糊主机级告警开始：

```text
Mock Alert(gpu_abnormal)
→ query_gpu_status
→ Observation: GPU-0 当前 unavailable，GPU-1 online
→ query_driver_status
→ Observation: 驱动已加载且 NVML 可用
→ query_xid_events(GPU-0, 最近 30 分钟)
→ Observation: GPU-0 出现 Xid 79
→ query_recent_kernel_logs(GPU-0, 最近 30 分钟)
→ Observation: 内核日志存在与 GPU-0/Xid 79 对应的 NVRM 事件
→ finish
→ 输出事实、有限推断、未知项和人工建议
```

这里的判断边界是：驱动全局可用、GPU-1 仍在线，而 GPU-0 当前不可用并出现 Xid 79，因此现有证据与 GPU-0 的主机通信丢失一致。系统不能据此确认 GPU-0 已永久损坏，也不能决定或执行 reset、驱动重载、节点隔离或重启。

具体一轮运行如下：

```text
Round 1: Planner 请求 query_gpu_status
Round 2: 根据 GPU-0 unavailable，请求 query_driver_status
Round 3: 根据驱动已加载且 NVML 可用，请求 query_xid_events(GPU-0)
Round 4: 根据 Xid 79，请求 query_recent_kernel_logs(GPU-0)
Round 5: 证据足够，finish
```

预期计数：

```text
Status = finished
TerminationReason = evidence_sufficient
Outcome = issue_identified
PlannerRounds = 5
ExecutionAttempts = 4
RejectedCalls = 0
```

`MaxExecutionAttempts=4` 表示最多允许四次实际工具执行，不应阻止第四次执行完成后由 Planner 提交不执行工具的 `finish` 或 `escalate`。实现本切片时需要用测试校准当前 Loop Guard：预算为零时禁止新的 `call_tool`，但仍允许形成终止决策。

### 22.3 统一 Mock Machine State 扩展

现有 `MachineState` 继续作为所有工具的唯一真相来源，不为每个工具分别准备互不相关的返回值。新增概念结构为：

```text
MachineState
├── TargetID
├── GPUs map[GPUID]GPU
├── Processes []Process
├── Driver DriverState
├── XIDEvents []XIDEvent
└── KernelLogs []KernelLogEntry
```

`GPU` 增加机器可观察的可用状态：

```text
GPU
├── ID
├── Availability: online | unavailable
├── MemoryTotalMB
├── MemoryUsedMB
├── Utilization
└── TemperatureC
```

`Availability` 只描述当前查询是否能够访问该设备，不直接表达“健康”“损坏”或根因。`online` 时现有数值字段必须满足范围约束；`unavailable` 时数值字段不作为有效测量，统一为零，防止报告同时声称“设备不可访问”又引用看似精确的实时利用率。

驱动状态：

```text
DriverState
├── Loaded bool
├── Version string
└── NVMLAvailable bool
```

约束：`Loaded=false` 时 `Version` 必须为空且 `NVMLAvailable=false`；`Loaded=true` 时 `Version` 必须非空。驱动已加载不等于所有 GPU 正常，NVML 可用也不等于单个设备一定可访问。

Xid 事件：

```text
XIDEvent
├── ID
├── GPUID
├── Code int
├── OccurredAt time.Time
└── Summary string
```

内核日志：

```text
KernelLogEntry
├── ID
├── GPUID
├── OccurredAt time.Time
├── Severity: info | warning | error
├── Component string
├── Message string
└── RelatedXIDCode *int
```

Mock 校验至少保证：

- Xid 和 KernelLog 引用的 GPU 必须存在于同一 `GPUs` 清单；
- ID 唯一、时间非零、Xid code 为正数；
- `RelatedXIDCode` 非空时，必须能与同一 GPU、相近时间窗口内的 Xid 事件对应；
- 查询结果按 `OccurredAt` 倒序、ID 次序做稳定排序；
- 高显存场景也补齐正常 DriverState，但不伪造 Xid 或错误日志。

### 22.4 第二条具体 Mock 场景

```text
AlertID: alert-xid-001
TargetID: host-01
GPUID: 未指定
Type: gpu_abnormal
Severity: critical
Message: One registered GPU is unavailable
```

校验后仍生成：

```text
Scope(TargetID=host-01, GPUAccessMode=all)
```

Mock 状态固定为：

```text
GPU-0: availability=unavailable, metrics=0
GPU-1: availability=online, total=24576 MiB, used=1200 MiB,
       utilization=15%, temperature=46 C

Driver: loaded=true, version=550.54.15, nvml_available=true

XIDEvent:
  id=xid-event-001
  gpu_id=GPU-0
  code=79
  occurred_at=2026-08-20T09:58:00+08:00
  summary="GPU has fallen off the bus"

KernelLogEntry:
  id=kernel-log-001
  gpu_id=GPU-0
  occurred_at=2026-08-20T09:58:01+08:00
  severity=error
  component=NVRM
  related_xid_code=79
  message="NVRM reported Xid 79 for GPU-0"
```

这些固定值用于稳定演示和测试，不代表生产环境下 Xid 79 只有一种根因。

### 22.5 新增工具参数与策略边界

三个工具都由 Orchestrator 从 Scope 注入 `TargetID`，Planner 不填写 Target，也不能提供命令、日志路径、正则表达式或任意过滤字符串。

```text
QueryDriverStatusArgs
  无 Planner 参数

QueryXIDEventsArgs
├── GPUID string          # 必填，必须属于 Scope
├── SinceMinutes int      # 1..1440
└── Limit int             # 1..100

QueryRecentKernelLogsArgs
├── GPUID string          # 必填，必须属于 Scope
├── SinceMinutes int      # 1..1440
└── Limit int             # 1..200
```

第一条 Xid 演示使用：

```text
query_xid_events(GPU-0, since_minutes=30, limit=20)
query_recent_kernel_logs(GPU-0, since_minutes=30, limit=50)
```

时间范围相对于本次运行注入的固定 Clock 计算。规范化后的参数进入 `ExecutedArguments` 和调用指纹；越界参数、Scope 外 GPU、重复成功调用继续由 Policy 拒绝并保留审计记录。

### 22.6 ObservationData 强类型扩展

`ObservationData` 显式增加三个互斥载荷：

```text
ObservationData
├── Type: gpu_status | gpu_processes | driver_status | xid_events | kernel_logs
├── GPUStatusData
├── GPUProcessesData
├── DriverStatusData
├── XIDEventsData
└── KernelLogsData
```

新增 Data：

```text
DriverStatusData
├── Loaded bool
├── Version string
└── NVMLAvailable bool

XIDEventsData
├── GPUID string
├── SinceMinutes int
└── Events []XIDEvent

KernelLogsData
├── GPUID string
├── SinceMinutes int
└── Entries []KernelLogEntry
```

仍保持“任何时刻只有一个专属 Data 非空，且必须与 Type 对应”的不变量。

### 22.7 可引用 ObservedFact

工具 Adapter 只生成预先定义的 Key，不允许 Planner 或 LLM 临时创建：

```text
query_gpu_status
  Subject=gpu/<GPUID>
  availability              text
  memory_total_mb           integer, MiB   # 仅 online 时生成
  memory_used_mb            integer, MiB   # 仅 online 时生成
  utilization_percent       decimal, %     # 仅 online 时生成
  temperature_c             decimal, C     # 仅 online 时生成

query_driver_status
  Subject=driver/nvidia
  loaded                    boolean
  version                   text           # 仅 loaded 时生成
  nvml_available            boolean

query_xid_events
  Subject=xid_event/<EventID>
  gpu_id                     text
  code                       integer
  occurred_at                text, RFC3339
  summary                    text

query_recent_kernel_logs
  Subject=kernel_log/<EntryID>
  gpu_id                     text
  occurred_at                text, RFC3339
  severity                   text
  component                  text
  message                    text
  related_xid_code           integer        # 仅有关联值时生成
```

`availability=unavailable` 是工具观察事实；“GPU 永久损坏”不是 Fact。`Xid code=79` 和对应日志是事件事实；“需要换卡”不是 Fact。

### 22.8 Planner 分支规则

本切片的 Deterministic Planner 只增加足以演示的规则：

```text
尚无 GPU status
  → query_gpu_status

存在 unavailable GPU，尚无 driver status
  → query_driver_status

driver loaded 且 NVML available，目标 GPU 尚无 Xid 查询
  → query_xid_events

发现 Xid 事件，尚无对应 kernel log 查询
  → query_recent_kernel_logs

Xid 与日志相互印证
  → finish

driver 未加载或 NVML 不可用
  → 不继续伪装成可读取单 GPU Xid；整理驱动事实并 escalate

Xid 与日志均没有提供足够证据
  → 明确 unknown 并 escalate
```

如果多个 GPU 同时 unavailable，第一版不自行选择任意一个深入调查，而是按稳定 GPU ID 顺序并结合剩余预算处理；预算不足以覆盖时必须升级人工，不能把未调查设备写成正常。

### 22.9 报告允许与禁止的内容

`ConfirmedFindings` 可以写：

- GPU-0 在本次状态查询中为 `unavailable`；
- NVIDIA 驱动已加载、版本为 `550.54.15`、NVML 可用；
- GPU-0 在指定时间窗口内出现 Xid 79；
- 内核日志存在与 GPU-0、Xid 79 对应的 NVRM 事件。

`Inferences` 可以有限写：

- 多项证据与 GPU-0 在该时刻发生主机通信丢失一致；
- 由于驱动和 GPU-1 仍可用，现象更集中于 GPU-0，而不是已经确认的全局驱动失效。

`Unknowns` 必须保留：

- GPU-0 是否永久硬件损坏；
- 故障由硬件、PCIe 链路、供电、驱动或其他因素中的哪一种造成；
- 租户实际业务影响；
- reset、驱动重载或重启是否能够安全恢复。

`Recommendations` 只能建议人工：

- 复核租户任务和节点影响；
- 按正式运维流程评估隔离、迁移、reset、驱动处理或硬件检查；
- 在采取任何状态修改前保存并复核本次证据。

报告禁止声称：

```text
GPU-0 permanently failed
hardware replacement required
GPU reset completed
driver reloaded
node isolated
host rebooted
```

### 22.10 自动测试验收

当前实现和测试已经覆盖：

1. 新 Mock 字段和跨工具引用的一致性校验；
2. unavailable GPU 不生成伪造的实时指标 Fact；
3. 三个工具的专属参数类型、范围规范化和 Scope 拒绝；
4. 工具 Registry 仍然只包含只读语义化工具；
5. 精确路径为 `gpu_status → driver_status → xid_events → kernel_logs → finish`；
6. 第四次执行后仍允许 Planner 形成 `finish`，但不允许第五次工具执行；
7. Policy 拒绝不产生 Observation，工具失败才产生失败 Observation；
8. Xid、日志和报告的每个 EvidenceRef 都解析到正确 Fact；
9. 报告不确认永久损坏，不声称执行恢复动作；
10. 驱动不可用、无 Xid、日志不匹配等分支能够形成 Unknown 或 escalate；
11. 同一 Alert 与 Mock State 重复运行得到相同路径、Facts 和报告。

### 22.11 LLM 接入门槛

Xid 切片完成后仍不删除 Deterministic Planner。下一步先补高温或未知异常分支，再实现最小 SOP Router，使同类模糊告警在不同 Mock 状态下存在多个合法调查方向。之后再设计实际 `PlannerContext` JSON，并在现有 `Planner` 接口后增加 LLM 实现。

接入 LLM 时，现有 Orchestrator、Policy、Scope、预算、工具执行、Fact 生成、Evidence 校验和报告安全边界保持不变；LLM 只获得选择下一项白名单检查或建议结束/升级的权限。

## 23. 编码阶段允许通过测试微调的内容

架构取舍已经收口。编码时只允许在不改变上述边界的前提下微调：

- Go 字段名、文件拆分和构造函数命名；
- 为满足序列化而增加的机械字段；
- 枚举的具体字符串拼写；
- 默认阈值和 Limits 在测试证据支持下的小幅调整；
- 状态转移非法调用返回的具体 Go error 包装。

如果实现发现必须改变安全边界、证据语义、Planner 权限或报告责任，应停止编码并重新讨论，不能把它当作普通实现细节自行修改。

## 24. 新对话交接与下一轮起点

开始下一轮编码前先阅读：

1. 项目总纲 `AGENTS.md`；
2. 本文档 `docs/agent-runtime-and-diagnosis-state.md`。

开始工作时先运行：

```bash
git status --short
go test ./...
go test -race ./...
go vet ./...
```

当前已经实现并验证：

```text
high-memory:
  gpu_status → gpu_processes → finish

xid-drop:
  gpu_status → driver_status → xid_events → kernel_logs → finish
```

下一项具体工作不是接入 LLM，而是对第三条高温场景做一个小切片设计并立即编码：

```text
定义高温 Mock 的机器事实与一致性约束
→ 定义需要复用或新增的只读工具 Data 和 Fact Keys
→ 明确温度阈值只是本地演示规则
→ 明确报告可确认项、有限推断和未知项
→ 先写模型与 Mock 非法组合测试
→ 扩展 Deterministic Planner、报告和 CLI
→ 验证同类模糊告警在不同 Mock 下选择不同路径
```

设计高温切片时先判断现有 `query_gpu_status` 和 `query_recent_kernel_logs` 是否足够。只有确实需要新的机器事实时才增加 `query_host_resources`；不得为了增加工具数量而增加与结论无关的查询。

高温路径通过后，再实现最小 SOP Router。LLM Planner 的接入门槛和不允许接管的确定性责任见第 22.11 节与 `AGENTS.md` 第 11.5 节。
