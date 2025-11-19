package calibre

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

type Chapter struct {
	Index int    `json:"index"`
	Title string `json:"title"`
	Href  string `json:"href"`
}

type SearchMatch struct {
	ChapterIndex int    `json:"chapter_index"`
	ChapterTitle string `json:"chapter_title"`
	Snippet      string `json:"snippet"`
}

type searchCacheEntry struct {
	matches   []SearchMatch
	timestamp time.Time
}

var searchCache = make(map[string]searchCacheEntry)

type Container struct {
	XMLName   xml.Name   `xml:"container"`
	Rootfiles []Rootfile `xml:"rootfiles>rootfile"`
}

type Rootfile struct {
	Path string `xml:"full-path,attr"`
}

type Package struct {
	XMLName  xml.Name `xml:"package"`
	Manifest Manifest `xml:"manifest"`
	Spine    Spine    `xml:"spine"`
}

type Spine struct {
	Itemrefs []Itemref `xml:"itemref"`
}

type Itemref struct {
	Idref string `xml:"idref,attr"`
}

type Manifest struct {
	Items []Item `xml:"item"`
}

type Item struct {
	Id   string `xml:"id,attr"`
	Href string `xml:"href,attr"`
}

func GetEPUBChapters(db *DB, libraryPath string, bookID int) ([]Chapter, error) {
	epubPath, err := getEPUBPath(db, libraryPath, bookID)
	if err != nil {
		return nil, err
	}

	r, err := zip.OpenReader(epubPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open EPUB: %w", err)
	}
	defer r.Close()

	// Read container.xml
	containerFile, err := r.Open("META-INF/container.xml")
	if err != nil {
		return nil, fmt.Errorf("failed to open container.xml: %w", err)
	}
	defer containerFile.Close()

	var container Container
	if err := xml.NewDecoder(containerFile).Decode(&container); err != nil {
		return nil, fmt.Errorf("failed to parse container.xml: %w", err)
	}

	if len(container.Rootfiles) == 0 {
		return nil, fmt.Errorf("no rootfile found")
	}

	opfPath := container.Rootfiles[0].Path

	// Read content.opf
	opfFile, err := r.Open(opfPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open OPF: %w", err)
	}
	defer opfFile.Close()

	data, err := io.ReadAll(opfFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read OPF: %w", err)
	}

	var pkg Package
	if err := xml.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("failed to parse OPF: %w", err)
	}

	// Build href map
	hrefMap := make(map[string]string)
	for _, item := range pkg.Manifest.Items {
		hrefMap[item.Id] = item.Href
	}

	// Get chapters from spine
	chapters := make([]Chapter, 0)
	for i, itemref := range pkg.Spine.Itemrefs {
		href, ok := hrefMap[itemref.Idref]
		if !ok {
			continue
		}
		title := fmt.Sprintf("Chapter %d", i+1)
		// Try to extract title from the chapter file
		chapterFile, err := r.Open(href)
		if err == nil {
			data, err := io.ReadAll(chapterFile)
			chapterFile.Close()
			if err == nil {
				extractedTitle := extractTitleFromHTML(string(data))
				if extractedTitle != "" {
					title = extractedTitle
				}
			}
		}
		chapters = append(chapters, Chapter{
			Index: i,
			Title: title,
			Href:  href,
		})
	}

	return chapters, nil
}

func GetEPUBChapterContent(db *DB, libraryPath string, bookID int, chapterIndex int) (string, error) {
	epubPath, err := getEPUBPath(db, libraryPath, bookID)
	if err != nil {
		return "", err
	}

	r, err := zip.OpenReader(epubPath)
	if err != nil {
		return "", fmt.Errorf("failed to open EPUB: %w", err)
	}
	defer r.Close()

	chapters, err := GetEPUBChapters(db, libraryPath, bookID)
	if err != nil {
		return "", err
	}

	if chapterIndex < 0 || chapterIndex >= len(chapters) {
		return "", fmt.Errorf("chapter index out of range")
	}

	chapter := chapters[chapterIndex]

	// Open the chapter file
	chapterFile, err := r.Open(chapter.Href)
	if err != nil {
		return "", fmt.Errorf("failed to open chapter: %w", err)
	}
	defer chapterFile.Close()

	data, err := io.ReadAll(chapterFile)
	if err != nil {
		return "", fmt.Errorf("failed to read chapter: %w", err)
	}

	// Extract text from XHTML
	content := extractTextFromHTML(string(data))

	return content, nil
}

