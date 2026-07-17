package main

import (
	"os"
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


// envString returns the value of an environment variable or a fallback default.
func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envBool returns the boolean value of an environment variable or a fallback default.
func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	return v == "true" || v == "1" || (v == "" && fallback)
}

// envBool returns the boolean value of an environment variable or a fallback default.
