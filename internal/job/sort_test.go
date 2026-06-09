package job

import (
	"strings"
	"testing"
)

func TestParseSortPlan(t *testing.T) {
	const ok = `{"inbox":"~/Inbox","library":"~/Server/Library",` +
		`"moves":[{"src":"a.mkv","dest":"Movies/A (2020)/a.mkv"}]}`

	plan, err := ParseSortPlan(ok)
	if err != nil {
		t.Fatalf("ParseSortPlan: %v", err)
	}
	if plan.Inbox != "~/Inbox" || plan.Library != "~/Server/Library" {
		t.Errorf("bases = %q, %q", plan.Inbox, plan.Library)
	}
	if len(plan.Moves) != 1 || plan.Moves[0].Src != "a.mkv" ||
		plan.Moves[0].Dest != "Movies/A (2020)/a.mkv" {
		t.Errorf("moves = %+v", plan.Moves)
	}
}

func TestParseSortPlanEmptyMoves(t *testing.T) {
	plan, err := ParseSortPlan(`{"inbox":"in","library":"lib","moves":[]}`)
	if err != nil {
		t.Fatalf("ParseSortPlan with no moves should be allowed: %v", err)
	}
	if len(plan.Moves) != 0 {
		t.Errorf("moves = %+v, want none", plan.Moves)
	}
}

func TestParseSortPlanErrors(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{"missing inbox", `{"library":"lib","moves":[]}`, "\"inbox\" is required"},
		{"missing library", `{"inbox":"in","moves":[]}`, "\"library\" is required"},
		{"unknown field", `{"inbox":"in","library":"lib","moves":[],"x":1}`, "parse sort plan"},
		{"trailing data", `{"inbox":"in","library":"lib","moves":[]}{}`, "trailing data"},
		{"empty src", `{"inbox":"in","library":"lib","moves":[{"src":"","dest":"d"}]}`, "\"src\" is required"},
		{"absolute src", `{"inbox":"in","library":"lib","moves":[{"src":"/etc/x","dest":"d"}]}`, "must be relative"},
		{"dotdot dest", `{"inbox":"in","library":"lib","moves":[{"src":"s","dest":"../x"}]}`, "must not contain"},
		{"dotdot mid dest", `{"inbox":"in","library":"lib","moves":[{"src":"s","dest":"a/../b"}]}`, "must not contain"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSortPlan(tc.json)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}
