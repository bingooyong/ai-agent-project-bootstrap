// Package model defines the evidence-bearing interchange contracts.
package model

type Evidence struct {
	Path    string `json:"path" yaml:"path"`
	Pointer string `json:"pointer,omitempty" yaml:"pointer,omitempty"`
	SHA256  string `json:"sha256" yaml:"sha256"`
}

type Fact struct {
	Kind           string     `json:"kind" yaml:"kind"`
	Name           string     `json:"name" yaml:"name"`
	Value          string     `json:"value,omitempty" yaml:"value,omitempty"`
	DeclaredValue  string     `json:"declared_value,omitempty" yaml:"declared_value,omitempty"`
	DeclaredSource string     `json:"declared_source,omitempty" yaml:"declared_source,omitempty"`
	Conflict       bool       `json:"conflict,omitempty" yaml:"conflict,omitempty"`
	Confidence     float64    `json:"confidence" yaml:"confidence"`
	Status         string     `json:"status" yaml:"status"`
	Evidence       []Evidence `json:"evidence" yaml:"evidence"`
}

type Command struct {
	Name            string     `json:"name" yaml:"name"`
	Argv            []string   `json:"argv" yaml:"argv"`
	Cwd             string     `json:"cwd" yaml:"cwd"`
	Status          string     `json:"status" yaml:"status"`
	ExecutionStatus string     `json:"execution_status" yaml:"execution_status"`
	Evidence        []Evidence `json:"evidence" yaml:"evidence"`
}

type Module struct {
	Path      string   `json:"path" yaml:"path"`
	Manifest  string   `json:"manifest" yaml:"manifest"`
	Languages []string `json:"languages" yaml:"languages"`
}

type Coverage struct {
	Status       string   `json:"status" yaml:"status"`
	FilesVisited int      `json:"files_visited" yaml:"files_visited"`
	BytesRead    int64    `json:"bytes_read" yaml:"bytes_read"`
	Excluded     []string `json:"excluded" yaml:"excluded"`
}

type Profile struct {
	SchemaVersion string            `json:"schema_version" yaml:"schema_version"`
	Facts         []Fact            `json:"facts" yaml:"facts"`
	Commands      []Command         `json:"commands" yaml:"commands"`
	Modules       []Module          `json:"modules" yaml:"modules"`
	AgentConfigs  []string          `json:"agent_configs" yaml:"agent_configs"`
	UnknownItems  []string          `json:"unknown_items" yaml:"unknown_items"`
	Inputs        map[string]string `json:"inputs" yaml:"inputs"`
	Coverage      Coverage          `json:"coverage" yaml:"coverage"`
}
