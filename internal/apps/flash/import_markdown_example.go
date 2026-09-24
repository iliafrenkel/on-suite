// internal/apps/flash/import_markdown_example.go
package flash

// ImportMarkdownExample is the worked example shown in the import pane's
// collapsed "Writing it by hand?" details block (#302.6), for the person
// writing a deck by hand instead of asking an AI. It shows the same
// Image:/Audio: keys parseCardBlock understands (F4), alongside the
// already-documented Front/Back/Tags keys. It is a real, parseable
// example, not just documentation prose that could quietly drift out of
// sync with the parser: TestImportMarkdownExampleParses feeds this exact
// text to ParseImport and checks the cards it produces.
const ImportMarkdownExample = `# Deck name

## Card
Front: Question
Back: Answer
Tags: tag-one, tag-two
Image: https://example.com/picture.jpg
Audio: https://example.com/sound.mp3
`
