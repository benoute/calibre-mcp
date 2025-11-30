package calibre

import (
	"context"
	"database/sql"
	"strings"
)

type Book struct {
	ID           int      `json:"id"`
	Title        string   `json:"title"`
	Authors      []string `json:"authors"`
	Tags         []string `json:"tags"`
	Series       string   `json:"series"`
	SeriesIndex  float64  `json:"series_index"`
	Formats      []string `json:"formats"`
	Cover        string   `json:"cover"`
	Publisher    string   `json:"publisher"`
	PubDate      string   `json:"pubdate"`
	Isbn         string   `json:"isbn"`
	Language     string   `json:"language"`
	Size         int      `json:"size"`
	Rating       int      `json:"rating"`
	Comments     string   `json:"comments"`
	Timestamp    string   `json:"timestamp"`
	LastModified string   `json:"last_modified"`
}

type SearchResult struct {
	Books    []Book `json:"books"`
	TotalNum int    `json:"total_num"`
}

type SearchOption func(*SearchOptions)

type SearchOptions struct {
	Limit  int
	Offset int
}

func WithLimit(limit int) SearchOption {
	return func(opts *SearchOptions) {
		opts.Limit = limit
	}
}

func WithOffset(offset int) SearchOption {
	return func(opts *SearchOptions) {
		opts.Offset = offset
	}
}

func Search(ctx context.Context, db *DB, query string, opts ...SearchOption) (*SearchResult, error) {
	options := &SearchOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// Build the query
	// This is a simplified search - in reality Calibre has complex search syntax
	// For now, search in title, author names, tags, and comments
	searchQuery := "%" + strings.ToLower(query) + "%"

	sqlQuery := `
 		SELECT DISTINCT b.id, b.title, s.name, b.series_index, p.name, b.pubdate,
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
 		LEFT JOIN books_authors_link bal ON b.id = bal.book
 		LEFT JOIN authors a ON bal.author = a.id
 		LEFT JOIN books_tags_link btl ON b.id = btl.book
 		LEFT JOIN tags t ON btl.tag = t.id
 		WHERE LOWER(b.title) LIKE ? OR LOWER(a.name) LIKE ? OR LOWER(t.name) LIKE ? OR LOWER(c.text) LIKE ?
 	`

	args := []any{searchQuery, searchQuery, searchQuery, searchQuery}

	if options.Limit > 0 {
		sqlQuery += " LIMIT ?"
		args = append(args, options.Limit)
	}
	if options.Offset > 0 {
		sqlQuery += " OFFSET ?"
		args = append(args, options.Offset)
	}

	rows, err := db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	books := make([]Book, 0, options.Limit)
	for rows.Next() {
		var book Book
		var series sql.NullString
		var publisher sql.NullString
		var language sql.NullString
		var rating sql.NullInt32
		var comments sql.NullString
		err := rows.Scan(&book.ID, &book.Title, &series, &book.SeriesIndex, &publisher,
			&book.PubDate, &book.Isbn, &language, &rating,
			&comments, &book.Timestamp, &book.LastModified)
		if err != nil {
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

		// Get authors
		book.Authors, err = getAuthorsForBook(db, book.ID)
		if err != nil {
			return nil, err
		}

		// Get tags
		book.Tags, err = getTagsForBook(db, book.ID)
		if err != nil {
			return nil, err
		}

		// Get formats
		book.Formats, err = getFormatsForBook(db, book.ID)
		if err != nil {
			return nil, err
		}

		books = append(books, book)
	}

	if books == nil {
		books = []Book{}
	}

	// Get total count (simplified, without DISTINCT)
	totalQuery := `
 		SELECT COUNT(DISTINCT b.id)
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
 		LEFT JOIN books_authors_link bal ON b.id = bal.book
 		LEFT JOIN authors a ON bal.author = a.id
 		LEFT JOIN books_tags_link btl ON b.id = btl.book
 		LEFT JOIN tags t ON btl.tag = t.id
 		WHERE LOWER(b.title) LIKE ? OR LOWER(a.name) LIKE ? OR LOWER(t.name) LIKE ? OR LOWER(c.text) LIKE ?
 	`
	var totalNum int
	err = db.QueryRowContext(ctx, totalQuery, searchQuery, searchQuery, searchQuery, searchQuery).Scan(&totalNum)
	if err != nil {
		return nil, err
	}

	return &SearchResult{Books: books, TotalNum: totalNum}, nil
}

func getAuthorsForBook(db *DB, bookID int) ([]string, error) {
	rows, err := db.Query(`
		SELECT a.name
		FROM authors a
		JOIN books_authors_link bal ON a.id = bal.author
		WHERE bal.book = ?
		ORDER BY bal.id
	`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var authors []string
	for rows.Next() {
		var author string
		if err := rows.Scan(&author); err != nil {
			return nil, err
		}
		authors = append(authors, author)
	}
	if authors == nil {
		authors = []string{}
	}
	return authors, nil
}

func getTagsForBook(db *DB, bookID int) ([]string, error) {
	rows, err := db.Query(`
		SELECT t.name
		FROM tags t
		JOIN books_tags_link btl ON t.id = btl.tag
		WHERE btl.book = ?
		ORDER BY t.name
	`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}

func getFormatsForBook(db *DB, bookID int) ([]string, error) {
	rows, err := db.Query(`
		SELECT format
		FROM data
		WHERE book = ?
	`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var formats []string
	for rows.Next() {
		var format string
		if err := rows.Scan(&format); err != nil {
			return nil, err
		}
		formats = append(formats, format)
	}
	if formats == nil {
		formats = []string{}
	}
	return formats, nil
}
