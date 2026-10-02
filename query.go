package batchtrace

import (
	"fmt"
	"sort"
)

// Query 查询一组异常入库批次流向一组目标领料的合计数量上下界。
// 上下界分别是全部合法分配中的真实最小值与最大值，
// 各附一份覆盖整份账本的完整分配作为核查依据。
// 查询不扣减库存，不同查询相互独立。
// 若整份账本不存在合法分配，返回 ErrInfeasible，不给出任何区间。
func (l *Ledger) Query(abnormalBatches []string, targetIssues []string) (*Bounds, error) {
	abnormal := make(map[int]bool, len(abnormalBatches))
	for _, id := range abnormalBatches {
		bi, ok := l.batchIndex[id]
		if !ok {
			return nil, fmt.Errorf("batchtrace: 查询的异常批次 %q 不存在", id)
		}
		abnormal[bi] = true
	}
	target := make(map[int]bool, len(targetIssues))
	for _, id := range targetIssues {
		ii, ok := l.issueIndex[id]
		if !ok {
			return nil, fmt.Errorf("batchtrace: 查询的目标领料 %q 不存在", id)
		}
		target[ii] = true
	}

	// 下界：最小化异常->目标流量（费用 1 落在异常∩目标边上）。
	minAlloc, minSum, err := l.solve(abnormal, target, true)
	if err != nil {
		return nil, err
	}
	// 上界：最大化异常->目标流量 = 最小化其余边上的流量。
	maxAlloc, maxSum, err := l.solve(abnormal, target, false)
	if err != nil {
		return nil, err
	}

	return &Bounds{
		Min:           minSum,
		Max:           maxSum,
		MinAllocation: minAlloc,
		MaxAllocation: maxAlloc,
	}, nil
}

// solve 求解一次最小费用最大流并提取完整分配。
// minimizeX 为 true 时费用 1 落在"异常批次->目标领料"边上（求下界），
// 为 false 时费用 1 落在其余边上（求上界）。
// 返回完整分配与其中异常->目标的合计数量。
func (l *Ledger) solve(abnormal, target map[int]bool, minimizeX bool) ([]Allocation, int64, error) {
	numBatches := len(l.batches)
	numIssues := len(l.issues)

	// 汇总确认记录为 (领料,批次) 对的下界，并扣减批次供给与领料需求。
	confirmed := make(map[int64]int64, len(l.confirmations))
	supply := make([]int64, numBatches)
	for i, b := range l.batches {
		supply[i] = b.Quantity
	}
	demand := make([]int64, numIssues)
	for i, is := range l.issues {
		demand[i] = is.Quantity
	}
	for _, c := range l.confirmations {
		ii := l.issueIndex[c.IssueID]
		bi := l.batchIndex[c.BatchID]
		key := int64(ii)*int64(numBatches) + int64(bi)
		confirmed[key] += c.Quantity
		supply[bi] -= c.Quantity
		demand[ii] -= c.Quantity
	}
	totalDemand := int64(0)
	for i := range demand {
		if demand[i] < 0 {
			return nil, 0, ErrInfeasible // 确认数量已超出领料需求
		}
		totalDemand += demand[i]
	}
	for _, s := range supply {
		if s < 0 {
			return nil, 0, ErrInfeasible // 确认数量已超出批次库存
		}
	}

	// 建网：0=源点，1..B=批次，B+1..B+I=领料，B+I+1=汇点。
	source := 0
	batchNode := func(bi int) int { return 1 + bi }
	issueNode := func(ii int) int { return 1 + numBatches + ii }
	sink := 1 + numBatches + numIssues
	g := newMCMF(sink + 1)

	for bi := 0; bi < numBatches; bi++ {
		if supply[bi] > 0 {
			g.addEdge(source, batchNode(bi), supply[bi], 0)
		}
	}
	for ii := 0; ii < numIssues; ii++ {
		if demand[ii] > 0 {
			g.addEdge(issueNode(ii), sink, demand[ii], 0)
		}
	}
	// 候选边：记录 (批次节点, 邻接下标, 领料下标) 以便回读流量。
	type midEdge struct {
		from   int
		adjIdx int
		issue  int
		batch  int
	}
	midEdges := make([]midEdge, 0, 30000)
	for ii := 0; ii < numIssues; ii++ {
		for bi := range l.candidateSet[ii] {
			if !usableAt(l.batches[bi], l.issues[ii].Time) {
				continue
			}
			cap := supply[bi]
			if demand[ii] < cap {
				cap = demand[ii]
			}
			if cap <= 0 {
				continue
			}
			isX := abnormal[bi] && target[ii]
			var cost int64
			if minimizeX == isX {
				cost = 1
			}
			from := batchNode(bi)
			midEdges = append(midEdges, midEdge{from: from, adjIdx: len(g.graph[from]), issue: ii, batch: bi})
			g.addEdge(from, issueNode(ii), cap, cost)
		}
	}

	flow, _ := g.minCostMaxFlow(source, sink)
	if flow != totalDemand {
		return nil, 0, ErrInfeasible
	}

	// 回读每条候选边上的流量，与确认下界按 (领料,批次) 对合并，组成完整分配。
	// 确认对也要单独遍历：需求已被确认覆盖的领料可能没有建候选边。
	totals := make(map[int64]int64, len(confirmed)+len(midEdges))
	for key, q := range confirmed {
		totals[key] = q
	}
	xSum := int64(0)
	for _, me := range midEdges {
		e := g.graph[me.from][me.adjIdx]
		used := g.graph[e.to][e.rev].cap // 反向边容量即正向流量
		if used == 0 {
			continue
		}
		key := int64(me.issue)*int64(numBatches) + int64(me.batch)
		totals[key] += used
	}
	alloc := make([]Allocation, 0, len(totals))
	for key, total := range totals {
		if total == 0 {
			continue
		}
		ii := int(key / int64(numBatches))
		bi := int(key % int64(numBatches))
		alloc = append(alloc, Allocation{
			IssueID:  l.issues[ii].ID,
			BatchID:  l.batches[bi].ID,
			Quantity: total,
		})
		if abnormal[bi] && target[ii] {
			xSum += total
		}
	}
	// 输出排序，保证同一账本同一查询的结果确定。
	sort.Slice(alloc, func(i, j int) bool {
		if alloc[i].IssueID != alloc[j].IssueID {
			return alloc[i].IssueID < alloc[j].IssueID
		}
		return alloc[i].BatchID < alloc[j].BatchID
	})
	return alloc, xSum, nil
}
