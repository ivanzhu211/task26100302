package batchtrace

import "testing"

// s->a(5,0) s->b(3,0) a->t(4,1) b->t(10,0) a->b(2,0)
// 最大流 8：b->t 走 5（s->b 的 3 加 a->b 改道的 2），a->t 走 3。费用 3。
func TestMCMFSmall(t *testing.T) {
	g := newMCMF(4) // 0=s 1=a 2=b 3=t
	g.addEdge(0, 1, 5, 0)
	g.addEdge(0, 2, 3, 0)
	g.addEdge(1, 3, 4, 1)
	g.addEdge(2, 3, 10, 0)
	g.addEdge(1, 2, 2, 0)
	flow, cost := g.minCostMaxFlow(0, 3)
	if flow != 8 {
		t.Fatalf("flow = %d, want 8", flow)
	}
	if cost != 3 {
		t.Fatalf("cost = %d, want 3", cost)
	}
}

// 需要残量回退改道才能达到最大流：
// s->a(1,0) s->b(1,0) a->b(1,0) b->t(1,0) a->t(1,2)
// 首轮最短路 s->a->b->t 费用 0；之后必须经 b->a 回退边改走 a->t，
// 总流 2，费用 2。
func TestMCMFReroute(t *testing.T) {
	g := newMCMF(4) // 0=s 1=a 2=b 3=t
	g.addEdge(0, 1, 1, 0)
	g.addEdge(0, 2, 1, 0)
	g.addEdge(1, 2, 1, 0)
	g.addEdge(2, 3, 1, 0)
	g.addEdge(1, 3, 1, 2)
	flow, cost := g.minCostMaxFlow(0, 3)
	if flow != 2 {
		t.Fatalf("flow = %d, want 2", flow)
	}
	if cost != 2 {
		t.Fatalf("cost = %d, want 2", cost)
	}
}

// 大数量：容量 1e12 级别，验证 int64 精确。
func TestMCMFLargeCap(t *testing.T) {
	g := newMCMF(4)
	const big = int64(1_000_000_000_000)
	g.addEdge(0, 1, big, 1)
	g.addEdge(0, 2, big, 0)
	g.addEdge(1, 3, big, 0)
	g.addEdge(2, 3, big, 0)
	flow, cost := g.minCostMaxFlow(0, 3)
	if flow != 2*big {
		t.Fatalf("flow = %d, want %d", flow, 2*big)
	}
	if cost != big {
		t.Fatalf("cost = %d, want %d", cost, big)
	}
}
