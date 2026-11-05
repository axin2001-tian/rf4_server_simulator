package store

import (
	"context"
	"sort"
)

// SkillIdentity 将共享等级的技能节点映射到同一个逻辑技能。
type SkillIdentity struct {
	byNode map[int64]int64
	groups map[int64][]int64
}

// LoadSkillIdentity 按同类目 group_id 与跨类目 skill_link 合并节点。
func (d *DB) LoadSkillIdentity(ctx context.Context) (*SkillIdentity, error) {
	parent := map[int64]int64{}
	add := func(node int64) {
		if _, ok := parent[node]; !ok {
			parent[node] = node
		}
	}
	var find func(int64) int64
	find = func(node int64) int64 {
		root := node
		for parent[root] != root {
			root = parent[root]
		}
		for parent[node] != node {
			next := parent[node]
			parent[node] = root
			node = next
		}
		return root
	}
	union := func(a, b int64) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}

	type groupKey struct {
		catID   int64
		groupID int64
	}
	groupOwner := map[groupKey]int64{}
	rows, err := d.SQL.QueryContext(ctx, `SELECT cat_id,node_id,group_id FROM skill_def`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var catID, nodeID, groupID int64
		if err := rows.Scan(&catID, &nodeID, &groupID); err != nil {
			rows.Close()
			return nil, err
		}
		add(nodeID)
		if groupID == 0 {
			continue
		}
		key := groupKey{catID: catID, groupID: groupID}
		if owner, ok := groupOwner[key]; ok {
			union(owner, nodeID)
		} else {
			groupOwner[key] = nodeID
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	linkOwner := map[int64]int64{}
	rows, err = d.SQL.QueryContext(ctx, `SELECT node_id,link_id FROM skill_link`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var nodeID, linkID int64
		if err := rows.Scan(&nodeID, &linkID); err != nil {
			rows.Close()
			return nil, err
		}
		add(nodeID)
		if owner, ok := linkOwner[linkID]; ok {
			union(owner, nodeID)
		} else {
			linkOwner[linkID] = nodeID
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	ident := &SkillIdentity{
		byNode: make(map[int64]int64, len(parent)),
		groups: map[int64][]int64{},
	}
	for node := range parent {
		root := find(node)
		ident.byNode[node] = root
		ident.groups[root] = append(ident.groups[root], node)
	}
	for root := range ident.groups {
		sort.Slice(ident.groups[root], func(i, j int) bool {
			return ident.groups[root][i] < ident.groups[root][j]
		})
	}
	return ident, nil
}

// Group 返回与 nodeID 共享等级的节点；未登记节点按单节点处理。
func (i *SkillIdentity) Group(nodeID int64) []int64 {
	if i != nil {
		if root, ok := i.byNode[nodeID]; ok {
			return append([]int64(nil), i.groups[root]...)
		}
	}
	return []int64{nodeID}
}

// Project 将每个技能身份的最高等级映射回组内全部节点。
func (i *SkillIdentity) Project(nodes map[int64]int64) map[int64]int64 {
	out := make(map[int64]int64, len(nodes))
	for node, level := range nodes {
		out[node] = max(int64(0), level)
	}
	if i == nil {
		return out
	}
	for _, ids := range i.groups {
		var level int64
		found := false
		for _, id := range ids {
			if value, ok := nodes[id]; ok {
				found = true
				level = max(level, value)
			}
		}
		if !found {
			continue
		}
		for _, id := range ids {
			out[id] = level
		}
	}
	return out
}

// SpentPoints 按技能身份统计已投入点数，每个身份只计一次。
func (i *SkillIdentity) SpentPoints(nodes map[int64]int64) int64 {
	var spent int64
	seen := make(map[int64]bool, len(nodes))
	if i == nil {
		for _, level := range nodes {
			spent += max(int64(0), level)
		}
		return spent
	}
	for _, ids := range i.groups {
		var level int64
		for _, id := range ids {
			if value, ok := nodes[id]; ok {
				seen[id] = true
				level = max(level, value)
			}
		}
		spent += level
	}
	for node, level := range nodes {
		if !seen[node] {
			spent += max(int64(0), level)
		}
	}
	return spent
}
