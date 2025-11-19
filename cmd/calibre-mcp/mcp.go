package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/benoute/calibre-mcp/pkg/calibre"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type searchBooksInput struct {
	Query  string `json:"query"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

type searchBooksOutput struct {
	Results *calibre.SearchResult `json:"results"`
}

type getBookInput struct {
	ID int `json:"id"`
}

type getEPUBChaptersInput struct {
	BookID int `json:"book_id"`
}

type getEPUBChaptersOutput struct {
	Chapters *[]calibre.Chapter `json:"chapters"`
}

type getEPUBChapterContentInput struct {
	BookID       int `json:"book_id"`
	ChapterIndex int `json:"chapter_index"`
}

type searchEPUBContentInput struct {
	BookID int    `json:"book_id"`
	Query  string `json:"query"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

type getEPUBChapterContentOutput struct {
	Content string `json:"content"`
}

type searchEPUBContentOutput struct {
	Matches []calibre.SearchMatch `json:"matches"`
}

type booksSearchCacheEntry struct {
	results   *calibre.SearchResult
	timestamp time.Time
}

var booksSearchCache = make(map[string]booksSearchCacheEntry)

func searchBooks(ctx context.Context, req *mcp.CallToolRequest, input searchBooksInput, db *calibre.DB) (
	*mcp.CallToolResult,
	*searchBooksOutput,
	error,
) {
	key := input.Query
	var results *calibre.SearchResult
	if entry, ok := booksSearchCache[key]; ok && time.Since(entry.timestamp) < time.Minute {
		results = entry.results
	} else {
		var err error
		results, err = calibre.Search(ctx, db, input.Query)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: err.Error()},
				},
				IsError: true,
			}, nil, nil
		}
		booksSearchCache[key] = booksSearchCacheEntry{results: results, timestamp: time.Now()}
	}

	// Apply limit and offset
	books := results.Books
	if input.Offset > 0 {
		if input.Offset >= len(books) {
			books = []calibre.Book{}
		} else {
			books = books[input.Offset:]
		}
	}
	if input.Limit > 0 && len(books) > input.Limit {
		books = books[:input.Limit]
	}

	limitedResults := &calibre.SearchResult{
		Books:    books,
		TotalNum: results.TotalNum,
	}

	// Format the display text
	var contentLines []string
	contentLines = append(contentLines, fmt.Sprintf("Search results for '%s':", input.Query))
	contentLines = append(contentLines, "")
	for i, book := range limitedResults.Books {
		contentLines = append(
			contentLines,
			fmt.Sprintf("%d. %s by %s (ID: %d)", i+1, book.Title, strings.Join(book.Authors, ", "), book.ID),
		)
		if len(book.Tags) > 0 {
			contentLines = append(contentLines, fmt.Sprintf("   Tags: %s", strings.Join(book.Tags, ", ")))
		}
		if book.Series != "" {
			contentLines = append(contentLines, fmt.Sprintf("   Series: %s #%g", book.Series, book.SeriesIndex))
		}
		contentLines = append(contentLines, fmt.Sprintf("   Formats: %s", strings.Join(book.Formats, ", ")))
		contentLines = append(contentLines, "")
	}
	contentLines = append(contentLines, fmt.Sprintf("Total results: %d", limitedResults.TotalNum))

	searchBooksOutput := searchBooksOutput{
		Results: limitedResults,
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: strings.Join(contentLines, "\n")},
		},
	}, &searchBooksOutput, nil
}

func getBook(ctx context.Context, req *mcp.CallToolRequest, input getBookInput, db *calibre.DB) (
	*mcp.CallToolResult,
	*calibre.BookDetails,
	error,
) {
	book, err := calibre.GetBook(ctx, db, input.ID)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: err.Error()},
			},
			IsError: true,
		}, nil, nil
	}

	// Format the display text
	var contentLines []string
	contentLines = append(contentLines, fmt.Sprintf("# %s", book.Title))
	contentLines = append(contentLines, "")
	contentLines = append(contentLines, fmt.Sprintf("**Authors:** %s", strings.Join(book.Authors, ", ")))
	if len(book.Tags) > 0 {
		contentLines = append(contentLines, fmt.Sprintf("**Tags:** %s", strings.Join(book.Tags, ", ")))
	}
	if book.Series != "" {
		contentLines = append(contentLines, fmt.Sprintf("**Series:** %s #%g", book.Series, book.SeriesIndex))
	}
	contentLines = append(contentLines, fmt.Sprintf("**Publisher:** %s", book.Publisher))
	contentLines = append(contentLines, fmt.Sprintf("**Publication Date:** %s", book.PubDate))
	if book.Isbn != "" {
		contentLines = append(contentLines, fmt.Sprintf("**ISBN:** %s", book.Isbn))
	}
	contentLines = append(contentLines, fmt.Sprintf("**Language:** %s", book.Language))
	contentLines = append(contentLines, fmt.Sprintf("**Size:** %d bytes", book.Size))
	if book.Rating > 0 {
		contentLines = append(contentLines, fmt.Sprintf("**Rating:** %d/5", book.Rating))
	}
	contentLines = append(contentLines, fmt.Sprintf("**Formats:** %s", strings.Join(book.Formats, ", ")))
	if book.Comments != "" {
		contentLines = append(contentLines, "")
		contentLines = append(contentLines, "**Comments:**")
		contentLines = append(contentLines, book.Comments)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: strings.Join(contentLines, "\n")},
		},
	}, book, nil
}

