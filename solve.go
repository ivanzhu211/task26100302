package batchtrace

// solve 把一次查询建模为带下界的最小费用流并求解。
//
// 网络结构（所有费用非负，0 或 1）：
//
//	SS(超级源) --> 各正需求节点
//	S --> 批次(容量=批次量) --> 领料(仅候选且时间可用, 下界=确认量, 费用 0/1)
//	领料 --> T(下界=容量=领料量, 即必须全额满足)
//	T --> S(容量=总需求, 闭合为循环流)
//	各负需求节点 --> TT(超级汇)
//
// 求下界时：异常批次 -> 目标领料 的边费用为 1，其余为 0，
// 最小费用即所求下界。求上界时改为对 非异常批次 -> 目标领料
// 的边计费用，上界 = 目标领料总量 - 该最小费用。
// 注意下界变换后边上的费用只计量超出确认量的部分，
// 确认量本身的费用（confirmedCost）需加回。
//
// maximize 为 false 时求下界，为 true 时求上界。
// 返回端点值与达到该端点的一份覆盖整份账本的完整分配；
// 账本无可行分配时返回 ErrInfeasible。
func (l *Ledger) solve(anomalous map[string]bool, targets map[string]bool, maximize bool) (int64, []Assignment, error) {
	nb := len(l.batches)
	ni := len(l.issues)

	nodeS := 0
	batchNode := func(bi int) int { return 1 + bi }
	issueNode := func(ii int) int { return 1 + nb + ii }
	nodeT := 1 + nb + ni
	nodeSS := nodeT + 1
	nodeTT := nodeT + 2
	n := nodeTT + 1

	type pair struct{ b, i int }
	confirmed := make(map[pair]int64, len(l.confirms))
	for _, c := range l.confirms {
		confirmed[pair{l.batchIndex[c.BatchID], l.issueIndex[c.IssueID]}] += c.Quantity
	}

	g := newMCFGraph(n)
	demand := make([]int64, n)

	var totalDemand, targetQty, confirmedCost int64
	for bi, b := range l.batches {
		g.addEdge(nodeS, batchNode(bi), b.Quantity, 0)
	}

	type candEdge struct{ bi, ii, from, idx int }
	candEdges := make([]candEdge, 0, 1024)
	for ii, is := range l.issues {
		isTarget := targets[is.ID]
		if isTarget {
			targetQty += is.Quantity
		}
		totalDemand += is.Quantity
		for _, bid := range is.Candidates {
			bi := l.batchIndex[bid]
			if !usableAt(l.batches[bi], is.Time) {
				continue // 时间不可用的候选不产生可用来源
			}
			lower := confirmed[pair{bi, ii}]
			if lower > is.Quantity {
				return 0, nil, ErrInfeasible // 同一对批次/领料的确认量超过领料量
			}
			var cost int64
			if isTarget && anomalous[bid] != maximize {
				// 下界：异常->目标计 1；上界：非异常->目标计 1
				cost = 1
				confirmedCost += lower
			}
			from := batchNode(bi)
			idx := len(g.g[from])
			g.addEdge(from, issueNode(ii), is.Quantity-lower, cost)
			demand[from] -= lower
			demand[issueNode(ii)] += lower
			candEdges = append(candEdges, candEdge{bi, ii, from, idx})
		}
		// 领料必须全额满足：下界=容量=领料量
		g.addEdge(issueNode(ii), nodeT, 0, 0)
		demand[issueNode(ii)] -= is.Quantity
		demand[nodeT] += is.Quantity
	}
	// 闭合为循环流，未使用库存可留存在 S->批次 边上
	g.addEdge(nodeT, nodeS, totalDemand, 0)

	var required int64
	for v := 0; v < n; v++ {
		if demand[v] > 0 {
			g.addEdge(nodeSS, v, demand[v], 0)
			required += demand[v]
		} else if demand[v] < 0 {
			g.addEdge(v, nodeTT, -demand[v], 0)
		}
	}

	flow, cost := minCostFlow(g, nodeSS, nodeTT, required)
	if flow < required {
		return 0, nil, ErrInfeasible
	}
	cost += confirmedCost

	witness := make([]Assignment, 0, len(candEdges))
	for _, ce := range candEdges {
		qty := confirmed[pair{ce.bi, ce.ii}] + g.flowOn(ce.from, ce.idx)
		if qty > 0 {
			witness = append(witness, Assignment{
				BatchID:  l.batches[ce.bi].ID,
				IssueID:  l.issues[ce.ii].ID,
				Quantity: qty,
			})
		}
	}

	if maximize {
		return targetQty - cost, witness, nil
	}
	return cost, witness, nil
}
