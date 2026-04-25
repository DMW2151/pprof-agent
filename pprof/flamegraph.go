package pprof

import "sort"

// FlameNode is one node in the call tree.
type FlameNode struct {
	Name     string       `json:"name"`
	Value    int64        `json:"value"`
	Pct      float64      `json:"pct"`
	Children []*FlameNode `json:"children,omitempty"`
}

// FlameTree builds a call tree from the profile samples for the given value index.
// Nodes with value below thresholdPct% of total are pruned.
func FlameTree(p *Profile, idx int, thresholdPct float64) *FlameNode {
	total := p.TotalValue(idx)
	if total == 0 {
		return &FlameNode{Name: "root", Value: 0}
	}
	threshold := int64(thresholdPct / 100 * float64(total))

	root := &FlameNode{Name: "root", Value: total, Pct: 100}
	childMap := map[*FlameNode]map[string]*FlameNode{root: {}}

	for _, s := range p.raw.Sample {
		if idx >= len(s.Value) {
			continue
		}
		v := s.Value[idx]

		path := make([]string, 0, len(s.Location))
		for i := len(s.Location) - 1; i >= 0; i-- {
			loc := s.Location[i]
			for j := len(loc.Line) - 1; j >= 0; j-- {
				if loc.Line[j].Function != nil {
					path = append(path, loc.Line[j].Function.Name)
				}
			}
		}

		cur := root
		for _, fn := range path {
			if childMap[cur] == nil {
				childMap[cur] = map[string]*FlameNode{}
			}
			child, ok := childMap[cur][fn]
			if !ok {
				child = &FlameNode{Name: fn}
				childMap[cur][fn] = child
				cur.Children = append(cur.Children, child)
				childMap[child] = map[string]*FlameNode{}
			}
			child.Value += v
			cur = child
		}
	}

	computePct(root, total)
	pruneTree(root, threshold)
	sortTree(root)
	return root
}

func computePct(n *FlameNode, total int64) {
	if total == 0 {
		return
	}
	n.Pct = 100 * float64(n.Value) / float64(total)
	for _, c := range n.Children {
		computePct(c, total)
	}
}

func pruneTree(n *FlameNode, threshold int64) {
	kept := n.Children[:0]
	for _, c := range n.Children {
		if c.Value >= threshold {
			pruneTree(c, threshold)
			kept = append(kept, c)
		}
	}
	n.Children = kept
}

func sortTree(n *FlameNode) {
	sort.Slice(n.Children, func(i, j int) bool {
		return n.Children[i].Value > n.Children[j].Value
	})
	for _, c := range n.Children {
		sortTree(c)
	}
}
