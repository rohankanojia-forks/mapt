// Copyright 2023, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package eval

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/pulumi/esc"
	"github.com/pulumi/esc/ast"
	"github.com/pulumi/esc/schema"
	"golang.org/x/exp/maps"
)

// A value represents the result of evaluating an expr.
//
// Each value may have a base value. The base value is the value (if any) that this value is merged with per JSON merge
// patch semantics. This has important consequences for object values: when calculating the properties for an object
// value, the properties of its base must be taken into account, and the result must be valid with respect to JSON merge
// patch. Consumers should never directly examine the map[string]*value of an object value, and instead should use the
// keys() and property() methods to access an object value's contents.
type value struct {
	def    *expr          // the expression that produced this value
	base   *value         // the base value, if any
	schema *schema.Schema // the value's schema

	mergedKeys []string   // the value's merged keys. computed lazily--use keys().
	exported   *esc.Value // non-nil if this value has already been exported

	// true if the value is unknown (e.g. because it did not evaluate successfully or is the result of an unevaluated
	// fn::open)
	unknown    bool
	rotateOnly bool
	secret     bool // true if the value is secret

	repr any // nil | bool | json.Number | string | []*value | map[string]*value
}

func (v *value) GoString() string {
	if v == nil {
		return "<nil>"
	}
	r := v.def.defRange("")
	return fmt.Sprintf("%q @%v:%v (%p)", v.def.path, r.Environment, r.Begin, v)
}

// containsUnknowns returns true if the value contains any unknown values.
func (v *value) containsUnknowns() bool {
	if v == nil {
		return false
	}
	if v.unknown {
		return true
	}
	switch repr := v.repr.(type) {
	case []*value:
		for _, v := range repr {
			if v.containsUnknowns() {
				return true
			}
		}
	case map[string]*value:
		for _, k := range v.keys() {
			if v.property(v.def.repr.syntax(), k).containsUnknowns() {
				return true
			}
		}
	}
	return false
}

// containsObservableUnknowns returns true if the value contains any unknown values.
// the rotating flag indicate our tolerance of unknown rotateOnly values.
func (v *value) containsObservableUnknowns(rotating bool) bool {
	if v == nil {
		return false
	}
	if v.unknown {
		if v.rotateOnly && !rotating {
			return false // this unknown value will not be observed
		}
		return true
	}
	switch repr := v.repr.(type) {
	case []*value:
		for _, v := range repr {
			if v.containsObservableUnknowns(rotating) {
				return true
			}
		}
	case map[string]*value:
		for _, k := range v.keys() {
			if v.property(v.def.repr.syntax(), k).containsObservableUnknowns(rotating) {
				return true
			}
		}
	}
	return false
}

// containsSecrets returns true if the value contains any secret values.
func (v *value) containsSecrets() bool {
	if v == nil {
		return false
	}
	if v.secret {
		return true
	}
	switch repr := v.repr.(type) {
	case []*value:
		for _, v := range repr {
			if v.containsSecrets() {
				return true
			}
		}
	case map[string]*value:
		for _, k := range v.keys() {
			if v.property(v.def.repr.syntax(), k).containsSecrets() {
				return true
			}
		}
	}
	return false
}

// isObject returns true if this value is or may be an object.
func (v *value) isObject() bool {
	if v == nil {
		return false
	}
	if v.unknown {
		return v.schema.Always || v.schema.Type == "object"
	}
	_, ok := v.repr.(map[string]*value)
	return ok
}

func (v *value) is(other *value) bool {
	if v == other {
		return true
	}
	if v.unknown && other.unknown {
		vaccess, vok := v.def.repr.(*accessExpr)
		oaccess, ook := other.def.repr.(*accessExpr)
		if vok && ook {
			vname, vok := vaccess.accessor.(*ast.PropertyName)
			oname, ook := oaccess.accessor.(*ast.PropertyName)
			if vok && ook {
				return vaccess.receiver.is(oaccess.receiver) && vname.Name == oname.Name
			}
		}
	}
	return false
}

