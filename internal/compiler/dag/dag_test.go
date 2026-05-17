package dag

import "testing"

func TestTopologicalSort(t *testing.T) {
	g := New()
	g.AddNode("worker", []string{"db", "net"})
	g.AddNode("db", []string{"net"})
	g.AddNode("net", nil)

	got, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort() error = %v", err)
	}

	want := []string{"net", "db", "worker"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestHasCycle(t *testing.T) {
	g := New()
	g.AddNode("a", []string{"b"})
	g.AddNode("b", []string{"a"})

	if _, err := g.TopologicalSort(); err == nil {
		t.Fatalf("TopologicalSort() expected cycle error")
	}
	if !g.HasCycle() {
		t.Fatalf("HasCycle() = false, want true")
	}
}
