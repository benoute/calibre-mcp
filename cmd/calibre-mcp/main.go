package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/cors"
)

func parseFlags() (transport string, port string, libraryPath string) {
	flag.StringVar(&transport, "transport", "stdio", "Transport mode: stdio or http")
	flag.StringVar(&port, "port", "8080", "Port to listen on for http mode")
	flag.StringVar(&libraryPath, "library-path", ".", "Path to the Calibre library directory")
	flag.Parse()

	return transport, port, libraryPath
}

func main() {
	transport, port, libraryPath := parseFlags()

	// Create a server with search and book retrieval tools
	server := setupMCPServer(libraryPath)

	// Run the server based on transport
	switch transport {
	case "http":
		fmt.Printf("Server running in HTTP mode on port %s\n", port)
		handler := mcp.NewStreamableHTTPHandler(
			func(*http.Request) *mcp.Server { return server },
			nil,
		)
		// Create CORS handler
		corsHandler := cors.New(cors.Options{
			AllowOriginFunc: func(origin string) bool {
				return true
			},
			AllowedMethods: []string{"GET", "POST", "OPTIONS"}, // OPTIONS for preflight
			AllowedHeaders: []string{
				"Content-Type",
				"Authorization",
				"Mcp-Session-Id",
				"mcp-protocol-version",
			},
			ExposedHeaders:   []string{"Mcp-Session-Id"}, // Allow clients to read session ID
			AllowCredentials: true,                       // If using auth cookies
			MaxAge:           300,                        // Cache preflight for 5 minutes
		}).Handler(handler)

		log.Println("Starting MCP server on :" + port)
		if err := http.ListenAndServe(":"+port, corsHandler); err != nil {
			log.Fatal(err)
		}
	case "stdio":
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatal(errors.New("invalid transport"))
	}
}
