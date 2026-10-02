package batchtrace

import "fmt"

// Ledger 是一份完整账本：单一仓库、单一物料的
// 全部入库批次、全部领料与全部已确认来源记录。
// 账本为静态数据，查询不扣减库存、不固定历史，
// 同一账本上的各次查询相互独立。
type Ledger struct {
	batches  []Batch
	issues   []Issue
	confirms []Confirmation

	batchIndex map[string]int
	issueIndex map[string]int
}

// NewLedger 接收一份完整账本并做静态校验。
// 校验失败时返回描述性错误；入参切片会被拷贝，
// 之后调用方对入参的修改不影响账本。
func NewLedger(batches []Batch, issues []Issue, confirmations []Confirmation) (*Ledger, error) {
	if err := validateLedger(batches, issues, confirmations); err != nil {
		return nil, err
	}

	l := &Ledger{
		batches:    make([]Batch, len(batches)),
		issues:     make([]Issue, len(issues)),
		confirms:   make([]Confirmation, len(confirmations)),
		batchIndex: make(map[string]int, len(batches)),
		issueIndex: make(map[string]int, len(issues)),
	}
	copy(l.batches, batches)
	copy(l.confirms, confirmations)
	for i, b := range l.batches {
		l.batchIndex[b.ID] = i
	}
	for i, is := range issues {
		// 拷贝并去重候选来源，保持首次出现顺序
		cands := make([]string, 0, len(is.Candidates))
		seen := make(map[string]bool, len(is.Candidates))
		for _, bid := range is.Candidates {
			if !seen[bid] {
				seen[bid] = true
				cands = append(cands, bid)
			}
		}
		l.issues[i] = Issue{
			ID:         is.ID,
			Quantity:   is.Quantity,
			Time:       is.Time,
			Candidates: cands,
		}
		l.issueIndex[is.ID] = i
	}
	return l, nil
}

// Query 查询异常批次集合 anomalousBatches 流向目标领料集合
// targetIssues 的合计数量上下界，两端各附一份覆盖整份账本的
// 完整分配作为核查依据。查询不扣减库存、不固定历史。
// 若整份账本不存在合法分配，返回 ErrInfeasible。
func (l *Ledger) Query(anomalousBatches []string, targetIssues []string) (*Interval, error) {
	anomalous := make(map[string]bool, len(anomalousBatches))
	for _, id := range anomalousBatches {
		if _, ok := l.batchIndex[id]; !ok {
			return nil, fmt.Errorf("batchtrace: 查询的异常批次 %q 不在账本中", id)
		}
		anomalous[id] = true
	}
	targets := make(map[string]bool, len(targetIssues))
	for _, id := range targetIssues {
		if _, ok := l.issueIndex[id]; !ok {
			return nil, fmt.Errorf("batchtrace: 查询的目标领料 %q 不在账本中", id)
		}
		targets[id] = true
	}

	min, minWitness, err := l.solve(anomalous, targets, false)
	if err != nil {
		return nil, err
	}
	max, maxWitness, err := l.solve(anomalous, targets, true)
	if err != nil {
		return nil, err
	}
	return &Interval{
		Min:        min,
		Max:        max,
		MinWitness: minWitness,
		MaxWitness: maxWitness,
	}, nil
}
