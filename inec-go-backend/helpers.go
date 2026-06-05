package main

import (
	"context"
	"database/sql"
)

func querySingleRow(query string, args ...interface{}) (M, error) {
	return querySingleRowCtx(context.Background(), query, args...)
}

func querySingleRowCtx(ctx context.Context, query string, args ...interface{}) (M, error) {
	rows, err := dbQueryCtx(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	all := scanRows(rows)
	if len(all) == 0 {
		return nil, sql.ErrNoRows
	}
	return all[0], nil
}
