package batchtrace

// minCostFlow 是内部使用的最小费用流求解器。
// 本文件只放与具体业务无关的图算法。
// 所有数量均为 int64，全程整数运算，无浮点舍入。

// flowEdge 是残余网络中的一条边。
type flowEdge struct {
	to, rev int
	cap     int64 // 残余容量
	cost    int64 // 单位费用（正向边非负）
	cap0    int64 // 初始容量（仅正向边，用于提取流量）
}

// mcfGraph 为邻接表形式的残余网络。
type mcfGraph struct {
	g [][]flowEdge
}

func newMCFGraph(n int) *mcfGraph {
	return &mcfGraph{g: make([][]flowEdge, n)}
}

// addEdge 加入一条容量 cap、单位费用 cost 的正向边及其反向残余边。
func (g *mcfGraph) addEdge(from, to int, cap, cost int64) {
	fwd := flowEdge{to: to, rev: len(g.g[to]), cap: cap, cost: cost, cap0: cap}
	bwd := flowEdge{to: from, rev: len(g.g[from]), cap: 0, cost: -cost}
	g.g[from] = append(g.g[from], fwd)
	g.g[to] = append(g.g[to], bwd)
}

// flowOn 返回正向边 (from, edgeIdx) 上当前的流量。
func (g *mcfGraph) flowOn(from, edgeIdx int) int64 {
	e := g.g[from][edgeIdx]
	return e.cap0 - e.cap
}

const infDist = int64(^uint64(0) >> 1) // math.MaxInt64

// intHeap 是按距离排序的二叉小根堆（手写以避免接口开销）。
type intHeap struct {
	node []int
	dist []int64
}

func (h *intHeap) push(v int, d int64) {
	h.node = append(h.node, v)
	h.dist = append(h.dist, d)
	i := len(h.node) - 1
	for i > 0 {
		p := (i - 1) / 2
		if h.dist[p] <= h.dist[i] {
			break
		}
		h.node[p], h.node[i] = h.node[i], h.node[p]
		h.dist[p], h.dist[i] = h.dist[i], h.dist[p]
		i = p
	}
}

func (h *intHeap) pop() (int, int64) {
	top, topd := h.node[0], h.dist[0]
	n := len(h.node) - 1
	h.node[0], h.dist[0] = h.node[n], h.dist[n]
	h.node, h.dist = h.node[:n], h.dist[:n]
	i := 0
	for {
		l, r := 2*i+1, 2*i+2
		m := i
		if l < n && h.dist[l] < h.dist[m] {
			m = l
		}
		if r < n && h.dist[r] < h.dist[m] {
			m = r
		}
		if m == i {
			break
		}
		h.node[m], h.node[i] = h.node[i], h.node[m]
		h.dist[m], h.dist[i] = h.dist[i], h.dist[m]
		i = m
	}
	return top, topd
}

// minCostFlow 在图上求从 s 到 t 流量至多为 limit 的最小费用流，
// 返回实际流量与总费用。要求所有正向边费用非负。
//
// 使用原始-对偶算法：每轮以 Dijkstra 在简约费用下求最短路并
// 更新势，随后在零简约费用边构成的允许网络上用当前弧优化的
// 阻塞流一次推完该距离层的全部流量，Dijkstra 轮数远少于增广次数。
func minCostFlow(g *mcfGraph, s, t int, limit int64) (flow, cost int64) {
	n := len(g.g)
	potential := make([]int64, n)
	dist := make([]int64, n)
	iter := make([]int, n)
	onPath := make([]bool, n)
	heap := intHeap{node: make([]int, 0, 256), dist: make([]int64, 0, 256)}

	// 在允许网络（零简约费用边）上从 v 向 t 推送至多 f 的阻塞流。
	var augment func(v int, f int64) int64
	augment = func(v int, f int64) int64 {
		if v == t {
			return f
		}
		onPath[v] = true
		for ; iter[v] < len(g.g[v]); iter[v]++ {
			e := &g.g[v][iter[v]]
			if e.cap <= 0 || onPath[e.to] {
				continue
			}
			if e.cost+potential[v]-potential[e.to] != 0 {
				continue
			}
			d := f
			if e.cap < d {
				d = e.cap
			}
			if d = augment(e.to, d); d > 0 {
				e.cap -= d
				g.g[e.to][e.rev].cap += d
				onPath[v] = false
				return d
			}
		}
		onPath[v] = false
		return 0
	}

	for flow < limit {
		for i := range dist {
			dist[i] = infDist
		}
		dist[s] = 0
		heap.node, heap.dist = heap.node[:0], heap.dist[:0]
		heap.push(s, 0)
		for len(heap.node) > 0 {
			v, d := heap.pop()
			if d > dist[v] {
				continue
			}
			for ei := range g.g[v] {
				e := &g.g[v][ei]
				if e.cap <= 0 {
					continue
				}
				if nd := d + e.cost + potential[v] - potential[e.to]; nd < dist[e.to] {
					dist[e.to] = nd
					heap.push(e.to, nd)
				}
			}
		}
		if dist[t] == infDist {
			break
		}
		for v := 0; v < n; v++ {
			if dist[v] < infDist {
				potential[v] += dist[v]
			}
		}
		for i := range iter {
			iter[i] = 0
		}
		// potential[s] 恒为 0，potential[t] 即本层真实最短路费用
		for flow < limit {
			f := augment(s, limit-flow)
			if f == 0 {
				break
			}
			flow += f
			cost += f * potential[t]
		}
	}
	return flow, cost
}
