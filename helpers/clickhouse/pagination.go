// Package ClickHouseORM pagination for ClickHouse queries.
// Cloned from helpers.Paging with adaptations for ClickHouse (Query, Count, Find, context).
package ClickHouseORM

import (
	"context"
	"math"
	"regexp"
	"strings"

	"github.com/hexlinevault/core-api.git/helpers"
)

// PagingOrderByCH defines sort column and direction for ClickHouse paging.
type PagingOrderByCH struct {
	SortBy   string
	SortType string
}

// PagingConfigCH is the paging config for ClickHouse.
type PagingConfigCH struct {
	Query   *Query
	Page    int
	PerPage int
	OrderBy []*PagingOrderByCH
	ShowSQL bool
	All     bool
	Ctx     context.Context
}

// PaginatorCH is the paging response (same shape as helpers.Paginator for API consistency).
type PaginatorCH struct {
	TotalRecord int         `json:"total_record"`
	TotalPage   int         `json:"total_page"`
	Records     interface{} `json:"data"`
	Offset      int         `json:"offset"`
	PerPage     int         `json:"per_page"`
	Page        int         `json:"page"`
	PrevPage    int         `json:"prev_page"`
	NextPage    int         `json:"next_page"`
}

// ToMap returns a minimal map for meta (same as helpers.Paginator).
func (h *PaginatorCH) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"page":        h.Page,
		"per_page":    h.PerPage,
		"page_count":  h.TotalPage,
		"total_count": h.TotalRecord,
	}
}

// ToHelpersPaginator converts to helpers.Paginator so callers can use one type for both GORM and CH.
func (h *PaginatorCH) ToHelpersPaginator() *helpers.Paginator {
	return &helpers.Paginator{
		TotalRecord: h.TotalRecord,
		TotalPage:   h.TotalPage,
		Records:     h.Records,
		Offset:      h.Offset,
		PerPage:     h.PerPage,
		Page:        h.Page,
		PrevPage:    h.PrevPage,
		NextPage:    h.NextPage,
	}
}

var safeColumnRe = regexp.MustCompile(`^[a-zA-Z0-9_.]+$`)

func validateSortTypeCH(sortType string) string {
	switch strings.ToUpper(sortType) {
	case "DESC":
		return "DESC"
	case "ASC":
		return "ASC"
	default:
		return "ASC"
	}
}

func safeOrderArg(sortBy string) string {
	sortBy = strings.TrimSpace(strings.ReplaceAll(sortBy, "`", ""))
	if safeColumnRe.MatchString(sortBy) {
		return sortBy
	}
	return "1"
}

// PagingCH runs the query with paging and returns PaginatorCH.
// result must be a pointer to a slice of structs (*[]T).
func PagingCH(p *PagingConfigCH, result interface{}) (*PaginatorCH, error) {
	if p.Query == nil {
		return nil, ErrNoRows
	}
	ctx := p.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	q := p.Query

	if p.Page < 1 {
		p.Page = 1
	}
	if p.PerPage == 0 {
		p.PerPage = 10
	}

	// Count (does not mutate q.ds)
	count, err := q.Count(ctx)
	if err != nil {
		return nil, err
	}

	offset := 0
	if p.Page > 1 {
		offset = (p.Page - 1) * p.PerPage
	}

	// Apply order
	if len(p.OrderBy) > 0 {
		orderStrs := make([]string, 0, len(p.OrderBy))
		for _, o := range p.OrderBy {
			col := safeOrderArg(o.SortBy)
			dir := validateSortTypeCH(o.SortType)
			orderStrs = append(orderStrs, col+" "+dir)
		}
		q.OrderBy(orderStrs...)
	}

	if p.All {
		if err := q.Find(ctx, result); err != nil {
			return nil, err
		}
		totalRec := int(count)
		return &PaginatorCH{
			TotalRecord: totalRec,
			Records:     result,
			Page:        1,
			Offset:      0,
			PerPage:     totalRec,
			TotalPage:   1,
			PrevPage:    1,
			NextPage:    1,
		}, nil
	}

	perPage := uint(p.PerPage)
	offsetU := uint(offset)
	q.Limit(perPage).Offset(offsetU)
	if err := q.Find(ctx, result); err != nil {
		return nil, err
	}

	totalPage := int(math.Ceil(float64(count) / float64(p.PerPage)))
	if totalPage == 0 {
		totalPage = 1
	}
	prevPage := p.Page
	if p.Page > 1 {
		prevPage = p.Page - 1
	}
	nextPage := p.Page
	if p.Page < totalPage {
		nextPage = p.Page + 1
	}

	return &PaginatorCH{
		TotalRecord: int(count),
		TotalPage:   totalPage,
		Records:     result,
		Offset:      offset,
		PerPage:     p.PerPage,
		Page:        p.Page,
		PrevPage:    prevPage,
		NextPage:    nextPage,
	}, nil
}

/*
GeneratePagingOrderCH builds sort options for ClickHouse paging.
Same semantics as helpers.GeneratePagingOrder:
  - args[0] = sortBy (e.g. from request), default "created_at"
  - args[1] = sortType (e.g. from request), default "desc"
  - args[2] = sortBy fallback default
  - args[3] = sortType fallback default
*/
func GeneratePagingOrderCH(args ...string) *PagingOrderByCH {
	sortByDefault := "created_at"
	sortTypeDefault := "desc"
	switch len(args) {
	case 4:
		sortTypeDefault = args[3]
		fallthrough
	case 3:
		sortByDefault = args[2]
		fallthrough
	case 2:
		if args[1] != "" {
			sortTypeDefault = args[1]
		}
		fallthrough
	case 1:
		if args[0] != "" {
			sortByDefault = args[0]
		}
	}
	return &PagingOrderByCH{
		SortBy:   sortByDefault,
		SortType: sortTypeDefault,
	}
}
