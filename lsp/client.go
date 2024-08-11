package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	jsonRpcVersion  = "2.0"
	noContentLength = -1
)

type Client struct {
	s *Server
}

type Message struct {
	Jsonrpc string `json:"jsonrpc"`
	Id      string `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type Response struct {
	Headers      map[string]string `json:"-"`
	Id           string            `json:"id"`
	Result       any               `json:"result,omitempty"`
	Error        *ResponseError    `json:"error,omitempty"`
	Notification bool              `json:"-"`
}

func (req *Message) fill() {
	req.Jsonrpc = jsonRpcVersion
}

func NewClient(s *Server) *Client {
	return &Client{s: s}
}

func (c *Client) Send(req *Message) (*Response, error) {
	c.s.lock()
	defer c.s.unlock()

	req.fill()

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("unable to json marshal request: %w", err)
	}

	length := strconv.Itoa(len(data))
	_, err = c.s.write([]byte(fmt.Sprintf("Content-Length: %v\r\n\r\n", length)))
	if err != nil {
		return nil, fmt.Errorf("error sending headers to server: %w", err)
	}

	_, err = c.s.write(data)
	if err != nil {
		return nil, fmt.Errorf("error sending content to server: %w", err)
	}

	// Notification
	if req.Id == "" {
		return &Response{Notification: true}, nil
	}

	lrp := newResponseParser(req.Id)
	buf := make([]byte, 4096)

	for {
		n, ioErr := c.s.read(buf)
		resp, err := lrp.write(buf[:n])

		switch {
		case ioErr == io.EOF:
			return nil, fmt.Errorf("read error: EOF")
		case ioErr != nil:
			return nil, fmt.Errorf("read error: %w", err)
		case err != nil:
			return nil, fmt.Errorf("error reading LSP server output: %w", err)
		}

		if resp != nil {
			return resp, nil
		}
	}
}

type responseParser struct {
	received      bytes.Buffer
	current       bytes.Buffer
	i             int
	headers       map[string]string
	parsedHeaders bool
	last          byte
	contentLength int
	id            string
}

func newResponseParser(id string) *responseParser {
	return &responseParser{
		headers: make(map[string]string),
		id:      id,
	}
}

func (lrp *responseParser) getContentLength() int {
	for k, v := range lrp.headers {
		if strings.ToLower(k) != "content-length" {
			continue
		}

		n, err := strconv.Atoi(v)
		if err != nil {
			return noContentLength
		}
		return n
	}

	return noContentLength
}

func (lrp *responseParser) write(data []byte) (*Response, error) {
	lrp.received.Write(data)

	receivedBytes := lrp.received.Bytes()
	for ; lrp.i < len(receivedBytes); lrp.i++ {
		c := receivedBytes[lrp.i]

		if lrp.parsedHeaders {
			lrp.current.WriteByte(c)

			if lrp.current.Len() == lrp.contentLength {
				if lrp.i < len(receivedBytes)-1 {
					return nil, fmt.Errorf("received content exceeded content-length")
				}

				var resp Response
				err := json.Unmarshal(lrp.current.Bytes(), &resp)
				if err != nil {
					return nil, fmt.Errorf("unable to json unmarshal response content")
				}
				resp.Headers = lrp.headers
				return &resp, nil
			}
		} else if c == '\n' && lrp.last == '\r' {
			if lrp.current.Len() == 0 {
				if len(lrp.headers) == 0 {
					return nil, fmt.Errorf("did not receive response headers")
				}

				lrp.parsedHeaders = true

				lrp.contentLength = lrp.getContentLength()
				if lrp.contentLength == noContentLength {
					return nil, fmt.Errorf("did not receive a valid content length")
				}
			} else {
				line := lrp.current.String()
				k, v, _ := strings.Cut(line, ": ")
				if k == "" {
					return nil, fmt.Errorf("received header with empty name")
				}

				lrp.headers[k] = v
				lrp.current.Reset()
			}
		} else if c != '\r' && c != '\n' {
			lrp.current.WriteByte(c)
		}

		lrp.last = c
	}

	return nil, nil
}
