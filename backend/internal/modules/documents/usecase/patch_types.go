package usecase

import (
	"encoding/json"
	"fmt"
)

type Patch struct {
	Value PatchValue `json:"-"`
}
type PatchValue interface{ platePatch() }
type ReplaceNode struct {
	Op     string `json:"op"`
	NodeID string `json:"node_id"`
	Node   Node   `json:"node"`
}
type InsertNodes struct {
	Op       string  `json:"op"`
	ParentID *string `json:"parent_id"`
	BeforeID *string `json:"before_id"`
	Nodes    []Node  `json:"nodes"`
}
type RemoveNode struct {
	Op     string `json:"op"`
	NodeID string `json:"node_id"`
}
type MoveNode struct {
	Op       string  `json:"op"`
	NodeID   string  `json:"node_id"`
	ParentID *string `json:"parent_id"`
	BeforeID *string `json:"before_id"`
}
type ReplaceSection struct {
	Op                string   `json:"op"`
	HeadingID         string   `json:"heading_id"`
	ExpectedMemberIDs []string `json:"expected_member_ids"`
	Nodes             []Node   `json:"nodes"`
}

func (*ReplaceNode) platePatch()    {}
func (*InsertNodes) platePatch()    {}
func (*RemoveNode) platePatch()     {}
func (*MoveNode) platePatch()       {}
func (*ReplaceSection) platePatch() {}
func (p Patch) MarshalJSON() ([]byte, error) {
	if p.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(p.Value)
}
func (p *Patch) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(raw, &fields); e != nil {
		return e
	}
	var op string
	if e := json.Unmarshal(fields["op"], &op); e != nil {
		return e
	}
	var value PatchValue
	switch op {
	case "replace_node":
		value = &ReplaceNode{}
	case "insert_nodes":
		value = &InsertNodes{}
	case "remove_node":
		value = &RemoveNode{}
	case "move_node":
		value = &MoveNode{}
	case "replace_section":
		value = &ReplaceSection{}
	default:
		return fmt.Errorf("unsupported document patch")
	}
	if e := strict(raw, value); e != nil {
		return e
	}
	p.Value = value
	return nil
}
