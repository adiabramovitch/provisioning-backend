package pagination

type Pagination struct {
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
	Token  string `json:"token"`
}

// NewPagination creates a new Pagination with default values of 0 for offset and 100 for limit,
// unless valid limit and offset values are provided as parameters.
func NewPagination(limit, offset int) *Pagination {
	pagination := &Pagination{
		Offset: 0,
		Limit:  100,
	}

	if limit <= 100 && limit > 0 {
		pagination.Limit = limit
	}

	if offset >= 0 {
		pagination.Offset = offset
	}

	return pagination
}
