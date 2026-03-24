package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// cmdRAG handles /rag [add|list|search|remove] subcommands.
// It must be initialized with a RAGStore via SetRAGStore.
func (r *Reflector) cmdRAG(args []string, _ *MemoryStore) string {
	r.mu.RLock()
	rs := r.ragStore
	r.mu.RUnlock()

	if rs == nil {
		return "RAG not configured (no embedding provider set)"
	}

	if len(args) == 0 {
		return "Usage: /rag [add|list|search|remove]\n" +
			"  /rag add <path|url>  - index a file or URL\n" +
			"  /rag list            - list indexed sources\n" +
			"  /rag search <query>  - search knowledge base\n" +
			"  /rag remove <source> - remove from index"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	switch args[0] {
	case "add":
		return r.ragAdd(ctx, rs, args[1:])
	case "list":
		return r.ragList(rs)
	case "search":
		return r.ragSearch(ctx, rs, args[1:])
	case "remove":
		return r.ragRemove(ctx, rs, args[1:])
	default:
		return fmt.Sprintf("Unknown subcommand: %s. Use /rag for help.", args[0])
	}
}

func (r *Reflector) ragAdd(ctx context.Context, rs *RAGStore, args []string) string {
	if len(args) == 0 {
		return "Usage: /rag add <file-path|url>"
	}
	source := args[0]

	var content string
	var err error

	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		content, err = fetchURL(ctx, source)
	} else {
		content, err = readFile(source)
	}
	if err != nil {
		return fmt.Sprintf("Failed to read: %v", err)
	}
	if len(content) < 50 {
		return "Content too short to index (< 50 chars)"
	}

	if err := rs.Index(ctx, source, content); err != nil {
		return fmt.Sprintf("Index failed: %v", err)
	}
	return fmt.Sprintf("Indexed: %s (%d chars)", source, len(content))
}

func (r *Reflector) ragList(rs *RAGStore) string {
	sources, err := rs.ListSources()
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if len(sources) == 0 {
		return "No sources indexed yet."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Indexed Sources (%d)\n\n", len(sources))
	for i, s := range sources {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, s)
	}
	return sb.String()
}

func (r *Reflector) ragSearch(ctx context.Context, rs *RAGStore, args []string) string {
	if len(args) == 0 {
		return "Usage: /rag search <query>"
	}
	query := strings.Join(args, " ")

	chunks, err := rs.Search(ctx, query, 5)
	if err != nil {
		return fmt.Sprintf("Search failed: %v", err)
	}
	if len(chunks) == 0 {
		return "No results found."
	}

	return FormatChunks(chunks)
}

func (r *Reflector) ragRemove(ctx context.Context, rs *RAGStore, args []string) string {
	if len(args) == 0 {
		return "Usage: /rag remove <source>"
	}
	source := args[0]
	if err := rs.RemoveSource(ctx, source); err != nil {
		return fmt.Sprintf("Remove failed: %v", err)
	}
	return fmt.Sprintf("Removed: %s", source)
}

func fetchURL(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
