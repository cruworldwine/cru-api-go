package core

import (
	"context"
	"encoding/json"
	"iter"
)

// Page is one page of a list that pages with limit and offset.
//
// Items holds this page's items. Next fetches the page after it, and All walks every item of
// this page and the ones after it, fetching them as it goes.
type Page[T any] struct {
	// Items are this page's items.
	Items []T
	// Limit is meta.pagination.limit, the most items a page holds.
	Limit int
	// Offset is meta.pagination.offset, how many items came before this page.
	Offset int
	// Total is meta.pagination.total, or nil when the list does not count its items.
	Total *int
	// HasMore is meta.pagination.has_more. Without it, a full page is taken to have more.
	HasMore bool
	// Data is the envelope's data as it was received, with any keys besides the items.
	Data json.RawMessage
	// Meta is the envelope's meta.
	Meta Meta

	// next fetches the page at offset: a *Page[T], set by the call that fetched this one.
	next func(ctx context.Context, offset int) (any, error)
}

// Next fetches the page after this one, at offset Offset+len(Items). It returns nil and no
// error when there is none.
func (p *Page[T]) Next(ctx context.Context) (*Page[T], error) {
	if p == nil || !p.HasMore || len(p.Items) == 0 || p.next == nil {
		return nil, nil
	}
	next, err := p.next(ctx, p.Offset+len(p.Items))
	if err != nil {
		return nil, err
	}
	page, _ := next.(*Page[T])
	return page, nil
}

func (p *Page[T]) setNext(next func(ctx context.Context, offset int) (any, error)) { p.next = next }

// All returns an iterator over every item of this page and of the pages after it. A failed
// fetch is yielded as the zero item with the error, and ends the iteration.
func (p *Page[T]) All(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for page := p; page != nil; {
			for _, item := range page.Items {
				if !yield(item, nil) {
					return
				}
			}
			next, err := page.Next(ctx)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			page = next
		}
	}
}