func getEPUBChapters(ctx context.Context, req *mcp.CallToolRequest, input getEPUBChaptersInput, db *calibre.DB, libraryPath string) (
	*mcp.CallToolResult,
	*getEPUBChaptersOutput,
	error,
) {
	chapters, err := calibre.GetEPUBChapters(db, libraryPath, input.BookID)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: err.Error()},
			},
			IsError: true,
		}, nil, nil
	}

	// Format the display text
	var contentLines []string
	contentLines = append(contentLines, fmt.Sprintf("Chapters for book ID %d:", input.BookID))
	contentLines = append(contentLines, "")
	for _, chapter := range chapters {
		contentLines = append(contentLines, fmt.Sprintf("%d. %s", chapter.Index, chapter.Title))
	}

	getEPUBChaptersOutput := getEPUBChaptersOutput{
		Chapters: &chapters,
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: strings.Join(contentLines, "\n")},
		},
	}, &getEPUBChaptersOutput, nil
}

func getEPUBChapterContent(ctx context.Context, req *mcp.CallToolRequest, input getEPUBChapterContentInput, db *calibre.DB, libraryPath string) (
	*mcp.CallToolResult,
	*getEPUBChapterContentOutput,
	error,
) {
	content, err := calibre.GetEPUBChapterContent(db, libraryPath, input.BookID, input.ChapterIndex)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: err.Error()},
			},
			IsError: true,
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: content},
		},
	}, &getEPUBChapterContentOutput{Content: content}, nil
}

func searchEPUBContent(ctx context.Context, req *mcp.CallToolRequest, input searchEPUBContentInput, db *calibre.DB, libraryPath string) (
	*mcp.CallToolResult,
	*searchEPUBContentOutput,
	error,
) {
	matches, err := calibre.SearchEPUBContent(db, libraryPath, input.BookID, input.Query, input.Limit, input.Offset)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: err.Error()},
			},
			IsError: true,
		}, nil, nil
	}

	// Format the display text
	var contentLines []string
	contentLines = append(contentLines, fmt.Sprintf("Search results for '%s' in book ID %d:", input.Query, input.BookID))
	contentLines = append(contentLines, "")
	if len(matches) == 0 {
		contentLines = append(contentLines, "No matches found.")
	} else {
		for _, match := range matches {
			contentLines = append(contentLines, fmt.Sprintf("Chapter %d: %s", match.ChapterIndex, match.ChapterTitle))
			contentLines = append(contentLines, fmt.Sprintf("  %s", match.Snippet))
			contentLines = append(contentLines, "")
		}
	}

	searchEPUBContentOutput := searchEPUBContentOutput{
		Matches: matches,
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: strings.Join(contentLines, "\n")},
		},
	}, &searchEPUBContentOutput, nil
}

// setupMCPServer creates and configures the MCP server with Calibre tools
func setupMCPServer(libraryPath string) *mcp.Server {
	db, err := calibre.OpenLibrary(libraryPath)
	if err != nil {
		panic(fmt.Sprintf("Failed to open Calibre library: %v", err))
	}

	// Create a server with search and book retrieval tools
	server := mcp.NewServer(&mcp.Implementation{Name: "calibre-mcp", Version: "v1.0.0"}, nil)

	// Add search tool
	mcp.AddTool(server, &mcp.Tool{
		Name: "search_books",
		Description: "Search for books in the Calibre library by title, author, tags, or other metadata. " +
			"Returns a list of matching books with basic information. Supports limit and offset for fast pagination through results.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input searchBooksInput) (
		*mcp.CallToolResult, *searchBooksOutput, error,
	) {
		return searchBooks(ctx, req, input, db)
	})

	// Add get book tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_book",
		Description: "Retrieve detailed information about a specific book by its ID from the Calibre library",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input getBookInput) (
		*mcp.CallToolResult, *calibre.BookDetails, error,
	) {
		return getBook(ctx, req, input, db)
	})

	// Add get EPUB chapters tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_epub_chapters",
		Description: "Get the list of chapters in an EPUB book from the Calibre library by its ID",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input getEPUBChaptersInput) (
		*mcp.CallToolResult, *getEPUBChaptersOutput, error,
	) {
		return getEPUBChapters(ctx, req, input, db, libraryPath)
	})

	// Add get EPUB chapter content tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_epub_chapter_content",
		Description: "Get the text content of a specific chapter in an EPUB book from the Calibre library",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input getEPUBChapterContentInput) (
		*mcp.CallToolResult, *getEPUBChapterContentOutput, error,
	) {
		return getEPUBChapterContent(ctx, req, input, db, libraryPath)
	})

	// Add search EPUB content tool
	mcp.AddTool(server, &mcp.Tool{
		Name: "search_epub_content",
		Description: "Search for text within the content of an EPUB book from the Calibre " +
			"library and return matching paragraphs with chapter information. Supports limit and offset for fast pagination - use offset to walk through results.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input searchEPUBContentInput) (
		*mcp.CallToolResult, *searchEPUBContentOutput, error,
	) {
		return searchEPUBContent(ctx, req, input, db, libraryPath)
	})

	return server
}
