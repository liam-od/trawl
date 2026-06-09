package job

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Move is one relocation in a SortPlan: Src names a file or directory within the
// inbox, Dest its destination within the library (including the final folder and
// name). Both are slash-separated and relative to their respective roots.
type Move struct {
	Src  string `json:"src"`
	Dest string `json:"dest"`
}

// SortPlan is the JSON payload passed to --sort. Inbox and Library are the two
// base directories (absolute or ~); Moves lists the relocations to perform under
// them. It drives a local-to-local media sort, so unlike a transfer it names no
// host and touches no network.
type SortPlan struct {
	Inbox   string `json:"inbox"`
	Library string `json:"library"`
	Moves   []Move `json:"moves"`
}

// ParseSortPlan decodes s into a SortPlan and validates it. Unknown fields are
// rejected so a typo'd key is a loud error rather than a silently ignored
// instruction. Every move's Src and Dest must stay within their base directory:
// no leading slash and no ".." segment. An empty Moves list is allowed (nothing
// to sort).
func ParseSortPlan(s string) (SortPlan, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()

	var plan SortPlan
	if err := dec.Decode(&plan); err != nil {
		return SortPlan{}, fmt.Errorf("parse sort plan: %w", err)
	}
	if dec.More() {
		return SortPlan{}, fmt.Errorf("parse sort plan: trailing data after JSON object")
	}

	if plan.Inbox == "" {
		return SortPlan{}, fmt.Errorf("sort plan: \"inbox\" is required")
	}
	if plan.Library == "" {
		return SortPlan{}, fmt.Errorf("sort plan: \"library\" is required")
	}
	for i, m := range plan.Moves {
		if err := validateRel("src", m.Src); err != nil {
			return SortPlan{}, fmt.Errorf("sort plan: move %d: %w", i, err)
		}
		if err := validateRel("dest", m.Dest); err != nil {
			return SortPlan{}, fmt.Errorf("sort plan: move %d: %w", i, err)
		}
	}
	return plan, nil
}

// validateRel rejects a relative path that is empty or would escape its base
// directory. Paths are always slash-separated (they come from JSON), so the
// check is in terms of forward-slash segments.
func validateRel(field, p string) error {
	if p == "" {
		return fmt.Errorf("%q is required", field)
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("%q must be relative, not %q", field, p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("%q must not contain %q", field, "..")
		}
	}
	return nil
}
