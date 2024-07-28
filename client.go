package cpe

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"cpe/types"
)

// Client represents an LSP client
type Client struct {
	conn            net.Conn
	reader          *bufio.Reader
	writer          *bufio.Writer
	writerMu        sync.Mutex
	nextID          int64
	requests        map[int64]chan *types.ResponseMessage
	requestsMu      sync.Mutex
	notifyHandlers  map[string]NotificationHandler
	notifyHandlerMu sync.RWMutex
	stopChan        chan struct{}
}

type NotificationHandler func(*types.NotificationMessage)

// NewClient creates a new LSP client connected to the given address
func NewClient(address string) (*Client, error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to LSP server: %w", err)
	}

	c := &Client{
		conn:           conn,
		reader:         bufio.NewReader(conn),
		writer:         bufio.NewWriter(conn),
		requests:       make(map[int64]chan *types.ResponseMessage),
		notifyHandlers: make(map[string]NotificationHandler),
		stopChan:       make(chan struct{}),
	}

	go c.processMessages()

	return c, nil
}

func (c *Client) processMessages() {
	for {
		select {
		case <-c.stopChan:
			return
		default:
			msg, err := types.ParseMessage(c.reader)
			if errors.Is(err, io.EOF) {
				fmt.Printf("EOF recieved: %v\n", err)
				return
			}

			if err != nil {
				// Handle error (log it, maybe attempt reconnection)
				fmt.Printf("Error parsing message: %v\n", err)
				continue
			}

			go c.handleMessage(msg)
		}
	}
}

func (c *Client) handleMessage(msg *types.Message) {
	var header struct {
		JSONRPC string      `json:"jsonrpc"`
		ID      interface{} `json:"id,omitempty"`
		Method  string      `json:"method,omitempty"`
	}
	if err := json.Unmarshal(msg.Content, &header); err != nil {
		fmt.Printf("Error unmarshaling message header: %v\n", err)
		return
	}

	if header.ID != nil {
		// This is a response
		var response types.ResponseMessage
		if err := json.Unmarshal(msg.Content, &response); err != nil {
			fmt.Printf("Error unmarshaling response: %v\n", err)
			return
		}
		c.handleResponse(&response)
	} else if header.Method != "" {
		// This is a notification
		var notification types.NotificationMessage
		if err := json.Unmarshal(msg.Content, &notification); err != nil {
			fmt.Printf("Error unmarshaling notification: %v\n", err)
			return
		}
		c.handleNotification(&notification)
	}
}

func (c *Client) handleResponse(response *types.ResponseMessage) {
	c.requestsMu.Lock()
	defer c.requestsMu.Unlock()

	id, ok := response.ID.(float64)
	if !ok {
		fmt.Printf("Invalid response ID type: %T\n", response.ID)
		return
	}

	ch, ok := c.requests[int64(id)]
	if !ok {
		fmt.Printf("No pending request found for ID: %v\n", id)
		return
	}

	// Send the response to the channel
	select {
	case ch <- response:
		// Response sent successfully
	default:
		// Channel is full or closed, which shouldn't happen
		fmt.Printf("Failed to send response for ID: %v\n", id)
	}
}

func (c *Client) handleNotification(notification *types.NotificationMessage) {
	c.notifyHandlerMu.RLock()
	handler, ok := c.notifyHandlers[notification.Method]
	c.notifyHandlerMu.RUnlock()

	if ok {
		handler(notification)
	} else {
		fmt.Printf("No handler registered for notification method: %s\n", notification.Method)
	}
}

func (c *Client) RegisterNotificationHandler(method string, handler NotificationHandler) {
	c.notifyHandlerMu.Lock()
	defer c.notifyHandlerMu.Unlock()
	c.notifyHandlers[method] = handler
}

func (c *Client) SendRequest(method string, params interface{}) (*types.ResponseMessage, error) {
	id := atomic.AddInt64(&c.nextID, 1)
	request, err := types.NewRequest(id, method, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	responseChan := make(chan *types.ResponseMessage, 1)
	c.requestsMu.Lock()
	c.requests[id] = responseChan
	c.requestsMu.Unlock()

	defer func() {
		c.requestsMu.Lock()
		delete(c.requests, id)
		c.requestsMu.Unlock()
		close(responseChan)
	}()

	if sendMsgErr := c.sendMessage(request); sendMsgErr != nil {
		return nil, fmt.Errorf("failed to send request: %w", sendMsgErr)
	}

	select {
	case response := <-responseChan:
		return response, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("request timed out")
	}
}

func (c *Client) SendNotification(method string, params interface{}) error {
	notification, err := types.NewNotification(method, params)
	if err != nil {
		return fmt.Errorf("failed to create notification: %w", err)
	}

	return c.sendMessage(notification)
}

func (c *Client) sendMessage(message interface{}) error {
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))

	c.writerMu.Lock()
	defer c.writerMu.Unlock()

	if n, writeHeaderErr := c.writer.WriteString(header); writeHeaderErr != nil {
		return fmt.Errorf("failed to write header, wrote %d bytes: %w", n, writeHeaderErr)
	}

	if n, writeDataErr := c.writer.Write(data); writeDataErr != nil {
		return fmt.Errorf("failed to write message, wrote %d bytes: %w", n, writeDataErr)
	}

	return c.writer.Flush()
}

func (c *Client) Close() error {
	close(c.stopChan)
	return c.conn.Close()
}
