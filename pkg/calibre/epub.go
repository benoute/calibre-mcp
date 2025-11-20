package calibre

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/html"
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
	Id         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

// NCX structures for EPUB 2 TOC
type NCX struct {
	XMLName xml.Name `xml:"ncx"`
	NavMap  NavMap   `xml:"navMap"`
}

type NavMap struct {
	NavPoints []NavPoint `xml:"navPoint"`
}

type NavPoint struct {
	NavLabel NavLabel `xml:"navLabel"`
	Content  Content  `xml:"content"`
}

type NavLabel struct {
	Text string `xml:"text"`
}

type Content struct {
	Src string `xml:"src,attr"`
}

func extractText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var text strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text.WriteString(extractText(c))
	}
	return text.String()
}

func parseNavTOC(htmlContent string) map[string]string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}

	tocMap := make(map[string]string)

	var findNav func(*html.Node)
	findNav = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "nav" {
			// Check if it has type="toc" or epub:type="toc"
			for _, attr := range n.Attr {
				if (attr.Key == "type" || attr.Key == "epub:type") && strings.Contains(attr.Val, "toc") {
					// Found TOC nav, parse the ol/li/a
					var parseList func(*html.Node)
					parseList = func(ln *html.Node) {
						if ln.Type == html.ElementNode && ln.Data == "a" {
							href := ""
							for _, a := range ln.Attr {
								if a.Key == "href" {
									href = a.Val
									break
								}
							}
							if href != "" {
								// Strip fragment
								if idx := strings.Index(href, "#"); idx != -1 {
									href = href[:idx]
								}
								text := extractText(ln)
								if text != "" {
									tocMap[href] = text
								}
							}
						}
						for c := ln.FirstChild; c != nil; c = c.NextSibling {
							parseList(c)
						}
					}
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						parseList(c)
					}
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findNav(c)
		}
	}

	findNav(doc)
	return tocMap
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

	// Build href map, resolving relative to OPF directory
	opfDir := filepath.Dir(opfPath)
	hrefMap := make(map[string]string)
	for _, item := range pkg.Manifest.Items {
		fullHref := filepath.Join(opfDir, item.Href)
		hrefMap[item.Id] = fullHref
	}

	// Build title map from TOC
	titleMap := make(map[string]string)
	for _, item := range pkg.Manifest.Items {
		if strings.Contains(item.Properties, "nav") || item.MediaType == "application/x-dtbncx+xml" {
			tocHref := filepath.Join(opfDir, item.Href)
			tocFile, err := r.Open(tocHref)
			if err == nil {
				tocData, err := io.ReadAll(tocFile)
				tocFile.Close()
				if err == nil {
					tocDir := filepath.Dir(tocHref)
					if item.MediaType == "application/x-dtbncx+xml" {
						// Parse NCX (src relative to OPF)
						var ncx NCX
						if xml.Unmarshal(tocData, &ncx) == nil {
							for _, np := range ncx.NavMap.NavPoints {
								src := np.Content.Src
								// Strip fragment
								if idx := strings.Index(src, "#"); idx != -1 {
									src = src[:idx]
								}
								fullSrc := filepath.Join(opfDir, src)
								titleMap[fullSrc] = np.NavLabel.Text
							}
						}
					} else {
						// Parse nav document (src relative to nav file)
						navTitles := parseNavTOC(string(tocData))
						for src, title := range navTitles {
							fullSrc := filepath.Join(tocDir, src)
							titleMap[fullSrc] = title
						}
					}
				}
			}
			break // assume only one TOC
		}
	}

	// Get chapters from spine
	chapters := make([]Chapter, 0)
	for i, itemref := range pkg.Spine.Itemrefs {
		href, ok := hrefMap[itemref.Idref]
		if !ok {
			continue
		}
		// Default to filename without extension
		base := filepath.Base(href)
		ext := filepath.Ext(base)
		title := strings.TrimSuffix(base, ext)
		// Override with TOC title if available
		if tocTitle, exists := titleMap[href]; exists && tocTitle != "" {
			title = tocTitle
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
