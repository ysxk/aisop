package category

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name          string
		in            input
		create, valid bool
	}{
		{"minimal", input{Name: new("鞋靴")}, true, true},
		{"trim", input{Name: new("  鞋靴  ")}, true, true},
		{"missing", input{}, true, false},
		{"blank", input{Name: new(" \t ")}, true, false},
		{"long name", input{Name: new(strings.Repeat("鞋", 101))}, true, false},
		{"max name", input{Name: new(strings.Repeat("鞋", 100))}, true, true},
		{"nul", input{Name: new("bad\x00")}, true, false},
		{"empty patch", input{}, false, false},
		{"empty description", input{Description: new("")}, false, true},
		{"long description", input{Description: new(strings.Repeat("字", 2001))}, false, false},
		{"max description", input{Description: new(strings.Repeat("字", 2000))}, false, true},
		{"negative order", input{SortOrder: new(int64(-1))}, false, false},
		{"zero order", input{SortOrder: new(int64(0))}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validate(tc.in, tc.create)
			if (err == nil) != tc.valid {
				t.Fatalf("validate=%v", err)
			}
			if tc.name == "trim" && *tc.in.Name != "鞋靴" {
				t.Fatal("name not trimmed")
			}
		})
	}
}
