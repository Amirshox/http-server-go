package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var filesDir string

func main() {
	fmt.Println("Logs from your program will appear here!")

	dirFlag := flag.String("directory", "", "Directory to serve files from")
	flag.Parse()
	filesDir = *dirFlag

	listener, err := net.Listen("tcp", "0.0.0.0:4221")
	if err != nil {
		fmt.Println("Failed to bind to port 4221:", err)
		os.Exit(1)
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	req, err := parseRequest(conn)
	if err != nil {
		fmt.Println("Error parsing request:", err)
		return
	}

	// Determine if client supports gzip compression
	contentEncoding := ""
	if acceptEncodings, ok := req.Headers["Accept-Encoding"]; ok {
		if supportsGzip(acceptEncodings) {
			contentEncoding = "gzip"
		}
	}

	switch {
	case req.Path == "/":
		body := "Hello, this is a 200!"
		sendResponse(conn, 200, "OK", "text/plain", contentEncoding, []byte(body))
	case req.Path == "/user-agent":
		userAgent := req.Headers["User-Agent"]
		if userAgent == "" {
			userAgent = "No User-Agent found"
		}
		sendResponse(conn, 200, "OK", "text/plain", contentEncoding, []byte(userAgent))
	case strings.HasPrefix(req.Path, "/echo/"):
		echoStr := strings.TrimPrefix(req.Path, "/echo/")
		sendResponse(conn, 200, "OK", "text/plain", contentEncoding, []byte(echoStr))
	case strings.HasPrefix(req.Path, "/files/"):
		filename := strings.TrimPrefix(req.Path, "/files/")
		if req.Method == "GET" {
			serveFile(conn, filename, contentEncoding)
		} else if req.Method == "POST" {
			handleFileUpload(conn, req, filename)
		} else {
			sendErrorResponse(conn, 405, "Method Not Allowed")
		}
	default:
		sendErrorResponse(conn, 404, "Not Found")
	}
}

type Request struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    io.Reader
}

func parseRequest(conn net.Conn) (*Request, error) {
	reader := bufio.NewReader(conn)
	requestLine, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	requestLine = strings.TrimSpace(requestLine)
	parts := strings.Split(requestLine, " ")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid request line: %s", requestLine)
	}
	method := parts[0]
	path := parts[1]

	headers := make(map[string]string)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		headerParts := strings.SplitN(line, ":", 2)
		if len(headerParts) == 2 {
			key := strings.TrimSpace(headerParts[0])
			value := strings.TrimSpace(headerParts[1])
			headers[key] = value
		}
	}

	var body io.Reader
	if method == "POST" || method == "PUT" {
		contentLengthStr := headers["Content-Length"]
		if contentLengthStr == "" {
			return nil, fmt.Errorf("missing Content-Length header")
		}
		contentLength, err := strconv.Atoi(contentLengthStr)
		if err != nil || contentLength < 0 {
			return nil, fmt.Errorf("invalid Content-Length header")
		}
		body = io.LimitReader(reader, int64(contentLength))
	}

	return &Request{
		Method:  method,
		Path:    path,
		Headers: headers,
		Body:    body,
	}, nil
}

func supportsGzip(acceptEncodings string) bool {
	for _, encoding := range strings.Split(acceptEncodings, ",") {
		if strings.TrimSpace(encoding) == "gzip" {
			return true
		}
	}
	return false
}

func sendResponse(conn net.Conn, statusCode int, statusText, contentType, contentEncoding string, body []byte) {
	var responseBody []byte
	var contentLength int
	var err error

	if contentEncoding == "gzip" {
		// Compress the body using gzip
		var buf bytes.Buffer
		gzipWriter := gzip.NewWriter(&buf)
		_, err = gzipWriter.Write(body)
		if err != nil {
			fmt.Println("Error compressing response body:", err)
			return
		}
		gzipWriter.Close()
		responseBody = buf.Bytes()
		contentLength = len(responseBody)
	} else {
		responseBody = body
		contentLength = len(responseBody)
	}

	// Build the response headers
	statusLine := fmt.Sprintf("HTTP/1.1 %d %s\r\n", statusCode, statusText)
	headers := fmt.Sprintf("Content-Type: %s\r\n", contentType)
	if contentEncoding != "" {
		headers += fmt.Sprintf("Content-Encoding: %s\r\n", contentEncoding)
	}
	headers += fmt.Sprintf("Content-Length: %d\r\n", contentLength)
	headers += "\r\n"

	// Send the response headers and body
	_, err = conn.Write([]byte(statusLine + headers))
	if err != nil {
		fmt.Println("Error writing response headers:", err)
		return
	}
	_, err = conn.Write(responseBody)
	if err != nil {
		fmt.Println("Error writing response body:", err)
	}
}

func sendErrorResponse(conn net.Conn, statusCode int, statusText string) {
	body := fmt.Sprintf("%d %s", statusCode, statusText)
	sendResponse(conn, statusCode, statusText, "text/plain", "", []byte(body))
}

func serveFile(conn net.Conn, filename, contentEncoding string) {
	filePath := filepath.Join(filesDir, filename)
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			sendErrorResponse(conn, 404, "Not Found")
		} else {
			fmt.Println("Error reading file:", err)
			sendErrorResponse(conn, 500, "Internal Server Error")
		}
		return
	}

	sendResponse(conn, 200, "OK", "application/octet-stream", contentEncoding, fileContent)
}

func handleFileUpload(conn net.Conn, req *Request, filename string) {
	if req.Body == nil {
		sendErrorResponse(conn, 411, "Length Required")
		return
	}

	fileContent, err := io.ReadAll(req.Body)
	if err != nil {
		fmt.Println("Error reading request body:", err)
		sendErrorResponse(conn, 500, "Internal Server Error")
		return
	}

	filePath := filepath.Join(filesDir, filename)
	err = os.WriteFile(filePath, fileContent, 0644)
	if err != nil {
		fmt.Println("Error writing file:", err)
		sendErrorResponse(conn, 500, "Internal Server Error")
		return
	}

	sendResponse(conn, 201, "Created", "text/plain", "", []byte("201 Created Successfully"))
}
