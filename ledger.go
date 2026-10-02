// Package batchtrace 提供制造业批次追溯核算能力：
// 接收完整账本（单仓库单物料的入库批次、领料与已确认来源），
// 并回答"异常批次流向目标领料的合计数量上下界"查询。
package batchtrace

import (
	"errors"
	"fmt"
)

// ErrInfeasible 表示整份账本不存在合法分配，无法给出追溯区间。
var ErrInfeasible = errors.New("batchtrace: 账本不存在合法分配，无法解释")

// Batch 入库批次。数量为正整数（最小计量单位）。
// 可领用时间窗口为 [Arrival, Expiry)：到货时刻可领用，失效时刻不可领用。
type Batch struct {
	ID       string
	Quantity int64
	Arrival  int64
	Expiry   int64
}

// Issue 一笔生产领料。Candidates 为候选来源批次 ID 集合，
// 集合之外的批次不可用于该笔领料。
type Issue struct {
	ID         string
	Quantity   int64
	Time       int64
	Candidates []string
}

// Confirmation 批次与领料之间已确认至少发生的数量。
// 最终分配不得少于该数量，且同样受候选范围与时间限制。
type Confirmation struct {
	IssueID  string
	BatchID  string
	Quantity int64
}

// Allocation 一条分配记录：某批次流向某领料的数量。
type Allocation struct {
	IssueID  string
	BatchID  string
	Quantity int64
}

// Bounds 一次追溯查询的结果：异常批次流向目标领料的合计数量上下界。
// MinAllocation / MaxAllocation 分别为达到下界 / 上界的完整账本分配（核查依据），
// 覆盖账本上全部领料，包括查询范围之外的领料。
type Bounds struct {
	Min           int64
	Max           int64
	MinAllocation []Allocation
	MaxAllocation []Allocation
}

// Ledger 一份静态账本：单仓库单物料的全部入库批次、领料与确认记录。
// 只读，不提供修改能力；不同查询相互独立。
type Ledger struct {
	batches       []Batch
	issues        []Issue
	confirmations []Confirmation
	batchIndex    map[string]int // 批次 ID -> batches 下标
	issueIndex    map[string]int // 领料 ID -> issues 下标
	candidateSet  []map[int]bool // 每笔领料的候选批次下标集合（已去重）
}

// NewLedger 接收完整账本并做输入校验。
// 校验失败返回错误；账本是否可解释（存在合法分配）由 Query 判定。
func NewLedger(batches []Batch, issues []Issue, confirmations []Confirmation) (*Ledger, error) {
	l := &Ledger{
		batchIndex:   make(map[string]int, len(batches)),
		issueIndex:   make(map[string]int, len(issues)),
		candidateSet: make([]map[int]bool, len(issues)),
	}

	for i, b := range batches {
		if b.Quantity <= 0 {
			return nil, fmt.Errorf("batchtrace: 批次 %q 数量必须为正整数", b.ID)
		}
		if _, dup := l.batchIndex[b.ID]; dup {
			return nil, fmt.Errorf("batchtrace: 批次 ID %q 重复", b.ID)
		}
		l.batchIndex[b.ID] = i
	}
	l.batches = append([]Batch(nil), batches...)

	for i, is := range issues {
		if is.Quantity <= 0 {
			return nil, fmt.Errorf("batchtrace: 领料 %q 数量必须为正整数", is.ID)
		}
		if _, dup := l.issueIndex[is.ID]; dup {
			return nil, fmt.Errorf("batchtrace: 领料 ID %q 重复", is.ID)
		}
		l.issueIndex[is.ID] = i
		set := make(map[int]bool, len(is.Candidates))
		for _, bid := range is.Candidates {
			bi, ok := l.batchIndex[bid]
			if !ok {
				return nil, fmt.Errorf("batchtrace: 领料 %q 的候选批次 %q 不存在", is.ID, bid)
			}
			set[bi] = true
		}
		l.candidateSet[i] = set
	}
	l.issues = append([]Issue(nil), issues...)

	for _, c := range confirmations {
		if c.Quantity <= 0 {
			return nil, fmt.Errorf("batchtrace: 确认记录 (%q,%q) 数量必须为正整数", c.IssueID, c.BatchID)
		}
		ii, ok := l.issueIndex[c.IssueID]
		if !ok {
			return nil, fmt.Errorf("batchtrace: 确认记录引用的领料 %q 不存在", c.IssueID)
		}
		bi, ok := l.batchIndex[c.BatchID]
		if !ok {
			return nil, fmt.Errorf("batchtrace: 确认记录引用的批次 %q 不存在", c.BatchID)
		}
		if !l.candidateSet[ii][bi] {
			return nil, fmt.Errorf("batchtrace: 确认记录 (%q,%q) 超出候选来源范围", c.IssueID, c.BatchID)
		}
		if !usableAt(l.batches[bi], l.issues[ii].Time) {
			return nil, fmt.Errorf("batchtrace: 确认记录 (%q,%q) 不满足到货/失效时间限制", c.IssueID, c.BatchID)
		}
	}
	l.confirmations = append([]Confirmation(nil), confirmations...)

	return l, nil
}

// usableAt 批次 b 在时刻 t 是否可领用：已到货（含到货时刻）且未失效（不含失效时刻）。
func usableAt(b Batch, t int64) bool {
	return b.Arrival <= t && t < b.Expiry
}
