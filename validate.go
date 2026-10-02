package batchtrace

import "fmt"

// usableAt 报告批次在时刻 t 是否可领用：
// 到货时刻（含）起可领用，失效时刻（含）起不可领用。
func usableAt(b Batch, t int64) bool {
	return b.Arrival <= t && t < b.Expiry
}

// validateLedger 对账本做静态校验：
//   - 数量必须为正整数；
//   - 批次、领料 ID 必须非空且各自唯一；
//   - 候选来源必须引用已存在的批次；
//   - 确认记录必须引用已存在的批次与领料、落在候选范围内、
//     满足时间限制，且不超过单笔批次/领料数量。
//
// 候选集合中时间不可用的批次不视为错误，只是不产生可用来源；
// 整体可行性（如总库存是否足够）不在此校验，由查询时判定。
func validateLedger(batches []Batch, issues []Issue, confirmations []Confirmation) error {
	batchIdx := make(map[string]int, len(batches))
	for i, b := range batches {
		if b.ID == "" {
			return fmt.Errorf("batchtrace: 第 %d 个入库批次 ID 为空", i)
		}
		if _, dup := batchIdx[b.ID]; dup {
			return fmt.Errorf("batchtrace: 入库批次 ID %q 重复", b.ID)
		}
		if b.Quantity <= 0 {
			return fmt.Errorf("batchtrace: 入库批次 %q 数量 %d 非正整数", b.ID, b.Quantity)
		}
		if b.Arrival > b.Expiry {
			return fmt.Errorf("batchtrace: 入库批次 %q 到货时刻 %d 晚于失效时刻 %d", b.ID, b.Arrival, b.Expiry)
		}
		batchIdx[b.ID] = i
	}

	issueIdx := make(map[string]int, len(issues))
	candidate := make([]map[string]bool, len(issues))
	for i, is := range issues {
		if is.ID == "" {
			return fmt.Errorf("batchtrace: 第 %d 笔领料 ID 为空", i)
		}
		if _, dup := issueIdx[is.ID]; dup {
			return fmt.Errorf("batchtrace: 领料 ID %q 重复", is.ID)
		}
		if is.Quantity <= 0 {
			return fmt.Errorf("batchtrace: 领料 %q 数量 %d 非正整数", is.ID, is.Quantity)
		}
		seen := make(map[string]bool, len(is.Candidates))
		for _, bid := range is.Candidates {
			if _, ok := batchIdx[bid]; !ok {
				return fmt.Errorf("batchtrace: 领料 %q 的候选来源批次 %q 不存在", is.ID, bid)
			}
			seen[bid] = true
		}
		candidate[i] = seen
		issueIdx[is.ID] = i
	}

	for i, c := range confirmations {
		bi, ok := batchIdx[c.BatchID]
		if !ok {
			return fmt.Errorf("batchtrace: 第 %d 条确认记录引用的批次 %q 不存在", i, c.BatchID)
		}
		ii, ok := issueIdx[c.IssueID]
		if !ok {
			return fmt.Errorf("batchtrace: 第 %d 条确认记录引用的领料 %q 不存在", i, c.IssueID)
		}
		if c.Quantity <= 0 {
			return fmt.Errorf("batchtrace: 确认记录 (%q, %q) 数量 %d 非正整数", c.BatchID, c.IssueID, c.Quantity)
		}
		if !candidate[ii][c.BatchID] {
			return fmt.Errorf("batchtrace: 确认记录 (%q, %q) 超出该笔领料的候选来源范围", c.BatchID, c.IssueID)
		}
		if !usableAt(batches[bi], issues[ii].Time) {
			return fmt.Errorf("batchtrace: 确认记录 (%q, %q) 违反批次到货/失效时间限制", c.BatchID, c.IssueID)
		}
		if c.Quantity > issues[ii].Quantity {
			return fmt.Errorf("batchtrace: 确认记录 (%q, %q) 数量 %d 超过领料数量 %d", c.BatchID, c.IssueID, c.Quantity, issues[ii].Quantity)
		}
		if c.Quantity > batches[bi].Quantity {
			return fmt.Errorf("batchtrace: 确认记录 (%q, %q) 数量 %d 超过批次数量 %d", c.BatchID, c.IssueID, c.Quantity, batches[bi].Quantity)
		}
	}
	return nil
}