// combine combines the unknown-ness and secret-ness of the given values and applies the result to the receiver.
// If any of the inputs contains unknowns or secrets, the receiver is unknown or secret. This should only be used when
// computing an aggregate value from other values (e.g. the output of fn::join is unknown if any of its inputs
// are unknown).
func (v *value) combine(others ...*value) {
	for _, o := range others {
		v.unknown = v.containsUnknowns() || o.containsUnknowns()
		v.secret = v.containsSecrets() || o.containsSecrets()
	}
}

// keys returns the value's keys if the value is an object. This method should be used instead of accessing the
// underlying map[string]*value directly, as it takes JSON merge patch semantics into account.
func (v *value) keys() []string {
	if v == nil {
		return nil
	}
	if v.mergedKeys == nil {
		m, ok := v.repr.(map[string]*value)
		if !ok {
			return nil
		}

		baseKeys := v.base.keys()
		if len(baseKeys) == 0 {
			v.mergedKeys = maps.Keys(m)
		} else {
			l := len(baseKeys)
			if l < len(m) {
				l = len(m)
			}
			keySet := make(map[string]struct{}, l)

			for _, k := range baseKeys {
				keySet[k] = struct{}{}
			}
			for k := range m {
				keySet[k] = struct{}{}
			}
			v.mergedKeys = maps.Keys(keySet)
		}
		sort.Strings(v.mergedKeys)
	}
	return v.mergedKeys
}

// property returns the named property (if any) as per JSON merge patch semantics. If the receiver is unknown,
// this method returns a late-bound access. This should only happen in case of an error or during validation.
func (v *value) property(x ast.Expr, key string) *value {
	if v == nil {
		return nil
	}

	if object, ok := v.repr.(map[string]*value); ok {
		if v, ok := object[key]; ok {
			return v
		}
		return v.base.property(x, key)
	}

	if v.unknown {
		schema, base := v.schema.Property(key), v.base.property(x, key)
		return &value{
			def: &expr{
				repr: &accessExpr{
					node:     x,
					receiver: v,
					accessor: &ast.PropertyName{Name: key},
				},
				schema: schema,
				state:  exprDone,
				base:   base,
			},
			base:    base,
			schema:  schema,
			unknown: true,
		}
	}

	return nil
}

// merge merges this value with the base. Note that this is mutating, and callers should probably make a copy prior to
// calling merge.
func (v *value) merge(base *value) {
	if base == nil || v.is(base) {
		return
	}

	if v.base != nil {
		// If this value already has a base, apply the merge to its base.
		v.base.merge(base)
	} else {
		// Otherwise, set the base.
		v.base = base
	}

	// If this is an object, re-merge each property.
	if object, ok := v.repr.(map[string]*value); ok {
		for k, p := range object {
			p.merge(v.base.property(p.def.repr.syntax(), k))
		}
	}

	// finally, update the value's schema with the merged schema.
	v.schema = mergedSchema(v.base.schema, v.schema)
}

// toString returns the string representation of this value, whether the string is known, and whether the string is
// secret.
func (v *value) toString() (str string, unknown bool, secret bool) {
	if v.unknown {
		return "[unknown]", true, v.secret
	}

	s, unknown, secret := "", false, v.secret
	switch repr := v.repr.(type) {
	case bool:
		if repr {
			s = "true"
		} else {
			s = "false"
		}
	case json.Number:
		s = repr.String()
	case string:
		s = repr
	case []*value:
		vals := make([]string, len(repr))
		for i, v := range repr {
			vs, vunknown, vsecret := v.toString()
			vals[i], unknown, secret = strconv.Quote(vs), unknown || vunknown, secret || vsecret
		}
		s = strings.Join(vals, ",")
	case map[string]*value:
		keys := maps.Keys(repr)
		sort.Strings(keys)

		pairs := make([]string, len(repr))
		for i, k := range keys {
			vs, vunknown, vsecret := repr[k].toString()
			pairs[i], unknown, secret = fmt.Sprintf("%q=%q", k, vs), unknown || vunknown, secret || vsecret
		}
		s = strings.Join(pairs, ",")
	}
	return s, unknown, secret
}

