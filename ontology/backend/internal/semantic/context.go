package semantic

import (
	"encoding/json"
	"fmt"
	"slices"
)

// ClassContext describes definitions only, never observed instance facts.
type ClassContext struct {
	Class      Class         `json:"class"`
	Ancestors  []Class       `json:"ancestors"`
	Properties []Property    `json:"properties"`
	Relations  []Relation    `json:"relations"`
	Rules      []ContextRule `json:"rules"`
}

// ContextRule exposes whether a rule has a published fact binding without
// exposing the physical service mapping to semantic readers.
type ContextRule struct {
	ID                string  `json:"id"`
	ClassID           string  `json:"class_id"`
	Expression        string  `json:"expression"`
	Basis             string  `json:"basis"`
	Inputs            []Input `json:"inputs"`
	BusinessEvaluable bool    `json:"business_evaluable,omitempty"`
}

func (s *Snapshot) Classes() []Class {
	classes := make([]Class, len(s.definition.Classes))
	for i, c := range s.definition.Classes {
		c.Parents = append([]string{}, c.Parents...)
		classes[i] = c
	}
	return classes
}

func (s *Snapshot) ClassContext(id string) (*ClassContext, error) {
	ancestors, err := s.Ancestors(id)
	if err != nil {
		return nil, err
	}
	result := ClassContext{Ancestors: []Class{}, Properties: []Property{}, Relations: []Relation{}, Rules: []ContextRule{}}
	for _, c := range s.definition.Classes {
		if c.ID == id {
			result.Class = c
		}
		if slices.Contains(ancestors, c.ID) {
			result.Ancestors = append(result.Ancestors, c)
		}
	}
	for _, p := range s.definition.Properties {
		if p.ClassID == id || slices.Contains(ancestors, p.ClassID) {
			result.Properties = append(result.Properties, p)
		}
	}
	for _, r := range s.definition.Relations {
		if r.From == id || r.To == id {
			result.Relations = append(result.Relations, r)
		}
	}
	for _, r := range s.definition.Rules {
		if r.ClassID == id {
			result.Rules = append(result.Rules, ContextRule{ID: r.ID, ClassID: r.ClassID, Expression: r.Expression,
				Basis: r.Basis, Inputs: r.Inputs, BusinessEvaluable: r.ServiceBinding != nil})
		}
	}
	// Return detached arrays, preserving the snapshot's immutability.
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("context_encoding_failed")
	}
	var copy ClassContext
	if err := json.Unmarshal(data, &copy); err != nil {
		return nil, err
	}
	return &copy, nil
}
