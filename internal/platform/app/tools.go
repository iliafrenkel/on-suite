package app

import (
	"encoding/json"
	"unicode/utf8"
)

// MaxToolText caps how much text one tool result may carry back to the model.
const MaxToolText = 200_000

// toolTextNote is appended to a result that CapToolText has cut short.
const toolTextNote = "\n\n[Cut at 200,000 characters. Ask for a smaller part.]"

// ParseToolArgs decodes a call's arguments; a client may send none at all
// for a tool whose arguments are all optional.
func ParseToolArgs(args json.RawMessage, into any) error {
	if len(args) == 0 {
		return nil
	}
	if err := json.Unmarshal(args, into); err != nil {
		return ToolError{Msg: "The arguments are not valid."}
	}
	return nil
}

// MarshalTool encodes a tool result as compact JSON.
func MarshalTool(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// CapToolText keeps the first MaxToolText runes of s and says so when it cuts.
func CapToolText(s string) string {
	if utf8.RuneCountInString(s) <= MaxToolText {
		return s
	}
	cut := 0
	for i := range s {
		if cut == MaxToolText {
			return s[:i] + toolTextNote
		}
		cut++
	}
	return s
}
