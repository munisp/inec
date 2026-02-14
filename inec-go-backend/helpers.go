package main

import "database/sql"

func querySingleRow(query string, args ...interface{}) (M, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	all := scanRows(rows)
	if len(all) == 0 {
		return nil, sql.ErrNoRows
	}
	return all[0], nil
}
