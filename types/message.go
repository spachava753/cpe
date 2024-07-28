package types

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Header represents the header part of an LSP message
type Header struct {
	ContentLength int
	ContentType   string
}

// Message represents a complete LSP message
type Message struct {
	Header  Header
	Content json.RawMessage
}

// ParseMessage parses an LSP message from the given reader
func ParseMessage(r io.Reader) (*Message, error) {
	header, err := parseHeader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse header: %w", err)
	}

	content, err := parseContent(r, header.ContentLength)
	if err != nil {
		return nil, fmt.Errorf("failed to parse content: %w", err)
	}

	return &Message{
		Header:  *header,
		Content: content,
	}, nil
}

func parseHeader(r io.Reader) (*Header, error) {
	reader := bufio.NewReader(r)
	header := &Header{
		ContentType: "application/vscode-jsonrpc; charset=utf-8", // default value
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			break // End of header
		}

		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid header line: %s", line)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "Content-Length":
			length, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length: %s", value)
			}
			header.ContentLength = length
		case "Content-Type":
			header.ContentType = value
		default:
			// Ignore unknown headers
		}
	}

	if header.ContentLength == 0 {
		return nil, errors.New("missing Content-Length header")
	}

	return header, nil
}

func parseContent(r io.Reader, length int) (json.RawMessage, error) {
	content := make([]byte, length)
	_, err := io.ReadFull(r, content)
	if err != nil {
		return nil, err
	}

	// Validate that the content is valid JSON
	if !json.Valid(content) {
		return nil, errors.New("invalid JSON content")
	}

	return json.RawMessage(content), nil
}

// ParseJSONRPC parses the JSON-RPC content of the message
func (m *Message) ParseJSONRPC() (map[string]interface{}, error) {
	var result map[string]interface{}
	err := json.Unmarshal(m.Content, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON-RPC content: %w", err)
	}

	// Validate JSON-RPC structure
	if result["jsonrpc"] != "2.0" {
		return nil, errors.New("invalid JSON-RPC version")
	}

	if _, ok := result["id"]; !ok {
		return nil, errors.New("missing JSON-RPC id")
	}

	if _, ok := result["method"]; !ok {
		return nil, errors.New("missing JSON-RPC method")
	}

	return result, nil
}
