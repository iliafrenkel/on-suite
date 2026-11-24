package notes

import (
	"context"
	"encoding/json"
	"math"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

type addToolArgs struct {
	Text     string `json:"text" jsonschema:"the bullet's text; anything after the first line becomes its note"`
	ParentID int64  `json:"parent_id,omitempty" jsonschema:"the id of the bullet to add under, from notes_search or notes_get; leave out for the top level"`
	Due      string `json:"due,omitempty" jsonschema:"a due date as YYYY-MM-DD; leave out for none"`
}

type setDueToolArgs struct {
	ID  int64   `json:"id" jsonschema:"the bullet's id, from notes_search, notes_get or notes_due"`
	Due *string `json:"due" jsonschema:"the due date as YYYY-MM-DD, or an empty string to clear it"`
}

type setDoneToolArgs struct {
	ID   int64 `json:"id" jsonschema:"the bullet's id, from notes_search, notes_get or notes_due"`
	Done *bool `json:"done" jsonschema:"true to tick it done, false to un-tick it"`
}

func (a *App) addTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in addToolArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	text := strings.ReplaceAll(in.Text, "\r\n", "\n")
	title, note, _ := strings.Cut(text, "\n")
	title, note = strings.TrimSpace(title), strings.TrimSpace(note)
	// The UI lets Enter make an empty bullet; an assistant doing so is a mistake.
	if title == "" {
		return "", app.ToolError{Msg: "Give the note some text."}
	}

	// Create would accept an archived parent, and the new bullet would vanish
	// under it, so the parent has to be visible first.
	var crumbs []Node
	if in.ParentID != 0 {
		parent, up, err := a.visibleNode(ctx, userID, in.ParentID)
		if err != nil {
			return "", err
		}
		crumbs = append(up, parent)
	}
	// One transaction, so a bad due date leaves no dateless bullet behind.
	var n Node
	err := a.store.Do(ctx, func(o *Ops) error {
		var err error
		if n, err = o.Create(ctx, userID, in.ParentID, math.MaxInt32, title, note); err != nil {
			return err
		}
		if in.Due == "" {
			return nil
		}
		return o.SetDue(ctx, userID, n.ID, in.Due)
	})
	if err != nil {
		return "", toolErr(err)
	}
	return app.MarshalTool(struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		ParentID int64  `json:"parent_id,omitempty"`
		Path     string `json:"path,omitempty"`
		Due      string `json:"due,omitempty"`
	}{n.ID, n.DisplayTitle(), in.ParentID, toolPath(crumbs), in.Due})
}

func (a *App) setDueTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in setDueToolArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	if in.Due == nil {
		return "", app.ToolError{Msg: "Give a due date as YYYY-MM-DD, or an empty one to clear it."}
	}
	due := strings.TrimSpace(*in.Due)
	n, _, err := a.visibleNode(ctx, userID, in.ID)
	if err != nil {
		return "", err
	}
	if err := a.store.SetDue(ctx, userID, n.ID, due); err != nil {
		return "", toolErr(err)
	}
	return app.MarshalTool(struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
		Due   string `json:"due,omitempty"`
	}{n.ID, n.DisplayTitle(), due})
}

func (a *App) setDoneTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in setDoneToolArgs
	if err := app.ParseToolArgs(args, &in); err != nil {
		return "", err
	}
	if in.Done == nil {
		return "", app.ToolError{Msg: "Say whether the note is done, true or false."}
	}
	n, _, err := a.visibleNode(ctx, userID, in.ID)
	if err != nil {
		return "", err
	}
	if err := a.store.SetDone(ctx, userID, n.ID, *in.Done); err != nil {
		return "", toolErr(err)
	}
	return app.MarshalTool(struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
		Done  bool   `json:"done"`
	}{n.ID, n.DisplayTitle(), *in.Done})
}
