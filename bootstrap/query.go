package bootstrap

import "github.com/olivere/elastic/v7"

type CommonQuery struct {
	Id         uint32   `json:"id"`
	Ids        []uint32 `json:"ids"`
	ProjectId  uint32   `json:"project_id"`
	ProjectIds []uint32 `json:"project_ids"`
	BrandId    uint32   `json:"brand_id"`
	BrandIds   []uint32 `json:"brand_ids"`
	OutletId   uint32   `json:"outlet_id"`
	OutletIds  []uint32 `json:"outlet_ids"`
}

type RangeQuery struct {
	Min uint32 `json:"min"`
	Max uint32 `json:"max"`
}

type RangeQueries []RangeQuery

type ElasticQueries []elastic.Query

func (e *ElasticQueries) AppendQuery(key string, id uint32, ids []uint32) {
	if len(ids) > 0 {
		if id != 0 {
			ids = append(ids, id)
		}
		newIds := make([]interface{}, len(ids))
		for i, v := range ids {
			newIds[i] = v
		}
		*e = append(*e, elastic.NewTermsQuery(key, newIds...))
	} else if id != 0 {
		*e = append(*e, elastic.NewTermQuery(key, id))
	}
}

func (req CommonQuery) MapElasticCommonQuery() *elastic.BoolQuery {
	query := make(ElasticQueries, 0)
	query.AppendQuery("id", req.Id, req.Ids)
	query.AppendQuery("project_id", req.ProjectId, req.ProjectIds)
	query.AppendQuery("brand_id", req.BrandId, req.BrandIds)
	query.AppendQuery("outlet_id", req.OutletId, req.OutletIds)
	return elastic.NewBoolQuery().Must(query...)
}

func (qs RangeQueries) MapRangeQueries(key string) []elastic.Query {
	result := make([]elastic.Query, 0)
	for _, rangeQuery := range qs {
		query := rangeQuery.MapRangeQuery(key)
		result = append(result, &query)
	}
	return result
}

func (q *RangeQuery) MapRangeQuery(key string) elastic.RangeQuery {
	elasticRangeQuery := elastic.NewRangeQuery(key)
	if q.Min != 0 {
		elasticRangeQuery = elasticRangeQuery.Gte(q.Min)
	}
	if q.Max != 0 {
		elasticRangeQuery = elasticRangeQuery.Lte(q.Max)
	}
	return *elasticRangeQuery
}
