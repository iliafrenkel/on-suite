// internal/apps/flash/import_prompt.go
package flash

import _ "embed"

// importPrompt is the ready-made AI prompt the import pane's "Copy the
// prompt" button copies (UI overhaul spec §7): it describes the JSON
// schema importJSON accepts, with a [TOPIC] placeholder for the person to
// fill in. TestImportPromptCoversTheSchema keeps it in step with the
// schema.
//
//go:embed import_prompt.txt
var importPrompt string
