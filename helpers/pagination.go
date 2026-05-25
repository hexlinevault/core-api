package helpers

import (
	"math"
	"strings"

	"gorm.io/gorm/clause"

	"gorm.io/gorm"
)

type (
	// FetchQuery fetch query options
	FetchQuery struct {
		Type  string
		Query string
		Args  []interface{}
	}

	// PagingConfig paging config
	PagingConfig struct {
		DB         *gorm.DB
		Query      *gorm.DB
		FetchQuery []*FetchQuery
		Page       int
		PerPage    int
		OrderBy    []*PagingOrderBy
		ShowSQL    bool
		All        bool
		UseScan    bool
	}

	PagingOrderBy struct {
		SortBy   string
		SortType string
	}

	// Paginator paging response
	Paginator struct {
		TotalRecord int         `json:"total_record"`
		TotalPage   int         `json:"total_page"`
		Records     interface{} `json:"data"`
		Offset      int         `json:"offset"`
		PerPage     int         `json:"PerPage"`
		Page        int         `json:"page"`
		PrevPage    int         `json:"prev_page"`
		NextPage    int         `json:"next_page"`
	}
)

func (h *Paginator) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"page":        h.Page,
		"per_page":    h.PerPage,
		"page_count":  h.TotalPage,
		"total_count": h.TotalRecord,
	}
}

// Paging query data with paging
func Paging(p *PagingConfig, result interface{}) (*Paginator, error) {
	qr := p.Query
	if p.ShowSQL {
		qr = qr.Debug()
	}
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PerPage == 0 {
		p.PerPage = 10
	}

	var paginator Paginator
	var count int64
	var offset int

	if p.UseScan {
		if err := p.DB.Raw("select count(*) from (?) x", p.Query).Count(&count).Error; err != nil {
			return nil, err
		}
	} else {
		model := qr.Statement.Model
		if model == nil {
			model = result
		}
		if err := qr.Model(model).Count(&count).Error; err != nil {
			return nil, err
		}
	}

	if p.Page == 1 {
		offset = 0
	} else {
		offset = (p.Page - 1) * p.PerPage
	}

	if len(p.FetchQuery) > 0 {
		for _, v := range p.FetchQuery {
			switch queryType := v.Type; queryType {
			case "select":
				qr = qr.Select(v.Query, v.Args...)
			case "join":
				qr = qr.Joins(v.Query, v.Args...)
			case "order":
				qr = qr.Order(v.Query)
			default:
			}
		}
	}

	if len(p.OrderBy) > 0 {
		for _, o := range p.OrderBy {
			qr = qr.Order(
				clause.OrderByColumn{
					Column: clause.Column{
						Name: strings.ReplaceAll(o.SortBy, "`", ""),
					},
					Desc: validateSortType(o.SortType),
				},
			)
		}
	}

	if p.All {
		if err := qr.Scan(result).Error; err != nil {
			return nil, err
		}
		totalRec := int(count)
		paginator := &Paginator{
			TotalRecord: totalRec,
			Records:     result,
			Page:        1,
			Offset:      0,
			PerPage:     totalRec,
			TotalPage:   1,
		}
		return paginator, nil
	}
	if p.UseScan {
		fetchQR := p.DB.Raw("? LIMIT ?,?", p.Query, offset, p.PerPage)
		if err := fetchQR.Scan(result).Error; err != nil {
			return nil, err
		}
	} else {
		fetchQR := qr.Limit(p.PerPage).Offset(offset)
		if err := fetchQR.Find(result).Error; err != nil {
			return nil, err
		}
	}

	paginator.TotalRecord = int(count)
	paginator.Records = result
	paginator.Page = p.Page
	paginator.Offset = offset
	paginator.PerPage = p.PerPage
	paginator.TotalPage = int(math.Ceil(float64(count) / float64(p.PerPage)))

	if p.Page > 1 {
		paginator.PrevPage = p.Page - 1
	} else {
		paginator.PrevPage = p.Page
	}

	if p.Page == paginator.TotalPage {
		paginator.NextPage = p.Page
	} else {
		paginator.NextPage = p.Page + 1
	}
	return &paginator, nil
}

func validateSortType(sortType string) bool {
	switch strings.ToLower(sortType) {
	case "desc":
		return true
	default:
		return false
	}
}

/*
	 GeneratePagingOrder(
		req.SortBy string, default "created_at"
		req.SortType string, default "desc"
		sortByDefault string, optional
		sortTypeDefault string, optional

)
*/
func GeneratePagingOrder(args ...string) *PagingOrderBy {
	var sortByDefault = "created_at"
	var sortTypeDefault = "desc"
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
	return &PagingOrderBy{
		SortBy:   sortByDefault,
		SortType: sortTypeDefault,
	}
}
