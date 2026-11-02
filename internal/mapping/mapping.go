// Package mapping turns CSV tables into FollowTheMoney entities using a
// mapping file in the format of `ftm map`.
//
// Only CSV sources (csv_url) are supported. Property values are not
// validated against the FollowTheMoney model: the OpenAleph API does that
// when the entities are written.
package mapping

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// File is a parsed mapping file.
type File struct {
	// Dataset prefixes the generated entity IDs so that the same keys in
	// different datasets do not collide.
	Dataset string  `yaml:"dataset"`
	Queries []Query `yaml:"queries"`
}

// Query maps the rows of one CSV file to entities.
type Query struct {
	CSVURL     string                `yaml:"csv_url"`
	Filters    map[string]string     `yaml:"filters"`
	FiltersNot map[string]string     `yaml:"filters_not"`
	Entities   map[string]*EntityDef `yaml:"entities"`
}

// EntityDef describes one entity that is created per row.
type EntityDef struct {
	Schema     string               `yaml:"schema"`
	IDColumn   string               `yaml:"id_column"`
	Keys       stringList           `yaml:"keys"`
	KeyLiteral stringList           `yaml:"key_literal"`
	Properties map[string]*Property `yaml:"properties"`
}

// Property describes where the values of one property come from.
type Property struct {
	Column   string            `yaml:"column"`
	Columns  stringList        `yaml:"columns"`
	Literal  string            `yaml:"literal"`
	Literals stringList        `yaml:"literals"`
	Entity   string            `yaml:"entity"`
	Join     string            `yaml:"join"`
	Split    stringList        `yaml:"split"`
	Replace  map[string]string `yaml:"replace"`
	Required bool              `yaml:"required"`
}

// stringList accepts a single scalar or a list of scalars.
type stringList []string

func (s *stringList) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		*s = stringList{n.Value}
	case yaml.SequenceNode:
		out := make(stringList, 0, len(n.Content))
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: expected a plain value in the list", c.Line)
			}
			out = append(out, c.Value)
		}
		*s = out
	default:
		return fmt.Errorf("line %d: expected a value or a list of values", n.Line)
	}
	return nil
}

// Load reads and validates a mapping file.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse reads and validates a mapping from YAML.
func Parse(b []byte) (*File, error) {
	var f File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse mapping: %w", err)
	}
	if err := f.validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

func (f *File) validate() error {
	if len(f.Queries) == 0 {
		return fmt.Errorf("mapping has no queries")
	}
	for i, q := range f.Queries {
		where := fmt.Sprintf("query %d", i+1)
		if q.CSVURL == "" {
			return fmt.Errorf("%s: csv_url is required (only CSV sources are supported)", where)
		}
		if len(q.Entities) == 0 {
			return fmt.Errorf("%s: no entities", where)
		}
		for _, name := range q.entityNames() {
			e := q.Entities[name]
			if e == nil || e.Schema == "" {
				return fmt.Errorf("%s: entity %q needs a schema", where, name)
			}
			if e.IDColumn == "" && len(e.Keys) == 0 && len(e.KeyLiteral) == 0 {
				return fmt.Errorf("%s: entity %q needs keys, key_literal or id_column", where, name)
			}
			for prop, p := range e.Properties {
				if p == nil {
					return fmt.Errorf("%s: entity %q: property %q is empty", where, name, prop)
				}
				if err := p.validate(q); err != nil {
					return fmt.Errorf("%s: entity %q: property %q: %w", where, name, prop, err)
				}
			}
		}
	}
	return nil
}

func (p *Property) validate(q Query) error {
	sources := 0
	for _, set := range []bool{
		p.Column != "", len(p.Columns) > 0, p.Literal != "", len(p.Literals) > 0, p.Entity != "",
	} {
		if set {
			sources++
		}
	}
	if sources != 1 {
		return fmt.Errorf("set exactly one of column, columns, literal, literals or entity")
	}
	if p.Entity != "" {
		if _, ok := q.Entities[p.Entity]; !ok {
			return fmt.Errorf("refers to unknown entity %q", p.Entity)
		}
	}
	return nil
}

// entityNames returns the entity names in a stable order.
func (q Query) entityNames() []string {
	names := make([]string, 0, len(q.Entities))
	for n := range q.Entities {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
