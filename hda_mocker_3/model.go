package main

import (
	"fmt"
	"time"
)

type nodeKind uint8

const (
	kindConstant nodeKind = iota
	kindDynamic
	kindBadRealtime
)

type NodeSpec struct {
	ID       string
	Value    any
	Kind     nodeKind
	Writable bool
}

func buildSpecs(s Settings) map[string]NodeSpec {
	m := make(map[string]NodeSpec, s.TypeGroups*24+s.DynamicCount+s.StaticCount+s.BadCount)
	type item struct {
		name  string
		value any
	}
	types := []item{{"boolean", true}, {"sbyte", int8(-1)}, {"byte", uint8(1)}, {"int16", int16(-16)}, {"uint16", uint16(16)}, {"int32", int32(-32)}, {"uint32", uint32(32)}, {"int64", int64(-64)}, {"uint64", uint64(64)}, {"float", float32(1.25)}, {"double", 2.5}, {"string", "mock"}, {"datetime", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}
	for _, t := range types {
		for i := 1; i <= s.TypeGroups; i++ {
			r := fmt.Sprintf("inter_%s_r_%04d", t.name, i)
			w := fmt.Sprintf("inter_%s_w_%04d", t.name, i)
			m[r] = NodeSpec{ID: r, Value: t.value}
			m[w] = NodeSpec{ID: w, Value: t.value, Writable: true}
		}
	}
	for i := 1; i <= s.DynamicCount; i++ {
		id := fmt.Sprintf("dynamic_%04d", i)
		m[id] = NodeSpec{ID: id, Value: 1.0, Kind: kindDynamic}
	}
	for i := 1; i <= s.StaticCount; i++ {
		id := fmt.Sprintf("static_%04d", i)
		m[id] = NodeSpec{ID: id, Value: 42.0, Writable: true}
	}
	for i := 1; i <= s.BadCount; i++ {
		id := fmt.Sprintf("bad_realtime_%04d", i)
		m[id] = NodeSpec{ID: id, Value: 1.0, Kind: kindBadRealtime}
	}
	return m
}

func valueAt(spec NodeSpec, second int64) any {
	if spec.Kind == kindDynamic || spec.Kind == kindBadRealtime {
		return float64(second%100 + 1)
	}
	return spec.Value
}
