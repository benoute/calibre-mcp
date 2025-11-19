package calibre

import (
	"context"
	"database/sql"
	"fmt"
)

type BookDetails struct {
	Book
	// Additional fields from database
	Identifiers    map[string]string   `json:"identifiers"`
	UserCategories map[string][]string `json:"user_categories"`
	CustomColumns  map[string]any      `json:"custom_columns"`
}

func GetBook(ctx context.Context, db *DB, id int) (*BookDetails, error) {
	// Get basic book info
	var book BookDetails
	var series sql.NullString
	var publisher sql.NullString
	var language sql.NullString
	var rating sql.NullInt32
	var comments sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT b.id, b.title, s.name, b.series_index, p.name, b.pubdate,
		       b.isbn, l.lang_code, r.rating, c.text, b.timestamp, b.last_modified
		FROM books b
		LEFT JOIN books_series_link bsl ON b.id = bsl.book
		LEFT JOIN series s ON bsl.series = s.id
		LEFT JOIN books_publishers_link bpl ON b.id = bpl.book
		LEFT JOIN publishers p ON bpl.publisher = p.id
		LEFT JOIN books_languages_link bll ON b.id = bll.book
		LEFT JOIN languages l ON bll.lang_code = l.id
		LEFT JOIN books_ratings_link brl ON b.id = brl.book
		LEFT JOIN ratings r ON brl.rating = r.id
		LEFT JOIN comments c ON b.id = c.book
		WHERE b.id = ?
	`, id).Scan(&book.ID, &book.Title, &series, &book.SeriesIndex, &publisher,
		&book.PubDate, &book.Isbn, &language, &rating,
		&comments, &book.Timestamp, &book.LastModified)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("book not found")
		}
		return nil, err
	}
	book.Series = series.String
	if !series.Valid {
		book.Series = ""
	}
	book.Publisher = publisher.String
	if !publisher.Valid {
		book.Publisher = ""
	}
	book.Language = language.String
	if !language.Valid {
		book.Language = ""
	}
	book.Rating = int(rating.Int32)
	if !rating.Valid {
		book.Rating = 0
	}
	book.Comments = comments.String
	if !comments.Valid {
		book.Comments = ""
	}
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("book not found")
		}
		return nil, err
	}

	// Get size (sum of uncompressed_size from data table)
	err = db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(uncompressed_size), 0)
		FROM data
		WHERE book = ?
	`, id).Scan(&book.Size)
	if err != nil {
		return nil, err
	}

	// Get authors
	book.Authors, err = getAuthorsForBook(db, id)
	if err != nil {
		return nil, err
	}

	// Get tags
	book.Tags, err = getTagsForBook(db, id)
	if err != nil {
		return nil, err
	}

	// Get formats
	book.Formats, err = getFormatsForBook(db, id)
	if err != nil {
		return nil, err
	}

	// Get identifiers (ISBN, etc.)
	book.Identifiers, err = getIdentifiersForBook(db, id)
	if err != nil {
		return nil, err
	}

	// For now, leave UserCategories and CustomColumns empty
	// They would require more complex queries into custom columns tables
	book.UserCategories = make(map[string][]string)
	book.CustomColumns = make(map[string]any)

	return &book, nil
}

func getIdentifiersForBook(db *DB, bookID int) (map[string]string, error) {
	rows, err := db.Query(`
		SELECT type, val
		FROM identifiers
		WHERE book = ?
	`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	identifiers := make(map[string]string)
	for rows.Next() {
		var typ, val string
		if err := rows.Scan(&typ, &val); err != nil {
			return nil, err
		}
		identifiers[typ] = val
	}
	return identifiers, nil
}
