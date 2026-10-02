// Package batchtrace 提供制造业批次追溯核算能力：
// 接收完整账本（单仓库单物料），并查询一组异常入库批次
// 流向一组目标领料的合计数量上下界，各附一份覆盖整份账本的
// 完整分配作为核查依据。
package batchtrace

import "errors"

// ErrInfeasible 表示整份账本不存在任何合法分配，无法给出追溯区间。
var ErrInfeasible = errors.New("batchtrace: 账本不存在合法分配，无法解释")

// Batch 是一个入库批次。数量以最小计量单位计，为正整数。
// 到货时刻 Arrival 起可领用（含），失效时刻 Expiry 起不可领用（不含）。
type Batch struct {
	ID       string
	Quantity int64
	Arrival  int64
	Expiry   int64
}

// Issue 是一笔生产领料。Time 为发生时刻；
// Candidates 列出该笔领料允许使用的候选来源批次，
// 候选集合之外的批次不可用于该笔领料。
type Issue struct {
	ID         string
	Quantity   int64
	Time       int64
	Candidates []string
}

// Confirmation 记录批次 BatchID 与领料 IssueID 之间
// 已确认至少发生的数量。最终分配不得少于该数量，但可以更多。
// 确认记录同样受候选范围与时间限制。
type Confirmation struct {
	BatchID  string
	IssueID  string
	Quantity int64
}

// Assignment 是完整分配中的一条：批次 BatchID 向领料 IssueID
// 实际分配了 Quantity 单位。
type Assignment struct {
	BatchID  string
	IssueID  string
	Quantity int64
}

// Interval 是一次追溯查询的结果：异常批次流向目标领料的
// 合计数量下界 Min 与上界 Max。MinWitness / MaxWitness 分别是
// 达到下界 / 上界的一份覆盖整份账本的完整分配。
type Interval struct {
	Min        int64
	Max        int64
	MinWitness []Assignment
	MaxWitness []Assignment
}
