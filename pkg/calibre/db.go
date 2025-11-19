package calibre

import (
	"database/sql"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
}

func OpenLibrary(path string) (*DB, error) {
	dbPath := filepath.Join(path, "metadata.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	return &DB{db}, nil
}
