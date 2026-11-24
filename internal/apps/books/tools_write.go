package books

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

type idToolArgs struct {
	ID int64 `json:"id" jsonschema:"the book's id, from books_list"`
}

type progressToolArgs struct {
	ID      int64 `json:"id" jsonschema:"the book's id, from books_list"`
	Page    int   `json:"page,omitempty" jsonschema:"the page the user is on; give this or percent"`
	Percent int   `json:"percent,omitempty" jsonschema:"how far through the book the user is, 1 to 100; give this or page"`
}

type finishToolArgs struct {
	ID     int64  `json:"id" jsonschema:"the book's id, from books_list"`
	Rating int    `json:"rating,omitempty" jsonschema:"1 to 5 stars; leave out to keep the current rating"`
	Day    string `json:"day,omitempty" jsonschema:"the day it was finished, YYYY-MM-DD (default today)"`
}

type dnfToolArgs struct {
	ID  int64  `json:"id" jsonschema:"the book's id, from books_list"`
	Day string `json:"day,omitempty" jsonschema:"the day the user stopped, YYYY-MM-DD (default today)"`
}

type noteToolArgs struct {
	ID   int64  `json:"id" jsonschema:"the book's id, from books_list"`
	Text string `json:"text" jsonschema:"the note, in the user's own words"`
	Page int    `json:"page,omitempty" jsonschema:"the page the note is about"`
}

type quoteToolArgs struct {
	ID   int64  `json:"id" jsonschema:"the book's id, from books_list"`
	Text string `json:"text" jsonschema:"the passage, word for word"`
	Page int    `json:"page,omitempty" jsonschema:"the page the passage is on"`
}

// addedResult is what a note or quote tool returns: where the new row went.
type addedResult struct {
	ID     int64 `json:"id"`
	BookID int64 `json:"book_id"`
	Page   int   `json:"page,omitempty"`
}

// toolErr turns a store error into what the model sees: a missing book and
// another user's give the same answer, so an ID reveals nothing (spec,
// "Tool rules"), and a Refusal is already a sentence for the user. Anything
// else stays a plain error: the platform logs it.
func toolErr(err error) error {
	var ref *Refusal
	switch {
	case errors.Is(err, ErrNotFound):
		return app.ToolError{Msg: "No book with that ID on your shelves"}
	case errors.As(err, &ref):
		return app.ToolError{Msg: ref.Msg}
	}
	return err
}

// bookResult is a write tool's result: the book as it is now, so the model
// can confirm the new state.
func (a *App) bookResult(ctx context.Context, userID, id int64) (string, error) {
	b, err := a.store.Get(ctx, userID, id)
	if err != nil {
		return "", toolErr(err)
	}
	return marshalTool(bookToolView(b))
}

func (a *App) startReadingTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in idToolArgs
	if err := parseToolArgs(args, &in); err != nil {
		return "", err
	}
	if err := a.store.StartReading(ctx, userID, in.ID); err != nil {
		return "", toolErr(err)
	}
	return a.bookResult(ctx, userID, in.ID)
}

func (a *App) progressTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in progressToolArgs
	if err := parseToolArgs(args, &in); err != nil {
		return "", err
	}
	if (in.Page == 0) == (in.Percent == 0) {
		// Zero can't be recorded here: nobody says "page 0", and it keeps
		// the both-or-neither check simple.
		return "", app.ToolError{Msg: "Give either page or percent."}
	}
	b, err := a.store.Get(ctx, userID, in.ID)
	if err != nil {
		return "", toolErr(err)
	}
	unit := UnitFor(b.Latest.Format, b.Pages)
	value := in.Page
	switch {
	case in.Percent != 0 && unit == UnitPercent:
		value = in.Percent
	case in.Percent != 0:
		// Range-check in the unit the user spoke before converting, so the
		// message is never in a unit they didn't use.
		if err := checkProgress(UnitPercent, b.Pages, in.Percent); err != nil {
			return "", toolErr(err)
		}
		value = (in.Percent*b.Pages + 50) / 100
	case unit == UnitPercent:
		if b.Pages <= 0 {
			return "", app.ToolError{Msg: "This book has no page count, so give a percentage."}
		}
		if err := checkProgress(UnitPage, b.Pages, in.Page); err != nil {
			return "", toolErr(err)
		}
		value = (in.Page*100 + b.Pages/2) / b.Pages
	}
	if err := a.store.RecordProgress(ctx, userID, in.ID, value); err != nil {
		return "", toolErr(err)
	}
	return a.bookResult(ctx, userID, in.ID)
}

func (a *App) finishTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in finishToolArgs
	if err := parseToolArgs(args, &in); err != nil {
		return "", err
	}
	// The store's checkRating returns a bare ErrInvalid, which the model
	// would see as "Something went wrong.".
	if in.Rating < 0 || in.Rating > 5 {
		return "", app.ToolError{Msg: "Give a rating from 1 to 5."}
	}
	if err := a.store.FinishReading(ctx, userID, in.ID, strings.TrimSpace(in.Day), in.Rating); err != nil {
		return "", toolErr(err)
	}
	return a.bookResult(ctx, userID, in.ID)
}

func (a *App) dnfTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in dnfToolArgs
	if err := parseToolArgs(args, &in); err != nil {
		return "", err
	}
	if err := a.store.MarkDNF(ctx, userID, in.ID, strings.TrimSpace(in.Day), 0); err != nil {
		return "", toolErr(err)
	}
	return a.bookResult(ctx, userID, in.ID)
}

func (a *App) addNoteTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in noteToolArgs
	if err := parseToolArgs(args, &in); err != nil {
		return "", err
	}
	id, err := a.store.AddNote(ctx, userID, in.ID, NoteInput{Page: in.Page, Body: in.Text})
	if err != nil {
		return "", toolErr(err)
	}
	return marshalTool(addedResult{ID: id, BookID: in.ID, Page: in.Page})
}

func (a *App) addQuoteTool(ctx context.Context, userID int64, args json.RawMessage) (string, error) {
	var in quoteToolArgs
	if err := parseToolArgs(args, &in); err != nil {
		return "", err
	}
	id, err := a.store.AddQuote(ctx, userID, in.ID, QuoteInput{Page: in.Page, Text: in.Text})
	if err != nil {
		return "", toolErr(err)
	}
	return marshalTool(addedResult{ID: id, BookID: in.ID, Page: in.Page})
}
