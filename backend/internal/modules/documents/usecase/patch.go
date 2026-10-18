package usecase

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
)

type nodeLocation struct {
	path []int
	node Node
}

func locate(nodes []Node, id uuid.UUID, path []int) (nodeLocation, bool) {
	for index, node := range nodes {
		element := node.element()
		if element == nil {
			continue
		}
		p := append(append([]int(nil), path...), index)
		current, _ := canonicalID(element.ID)
		if current == id {
			return nodeLocation{p, node}, true
		}
		if found, ok := locate(element.Children, id, p); ok {
			return found, true
		}
	}
	return nodeLocation{}, false
}
func childContainer(root *[]Node, path []int) *[]Node {
	children := root
	for _, index := range path {
		children = &(*children)[index].element().Children
	}
	return children
}
func containerFor(root *[]Node, id *string) (*[]Node, error) {
	if id == nil {
		return root, nil
	}
	key, valid := canonicalID(*id)
	if !valid {
		return nil, contentFailure(field("patches", "invalid_parent"))
	}
	location, ok := locate(*root, key, nil)
	if !ok {
		return nil, contentFailure(field("patches", "unknown_parent"))
	}
	return &location.node.element().Children, nil
}
func insertionIndex(children []Node, before *string) (int, error) {
	if before == nil {
		return len(children), nil
	}
	key, valid := canonicalID(*before)
	if !valid {
		return 0, contentFailure(field("patches", "invalid_before"))
	}
	for index, node := range children {
		if element := node.element(); element != nil {
			id, _ := canonicalID(element.ID)
			if id == key {
				return index, nil
			}
		}
	}
	return 0, contentFailure(field("patches", "unknown_before"))
}
func insertAt(children *[]Node, index int, nodes []Node) {
	out := make([]Node, 0, len(*children)+len(nodes))
	out = append(out, (*children)[:index]...)
	out = append(out, nodes...)
	out = append(out, (*children)[index:]...)
	*children = out
}
func collectIDs(nodes []Node, ids map[uuid.UUID]bool) {
	for _, node := range nodes {
		if element := node.element(); element != nil {
			if id, ok := canonicalID(element.ID); ok {
				ids[id] = true
			}
			collectIDs(element.Children, ids)
		}
	}
}
func headingRank(kind string) int {
	switch kind {
	case "h1":
		return 1
	case "h2":
		return 2
	case "h3":
		return 3
	}
	return 0
}
func section(nodes []Node, id string, expected []string) (int, int, error) {
	key, valid := canonicalID(id)
	if !valid {
		return 0, 0, contentFailure(field("patches", "invalid_heading"))
	}
	location, ok := locate(nodes, key, nil)
	if !ok || len(location.path) != 1 {
		return 0, 0, contentFailure(field("patches", "unknown_heading"))
	}
	rank := headingRank(location.node.element().Type)
	if rank == 0 {
		return 0, 0, contentFailure(field("patches", "invalid_heading"))
	}
	start := location.path[0] + 1
	end := start
	for end < len(nodes) {
		other := headingRank(nodes[end].element().Type)
		if other > 0 && other <= rank {
			break
		}
		end++
	}
	if expected == nil || len(expected) != end-start {
		return 0, 0, contentFailure(field("patches", "section_conflict"))
	}
	for index, value := range expected {
		id, valid := canonicalID(value)
		actual, _ := canonicalID(nodes[start+index].element().ID)
		if !valid || id != actual {
			return 0, 0, contentFailure(field("patches", "section_conflict"))
		}
	}
	return start, end, nil
}
func patchTouches(content []Node, patch Patch) (map[uuid.UUID]bool, error) {
	ids := map[uuid.UUID]bool{}
	target := func(value string) (nodeLocation, error) {
		key, valid := canonicalID(value)
		if !valid {
			return nodeLocation{}, contentFailure(field("patches", "invalid_node_id"))
		}
		node, ok := locate(content, key, nil)
		if !ok {
			return nodeLocation{}, contentFailure(field("patches", "unknown_node"))
		}
		collectIDs([]Node{node.node}, ids)
		return node, nil
	}
	anchor := func(value *string) {
		if value == nil {
			ids[uuid.Nil] = true
		} else if id, ok := canonicalID(*value); ok {
			ids[id] = true
		}
	}
	switch value := patch.Value.(type) {
	case *ReplaceNode:
		if value.Op != "replace_node" {
			return nil, contentFailure(field("patches", "invalid_op"))
		}
		old, e := target(value.NodeID)
		if e != nil {
			return nil, e
		}
		replacement := value.Node.element()
		if replacement == nil || replacement.ID != old.node.element().ID {
			return nil, contentFailure(field("patches", "identity_changed"))
		}
		collectIDs([]Node{value.Node}, ids)
	case *RemoveNode:
		if value.Op != "remove_node" {
			return nil, contentFailure(field("patches", "invalid_op"))
		}
		if _, e := target(value.NodeID); e != nil {
			return nil, e
		}
	case *InsertNodes:
		if value.Op != "insert_nodes" || len(value.Nodes) == 0 {
			return nil, contentFailure(field("patches", "invalid_insert"))
		}
		anchor(value.ParentID)
		if value.BeforeID != nil {
			anchor(value.BeforeID)
		}
		collectIDs(value.Nodes, ids)
	case *MoveNode:
		if value.Op != "move_node" {
			return nil, contentFailure(field("patches", "invalid_op"))
		}
		location, e := target(value.NodeID)
		if e != nil {
			return nil, e
		}
		anchor(value.ParentID)
		if value.BeforeID != nil {
			anchor(value.BeforeID)
		}
		if value.ParentID != nil {
			parent, _ := canonicalID(*value.ParentID)
			if _, descendant := ids[parent]; descendant {
				parentLoc, ok := locate(content, parent, nil)
				if ok && len(parentLoc.path) >= len(location.path) {
					same := true
					for i, index := range location.path {
						if parentLoc.path[i] != index {
							same = false
							break
						}
					}
					if same {
						return nil, contentFailure(field("patches", "descendant_move"))
					}
				}
			}
		}
	case *ReplaceSection:
		if value.Op != "replace_section" || value.Nodes == nil {
			return nil, contentFailure(field("patches", "invalid_section"))
		}
		start, end, e := section(content, value.HeadingID, value.ExpectedMemberIDs)
		if e != nil {
			return nil, e
		}
		heading, _ := canonicalID(value.HeadingID)
		ids[heading] = true
		collectIDs(content[start:end], ids)
		collectIDs(value.Nodes, ids)
		rank := headingRank(content[start-1].element().Type)
		for _, node := range value.Nodes {
			element := node.element()
			if element == nil {
				return nil, contentFailure(field("patches", "invalid_section"))
			}
			if next := headingRank(element.Type); next > 0 && next <= rank {
				return nil, contentFailure(field("patches", "section_boundary"))
			}
		}
	default:
		return nil, contentFailure(field("patches", "invalid_patch"))
	}
	return ids, nil
}
func ApplyPatches(content []Node, patches []Patch) ([]Node, error) {
	if len(patches) < 1 || len(patches) > 100 {
		return nil, contentFailure(field("patches", "invalid"))
	}
	touched := map[uuid.UUID]bool{}
	for _, patch := range patches {
		ids, e := patchTouches(content, patch)
		if e != nil {
			return nil, e
		}
		for id := range ids {
			if touched[id] {
				return nil, contentFailure(field("patches", "overlap"))
			}
			touched[id] = true
		}
	}
	raw, e := json.Marshal(content)
	if e != nil {
		return nil, e
	}
	var result []Node
	if e = json.Unmarshal(raw, &result); e != nil {
		return nil, e
	}
	for index, patch := range patches {
		switch value := patch.Value.(type) {
		case *ReplaceNode:
			key, _ := canonicalID(value.NodeID)
			location, ok := locate(result, key, nil)
			if !ok {
				return nil, contentFailure(field(fmt.Sprintf("patches[%d]", index), "unknown_node"))
			}
			children := childContainer(&result, location.path[:len(location.path)-1])
			(*children)[location.path[len(location.path)-1]] = value.Node
		case *RemoveNode:
			key, _ := canonicalID(value.NodeID)
			location, ok := locate(result, key, nil)
			if !ok {
				return nil, contentFailure(field("patches", "unknown_node"))
			}
			children := childContainer(&result, location.path[:len(location.path)-1])
			position := location.path[len(location.path)-1]
			*children = append((*children)[:position], (*children)[position+1:]...)
		case *InsertNodes:
			children, e := containerFor(&result, value.ParentID)
			if e != nil {
				return nil, e
			}
			position, e := insertionIndex(*children, value.BeforeID)
			if e != nil {
				return nil, e
			}
			insertAt(children, position, value.Nodes)
		case *MoveNode:
			key, _ := canonicalID(value.NodeID)
			location, ok := locate(result, key, nil)
			if !ok {
				return nil, contentFailure(field("patches", "unknown_node"))
			}
			children := childContainer(&result, location.path[:len(location.path)-1])
			position := location.path[len(location.path)-1]
			*children = append((*children)[:position], (*children)[position+1:]...)
			destination, e := containerFor(&result, value.ParentID)
			if e != nil {
				return nil, e
			}
			before, e := insertionIndex(*destination, value.BeforeID)
			if e != nil {
				return nil, e
			}
			insertAt(destination, before, []Node{location.node})
		case *ReplaceSection:
			start, end, e := section(result, value.HeadingID, value.ExpectedMemberIDs)
			if e != nil {
				return nil, e
			}
			replacement := make([]Node, 0, len(result)-end+start+len(value.Nodes))
			replacement = append(replacement, result[:start]...)
			replacement = append(replacement, value.Nodes...)
			replacement = append(replacement, result[end:]...)
			result = replacement
		}
	}
	if errors := ValidateContent(result); len(errors) > 0 {
		return nil, contentFailure(errors)
	}
	return result, nil
}