// export converts the value into its serializable representation.
func (v *value) export(environment string) esc.Value {
	if v.exported != nil {
		return *v.exported
	}

	var pv any
	switch repr := v.repr.(type) {
	case []*value:
		a := make([]esc.Value, len(repr))
		for i, v := range repr {
			a[i] = v.export(environment)
		}
		pv = a
	case map[string]*value:
		keys := v.keys()
		pm := make(map[string]esc.Value, len(keys))
		for _, k := range keys {
			pv := v.property(v.def.repr.syntax(), k)
			pm[k] = pv.export(environment)
		}
		pv = pm
	default:
		pv = repr
	}

	var base *esc.Value
	if v.base != nil {
		b := v.base.export("<import>")
		base = &b
	}

	v.exported = &esc.Value{
		Value:   pv,
		Secret:  v.secret,
		Unknown: v.unknown,
		Trace: esc.Trace{
			Def:  v.def.defRange(environment),
			Base: base,
		},
	}
	return *v.exported
}

// unexport creates a value from a Value. This is used when interacting with providers, as the Provider API works on
// Values, but the evaluator needs values.
func unexport(v esc.Value, x *expr) *value {
	vv := &value{def: x, secret: v.Secret || x.secret, unknown: v.Unknown}
	switch pv := v.Value.(type) {
	case nil:
		vv.repr, vv.schema = nil, schema.Null().Schema()
	case bool:
		vv.repr, vv.schema = pv, schema.Boolean().Const(pv).Schema()
	case json.Number:
		vv.repr, vv.schema = pv, schema.Number().Const(pv).Schema()
	case string:
		vv.repr, vv.schema = pv, schema.String().Const(pv).Schema()
	case []esc.Value:
		a, items := make([]*value, len(pv)), make([]schema.Builder, len(pv))
		for i, v := range pv {
			uv := unexport(v, x)
			a[i], items[i] = uv, uv.schema
		}
		vv.repr, vv.schema = a, schema.Tuple(items...).Schema()
	case map[string]esc.Value:
		m, properties := make(map[string]*value, len(pv)), make(schema.SchemaMap, len(pv))
		for k, v := range pv {
			uv := unexport(v, x)
			m[k], properties[k] = uv, uv.schema
		}
		vv.repr, vv.schema = m, schema.Record(properties).Schema()
	default:
		panic(fmt.Errorf("unexpected property of type %T", pv))
	}
	return vv
}

// mergedSchema computes the result of merging the base and top schemas per JSON merge patch semantics.
func mergedSchema(base, top *schema.Schema) *schema.Schema {
	if base == nil || top.Type != "object" {
		return top
	}
	if base.Type != "object" {
		return top
	}

	l := len(base.Properties)
	if l < len(top.Properties) {
		l = len(top.Properties)
	}

	record := make(schema.SchemaMap, l)
	for k, base := range base.Properties {
		record[k] = base
	}
	for k, top := range top.Properties {
		if base, ok := record[k]; ok {
			record[k] = mergedSchema(base.Schema(), top)
		} else {
			record[k] = top
		}
	}

	additional := top.AdditionalProperties
	if base.AdditionalProperties != nil {
		if additional == nil {
			additional = base.AdditionalProperties
		} else {
			// TODO(pdg): anyof?
			additional = schema.Always().Schema()
		}
	}

	return schema.Record(record).AdditionalProperties(additional).Schema()
}

type copier struct {
	memo map[*value]*value
}

func newCopier() copier {
	return copier{memo: map[*value]*value{}}
}

func (c copier) copy(v *value) *value {
	if v == nil {
		return nil
	}

	if copy, ok := c.memo[v]; ok {
		return copy
	}

	copy := &value{}
	c.memo[v] = copy

	var repr any
	switch vr := v.repr.(type) {
	case []*value:
		a := make([]*value, len(vr))
		for i, v := range vr {
			a[i] = c.copy(v)
		}
		repr = a
	case map[string]*value:
		m := make(map[string]*value, len(vr))
		for k, v := range vr {
			m[k] = c.copy(v)
		}
		repr = m
	default:
		repr = vr
	}

	*copy = value{
		def:     v.def,
		base:    c.copy(v.base),
		schema:  v.schema,
		unknown: v.unknown,
		secret:  v.secret,
		repr:    repr,
	}
	return copy
}
