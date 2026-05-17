package dag

import (
	"fmt"
	"sort"
)

type Graph struct {
	nodes map[string][]string
}

func New() *Graph {
	return &Graph{nodes: map[string][]string{}}
}

func (g *Graph) AddNode(name string, deps []string) {
	if g.nodes == nil {
		g.nodes = map[string][]string{}
	}
	depSet := map[string]struct{}{}
	for _, dep := range deps {
		depSet[dep] = struct{}{}
		if _, ok := g.nodes[dep]; !ok {
			g.nodes[dep] = nil
		}
	}
	uniq := make([]string, 0, len(depSet))
	for dep := range depSet {
		uniq = append(uniq, dep)
	}
	sort.Strings(uniq)
	g.nodes[name] = uniq
}

func (g *Graph) TopologicalSort() ([]string, error) {
	if g.nodes == nil {
		return nil, nil
	}
	keys := make([]string, 0, len(g.nodes))
	for key := range g.nodes {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	state := map[string]int{}
	out := make([]string, 0, len(keys))
	var visit func(string) error
	visit = func(node string) error {
		switch state[node] {
		case 1:
			return fmt.Errorf("cycle detected involving %q", node)
		case 2:
			return nil
		}
		state[node] = 1
		deps := append([]string(nil), g.nodes[node]...)
		sort.Strings(deps)
		for _, dep := range deps {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[node] = 2
		out = append(out, node)
		return nil
	}

	for _, key := range keys {
		if err := visit(key); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (g *Graph) HasCycle() bool {
	_, err := g.TopologicalSort()
	return err != nil
}
