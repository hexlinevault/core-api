package utils

import (
	"encoding/json"
	"testing"
	"time"
)

// --- JSON-based clone for comparison ---

func cloneJSON[T any](p *T) *T {
	if p == nil {
		return nil
	}
	data, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		panic(err)
	}
	return &out
}

// --- Test structs ---

type flat struct {
	ID     uint64  `json:"id"`
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Active bool    `json:"active"`
}

type nested3 struct {
	ID    uint64  `json:"id"`
	Name  string  `json:"name"`
	Child *level2 `json:"child"`
}
type level2 struct {
	Tags  []string          `json:"tags"`
	Meta  map[string]string `json:"meta"`
	Inner *level3           `json:"inner"`
}
type level3 struct {
	Value   string    `json:"value"`
	Created time.Time `json:"created"`
	Score   float64   `json:"score"`
}

type heavy struct {
	ID       uint64            `json:"id"`
	Name     string            `json:"name"`
	Tags     []string          `json:"tags"`
	Meta     map[string]any    `json:"meta"`
	Items    []flat            `json:"items"`
	Children []*nested3        `json:"children"`
	Scores   map[string][]int  `json:"scores"`
}

// --- Fixtures ---

func makeFlat() *flat {
	return &flat{ID: 1, Name: "test", Amount: 99.9, Active: true}
}

func makeNested3() *nested3 {
	return &nested3{
		ID:   1,
		Name: "parent",
		Child: &level2{
			Tags: []string{"a", "b", "c"},
			Meta: map[string]string{"k1": "v1", "k2": "v2"},
			Inner: &level3{
				Value:   "deep",
				Created: time.Now(),
				Score:   3.14,
			},
		},
	}
}

func makeHeavy() *heavy {
	items := make([]flat, 50)
	for i := range items {
		items[i] = flat{ID: uint64(i), Name: "item", Amount: float64(i) * 1.1, Active: i%2 == 0}
	}
	children := make([]*nested3, 10)
	for i := range children {
		children[i] = makeNested3()
	}
	return &heavy{
		ID:   100,
		Name: "heavy",
		Tags: []string{"x", "y", "z", "w"},
		Meta: map[string]any{"count": 42, "label": "bench", "nested": map[string]any{"a": 1}},
		Items:    items,
		Children: children,
		Scores:   map[string][]int{"math": {90, 95, 100}, "sci": {80, 85}},
	}
}

// --- Benchmarks: Flat struct (primitives only) ---

func BenchmarkCloneDeep_Reflect_Flat(b *testing.B) {
	src := makeFlat()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CloneDeep(src)
	}
}

func BenchmarkCloneDeep_JSON_Flat(b *testing.B) {
	src := makeFlat()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cloneJSON(src)
	}
}

// --- Benchmarks: 3-level nested pointers ---

func BenchmarkCloneDeep_Reflect_Nested3(b *testing.B) {
	src := makeNested3()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CloneDeep(src)
	}
}

func BenchmarkCloneDeep_JSON_Nested3(b *testing.B) {
	src := makeNested3()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cloneJSON(src)
	}
}

// --- Benchmarks: Heavy struct (slices, maps, nested) ---

func BenchmarkCloneDeep_Reflect_Heavy(b *testing.B) {
	src := makeHeavy()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CloneDeep(src)
	}
}

func BenchmarkCloneDeep_JSON_Heavy(b *testing.B) {
	src := makeHeavy()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cloneJSON(src)
	}
}

// --- Correctness test: verify deep independence ---

func TestCloneDeep_Nested3_Independence(t *testing.T) {
	src := makeNested3()
	cp := CloneDeep(src)

	cp.Name = "changed"
	cp.Child.Tags[0] = "CHANGED"
	cp.Child.Meta["k1"] = "CHANGED"
	cp.Child.Inner.Value = "CHANGED"

	if src.Name == "changed" {
		t.Error("top-level field leaked")
	}
	if src.Child.Tags[0] == "CHANGED" {
		t.Error("nested slice leaked")
	}
	if src.Child.Meta["k1"] == "CHANGED" {
		t.Error("nested map leaked")
	}
	if src.Child.Inner.Value == "CHANGED" {
		t.Error("3rd-level pointer leaked")
	}
}
