package main

import (
	"context"
	"database/sql"
)

func querySingleRow(query string, args ...interface{}) (M, error) {
	rows, err := dbQueryCtx(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	all := scanRows(rows)
	if len(all) == 0 {
		return nil, sql.ErrNoRows
	}
	return all[0], nil
}
