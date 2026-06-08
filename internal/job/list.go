package job

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Side values for ListSpec.Side.
const (
	SideRemote = "remote"
	SideLocal  = "local"
)

// ListSpec is the JSON payload passed to --list. Name selects a saved host and
// Side which of its two directories to walk; both are required. Path overrides
// the host's configured remote_dir/local_dir base for the chosen side. Depth
// caps how many levels below the base are walked; 0 means no limit.
type ListSpec struct {
	Name  string `json:"name"`
	Side  string `json:"side"`
	Path  string `json:"path,omitempty"`
	Depth int    `json:"depth,omitempty"`
}

// ParseList decodes s into a ListSpec and validates it. Unknown fields are
// rejected so a typo'd key is a loud error rather than a silently ignored
// instruction. Unlike a transfer's Object, Path is a full base-directory
// override (absolute or ~), so it carries no relative-path guard.
func ParseList(s string) (ListSpec, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()

	var spec ListSpec
	if err := dec.Decode(&spec); err != nil {
		return ListSpec{}, fmt.Errorf("parse list spec: %w", err)
	}
	if dec.More() {
		return ListSpec{}, fmt.Errorf("parse list spec: trailing data after JSON object")
	}

	if spec.Name == "" {
		return ListSpec{}, fmt.Errorf("list spec: \"name\" is required")
	}
	switch spec.Side {
	case SideRemote, SideLocal:
	case "":
		return ListSpec{}, fmt.Errorf("list spec: \"side\" is required (%s or %s)", SideRemote, SideLocal)
	default:
		return ListSpec{}, fmt.Errorf("list spec: unknown side %q (want %s or %s)", spec.Side, SideRemote, SideLocal)
	}
	if spec.Depth < 0 {
		return ListSpec{}, fmt.Errorf("list spec: \"depth\" must not be negative")
	}
	return spec, nil
}