func SearchEPUBContent(db *DB, libraryPath string, bookID int, query string, limit int, offset int) ([]SearchMatch, error) {
	key := fmt.Sprintf("%d:%s", bookID, query)
	var matches []SearchMatch

	if entry, ok := searchCache[key]; ok && time.Since(entry.timestamp) < time.Minute {
		matches = entry.matches
	} else {
		chapters, err := GetEPUBChapters(db, libraryPath, bookID)
		if err != nil {
			return nil, err
		}

		matches = make([]SearchMatch, 0)
		queryLower := strings.ToLower(query)

		for _, chapter := range chapters {
			content, err := GetEPUBChapterContent(db, libraryPath, bookID, chapter.Index)
			if err != nil {
				continue // skip chapters that can't be read
			}

			paragraphs := strings.Split(content, "\n")
			for _, para := range paragraphs {
				if para == "" {
					continue
				}
				paraLower := strings.ToLower(para)
				if strings.Contains(paraLower, queryLower) {
					// Find the position of the query in the paragraph
					pos := strings.Index(paraLower, queryLower)
					if pos != -1 {
						// Highlight the match in the paragraph
						snippet := para[:pos] + "**" + para[pos:pos+len(query)] + "**" + para[pos+len(query):]
						matches = append(matches, SearchMatch{
							ChapterIndex: chapter.Index,
							ChapterTitle: chapter.Title,
							Snippet:      snippet,
						})
					}
				}
			}
		}

		searchCache[key] = searchCacheEntry{matches: matches, timestamp: time.Now()}
	}

	if offset > 0 {
		if offset >= len(matches) {
			return []SearchMatch{}, nil
		}
		matches = matches[offset:]
	}
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}

	return matches, nil
}

func getEPUBPath(db *DB, libraryPath string, bookID int) (string, error) {
	var path, filename string
	err := db.QueryRow(`
		SELECT b.path, d.name
		FROM books b
		JOIN data d ON b.id = d.book
		WHERE b.id = ? AND d.format = 'EPUB'
		LIMIT 1
	`, bookID).Scan(&path, &filename)
	if err != nil {
		return "", fmt.Errorf("EPUB not found for book %d: %w", bookID, err)
	}

	return filepath.Join(libraryPath, path, filename+".epub"), nil
}

func extractTitleFromHTML(html string) string {
	// Find <title> tag
	start := strings.Index(html, "<title>")
	if start == -1 {
		// Try <title with attributes
		start = strings.Index(html, "<title ")
		if start == -1 {
			return ""
		}
		// Find the closing >
		endTag := strings.Index(html[start:], ">")
		if endTag == -1 {
			return ""
		}
		start += endTag + 1
	} else {
		start += 7
	}

	end := strings.Index(html[start:], "</title>")
	if end == -1 {
		return ""
	}

	title := html[start : start+end]
	// Decode basic entities
	title = strings.ReplaceAll(title, "&amp;", "&")
	title = strings.ReplaceAll(title, "&lt;", "<")
	title = strings.ReplaceAll(title, "&gt;", ">")
	title = strings.ReplaceAll(title, "&quot;", "\"")
	title = strings.ReplaceAll(title, "&#39;", "'")
	return strings.TrimSpace(title)
}

func extractTextFromHTML(html string) string {
	// Simple HTML to text extraction
	// Remove HTML tags and decode entities
	text := strings.ReplaceAll(html, "<br>", "\n")
	text = strings.ReplaceAll(text, "<br/>", "\n")
	text = strings.ReplaceAll(text, "<br />", "\n")
	text = strings.ReplaceAll(text, "<p>", "")
	text = strings.ReplaceAll(text, "</p>", "\n")
	text = strings.ReplaceAll(text, "<div>", "")
	text = strings.ReplaceAll(text, "</div>", "\n")
	text = strings.ReplaceAll(text, "<h1>", "")
	text = strings.ReplaceAll(text, "</h1>", "\n")
	text = strings.ReplaceAll(text, "<h2>", "")
	text = strings.ReplaceAll(text, "</h2>", "\n")
	text = strings.ReplaceAll(text, "<h3>", "")
	text = strings.ReplaceAll(text, "</h3>", "\n")
	text = strings.ReplaceAll(text, "<h4>", "")
	text = strings.ReplaceAll(text, "</h4>", "\n")
	text = strings.ReplaceAll(text, "<h5>", "")
	text = strings.ReplaceAll(text, "</h5>", "\n")
	text = strings.ReplaceAll(text, "<h6>", "")
	text = strings.ReplaceAll(text, "</h6>", "\n")
	// Remove other tags
	for strings.Contains(text, "<") && strings.Contains(text, ">") {
		start := strings.Index(text, "<")
		end := strings.Index(text, ">")
		if start < end {
			text = text[:start] + text[end+1:]
		} else {
			break
		}
	}
	// Decode common entities
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	// Clean up whitespace
	lines := strings.Split(text, "\n")
	var cleanLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleanLines = append(cleanLines, line)
		}
	}
	return strings.Join(cleanLines, "\n")
}
