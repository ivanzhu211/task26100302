package batchtrace

import "container/heap"

// mcmfEdge 残量网络中的一条边。
type mcmfEdge struct {
	to   int
	rev  int
	cap  int64
	cost int64
}

// mcmf 最小费用最大流求解器。
// 算法：primal-dual。每轮用 Dijkstra（带势）求最短路距离并更新势，
// 再在容许网络（约化费用为 0 的边）上用当前弧 DFS 尽量多地增广，
// 避免逐条路径重新跑 Dijkstra，保证大数量、大规模下依然快速。
// 本库中费用只取 0/1 且初始全非负，初始势全 0 即满足不变式。
type mcmf struct {
	graph [][]mcmfEdge
}

// newMCMF 创建 n 个节点的空网络。
func newMCMF(n int) *mcmf {
	return &mcmf{graph: make([][]mcmfEdge, n)}
}

// addEdge 加入一条容量 cap、单位费用 cost 的边（含反向残量边）。
func (g *mcmf) addEdge(from, to int, cap, cost int64) {
	g.graph[from] = append(g.graph[from], mcmfEdge{to: to, rev: len(g.graph[to]), cap: cap, cost: cost})
	g.graph[to] = append(g.graph[to], mcmfEdge{to: from, rev: len(g.graph[from]) - 1, cap: 0, cost: -cost})
}

const infCap = int64(1) << 62

// distNode Dijkstra 堆元素。
type distNode struct {
	v    int
	dist int64
}

type dijkstraHeap []distNode

func (h dijkstraHeap) Len() int            { return len(h) }
func (h dijkstraHeap) Less(i, j int) bool  { return h[i].dist < h[j].dist }
func (h dijkstraHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *dijkstraHeap) Push(x interface{}) { *h = append(*h, x.(distNode)) }
func (h *dijkstraHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// minCostMaxFlow 从 s 到 t 求最小费用最大流，返回流量与费用。
func (g *mcmf) minCostMaxFlow(s, t int) (flow, cost int64) {
	n := len(g.graph)
	potential := make([]int64, n)
	dist := make([]int64, n)
	iter := make([]int, n)
	state := make([]uint8, n) // 0=可用 1=在递归栈上 2=本轮已枯竭

	for {
		// Dijkstra：在约化费用上求 s 到各点最短距离。
		for i := range dist {
			dist[i] = -1
		}
		dist[s] = 0
		h := &dijkstraHeap{{v: s, dist: 0}}
		for h.Len() > 0 {
			cur := heap.Pop(h).(distNode)
			if cur.dist > dist[cur.v] {
				continue
			}
			for _, e := range g.graph[cur.v] {
				if e.cap <= 0 {
					continue
				}
				nd := cur.dist + e.cost + potential[cur.v] - potential[e.to]
				if dist[e.to] < 0 || nd < dist[e.to] {
					dist[e.to] = nd
					heap.Push(h, distNode{v: e.to, dist: nd})
				}
			}
		}
		if dist[t] < 0 {
			return flow, cost
		}
		// 更新势，保持所有残量边约化费用非负。
		for v := 0; v < n; v++ {
			if dist[v] >= 0 {
				potential[v] += dist[v]
			}
		}
		// 在容许网络上尽量增广（每条增广路都是当前最短路）。
		for i := range iter {
			iter[i] = 0
		}
		for i := range state {
			state[i] = 0
		}
		for {
			pushed := g.augment(s, t, infCap, potential, iter, state)
			if pushed == 0 {
				break
			}
			flow += pushed
			cost += pushed * potential[t] // potential[t] 即本轮最短路原费用
		}
	}
}

// augment 在容许网络中沿最短路增广，返回本次推送的流量。
// state 防止零费用环导致的递归死循环，并标记本轮枯竭节点。
func (g *mcmf) augment(v, t int, f int64, potential []int64, iter []int, state []uint8) int64 {
	if v == t {
		return f
	}
	if state[v] != 0 {
		return 0
	}
	state[v] = 1
	for ; iter[v] < len(g.graph[v]); iter[v]++ {
		e := &g.graph[v][iter[v]]
		if e.cap <= 0 || state[e.to] == 1 {
			continue
		}
		// 容许边：约化费用为 0，即满足最短路等式。
		if potential[e.to] != potential[v]+e.cost {
			continue
		}
		d := min(f, e.cap)
		if d = g.augment(e.to, t, d, potential, iter, state); d > 0 {
			e.cap -= d
			g.graph[e.to][e.rev].cap += d
			state[v] = 0
			return d
		}
	}
	state[v] = 2
	return 0
}
