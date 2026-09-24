// internal/apps/flash/import_prompt_test.go
package flash

import (
	"reflect"
	"strings"
	"testing"
)

// jsonFieldNames collects every json tag name in t, recursing into structs
// and slices of structs — the importer's whole accepted schema.
func jsonFieldNames(t reflect.Type) []string {
	for t.Kind() == reflect.Slice || t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var names []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if tag, _, _ := strings.Cut(f.Tag.Get("json"), ","); tag != "" && tag != "-" {
			names = append(names, tag)
		}
		names = append(names, jsonFieldNames(f.Type)...)
	}
	return names
}

// TestImportPromptCoversTheSchema keeps the AI prompt from drifting away
// from what the importer accepts: every JSON field importJSON knows must
// be named in the prompt (UI overhaul spec §7).
func TestImportPromptCoversTheSchema(t *testing.T) {
	names := jsonFieldNames(reflect.TypeOf(importJSON{}))
	if len(names) < 5 {
		t.Fatalf("found only %v json fields; the reflection walk is broken", names)
	}
	for _, name := range names {
		if !strings.Contains(importPrompt, `"`+name+`"`) {
			t.Errorf("the import prompt never mentions the %q field", name)
		}
	}
	for _, must := range []string{"[TOPIC]", "{{c1::", `"basic"`, `"cloze"`} {
		if !strings.Contains(importPrompt, must) {
			t.Errorf("the import prompt is missing %q", must)
		}
	}
}
