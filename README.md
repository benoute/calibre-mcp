# Calibre MCP Server

An MCP (Model Context Protocol) server that provides access to a local Calibre library.

## Features

- Search books by title, author, tags, or other metadata
- Retrieve detailed book information
- Access EPUB book chapters and content
- Search within EPUB book text content
- Supports both stdio and HTTP streamable transports

## Usage

### Build

```bash
go build ./cmd/calibre-mcp
```

### Run

#### Stdio mode (for local MCP clients)

```bash
./calibre-mcp -transport=stdio -library-path=/path/to/calibre/library
```

#### HTTP mode (for remote access)

```bash
./calibre-mcp -transport=http -port=8080 -library-path=/path/to/calibre/library
```

## Tools

### search_books

Search for books in the Calibre library by title, author, tags, or other metadata. Returns a list of matching books with basic information. Supports limit and offset for fast pagination through results.

Parameters:
- `query`: Search query string
- `limit`: Maximum number of results (optional)
- `offset`: Offset for pagination (optional)

### get_book

Retrieve detailed information about a specific book by its ID from the Calibre library.

Parameters:
- `id`: Book ID

### get_epub_chapters

Get the list of chapters in an EPUB book from the Calibre library by its ID.

Parameters:
- `book_id`: Book ID

### get_epub_chapter_content

Get the text content of a specific chapter in an EPUB book from the Calibre library.

Parameters:
- `book_id`: Book ID
- `chapter_index`: Chapter index (starting from 0)

### search_epub_content

Search for text within the content of an EPUB book from the Calibre library and return matching paragraphs with chapter information. Supports limit and offset for fast pagination - use offset to walk through results.

Parameters:
- `book_id`: Book ID
- `query`: Search query string
- `limit`: Maximum number of results (optional)
- `offset`: Offset for pagination (optional)

## Requirements

- Go 1.25+
- Access to a local Calibre library directory containing metadata.db