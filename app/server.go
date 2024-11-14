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

	l, err := net.Listen("tcp", "0.0.0.0:4221")
	if err != nil {
		fmt.Println("Failed to bind to port 4221")
		os.Exit(1)
	}
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting connection: ", err.Error())
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	requestLine, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("Error reading request:", err.Error())
		return
	}

	parts := strings.Split(requestLine, " ")
	if len(parts) < 2 {
		fmt.Println("Invalid request line:", requestLine)
		return
	}
	method := parts[0]
	path := parts[1]

	headers := make(map[string]string)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading header:", err.Error())
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		headerParts := strings.SplitN(line, ":", 2)
		if len(headerParts) == 2 {
			headers[strings.TrimSpace(headerParts[0])] = strings.TrimSpace(headerParts[1])
		}
	}

	// Check for Accept-Encoding header
	acceptEncoding, exists := headers["Accept-Encoding"]
	contentEncoding := ""
	if exists && strings.Contains(acceptEncoding, "gzip") {
		contentEncoding = "gzip"
	}

	if path == "/" {
		body := "Hello, this is a 200!"
		sendResponse(conn, 200, "OK", "text/plain", contentEncoding, body)
	} else if path == "/user-agent" {
		userAgent, exists := headers["User-Agent"]
		if !exists {
			userAgent = "No User-Agent found"
		}
		sendResponse(conn, 200, "OK", "text/plain", contentEncoding, userAgent)
	} else if strings.HasPrefix(path, "/echo/") {
		variable := strings.TrimPrefix(path, "/echo/")
		sendResponse(conn, 200, "OK", "text/plain", contentEncoding, variable)
	} else if strings.HasPrefix(path, "/files/") {
		switch method {
		case "GET":
			filename := strings.TrimPrefix(path, "/files/")
			serveFile(conn, filename, contentEncoding)
			return
		case "POST":
			handleFileUpload(conn, reader, headers, strings.TrimPrefix(path, "/files/"))
			return
		default:
			sendResponse(conn, 405, "Method Not Allowed", "text/plain", "", "405 Method Not Allowed")
		}
	} else {
		sendResponse(conn, 404, "Not Found", "text/plain", "", "404 Not Found")
	}
}

func sendResponse(conn net.Conn, statusCode int, statusText, contentType, contentEncoding, body string) {
	var responseBody []byte
	var contentLength int

	if contentEncoding == "gzip" {
		// Compress the body using gzip
		var buf bytes.Buffer
		gzipWriter := gzip.NewWriter(&buf)
		_, err := gzipWriter.Write([]byte(body))
		if err != nil {
			fmt.Println("Error compressing response body:", err.Error())
			return
		}
		gzipWriter.Close()
		responseBody = buf.Bytes()
		contentLength = len(responseBody)
	} else {
		responseBody = []byte(body)
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

	// Send the response
	_, err := conn.Write([]byte(statusLine + headers))
	if err != nil {
		fmt.Println("Error writing response headers:", err.Error())
		return
	}
	_, err = conn.Write(responseBody)
	if err != nil {
		fmt.Println("Error writing response body:", err.Error())
	}
}

func serveFile(conn net.Conn, filename string, contentEncoding string) {
	filePath := filepath.Join(filesDir, filename)

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			sendResponse(conn, 404, "Not Found", "text/plain", "", "404 Not Found")
		} else {
			fmt.Println("Error opening file:", err.Error())
			sendResponse(conn, 500, "Internal Server Error", "text/plain", "", "500 Internal Server Error")
		}
		return
	}
	defer file.Close()

	// Read the entire file content
	fileContent, err := io.ReadAll(file)
	if err != nil {
		fmt.Println("Error reading file:", err.Error())
		sendResponse(conn, 500, "Internal Server Error", "text/plain", "", "500 Internal Server Error")
		return
	}

	var responseBody []byte
	var contentLength int

	if contentEncoding == "gzip" {
		// Compress the file content using gzip
		var buf bytes.Buffer
		gzipWriter := gzip.NewWriter(&buf)
		_, err := gzipWriter.Write(fileContent)
		if err != nil {
			fmt.Println("Error compressing file content:", err.Error())
			sendResponse(conn, 500, "Internal Server Error", "text/plain", "", "500 Internal Server Error")
			return
		}
		gzipWriter.Close()
		responseBody = buf.Bytes()
		contentLength = len(responseBody)
	} else {
		responseBody = fileContent
		contentLength = len(responseBody)
	}

	// Build the response headers
	statusLine := "HTTP/1.1 200 OK\r\n"
	headers := "Content-Type: application/octet-stream\r\n"
	if contentEncoding != "" {
		headers += fmt.Sprintf("Content-Encoding: %s\r\n", contentEncoding)
	}
	headers += fmt.Sprintf("Content-Length: %d\r\n", contentLength)
	headers += "\r\n"

	// Send the response
	_, err = conn.Write([]byte(statusLine + headers))
	if err != nil {
		fmt.Println("Error writing response headers:", err.Error())
		return
	}
	_, err = conn.Write(responseBody)
	if err != nil {
		fmt.Println("Error writing response body:", err.Error())
	}
}

func handleFileUpload(conn net.Conn, reader *bufio.Reader, headers map[string]string, filename string) {
	// Ensure Content-Length is provided
	contentLengthStr, exists := headers["Content-Length"]
	if !exists {
		sendResponse(conn, 411, "Length Required", "text/plain", "", "411 Length Required")
		return
	}

	contentLength, err := strconv.Atoi(contentLengthStr)
	if err != nil || contentLength < 0 {
		sendResponse(conn, 400, "Bad Request", "text/plain", "", "400 Bad Request")
		return
	}

	// Read the body
	body := make([]byte, contentLength)
	_, err = io.ReadFull(reader, body)
	if err != nil {
		fmt.Println("Error reading request body:", err)
		sendResponse(conn, 500, "Internal Server Error", "text/plain", "", "500 Internal Server Error")
		return
	}

	// Write the file
	filePath := filepath.Join(filesDir, filename)
	err = os.WriteFile(filePath, body, 0644)
	if err != nil {
		fmt.Println("Error writing file:", err)
		sendResponse(conn, 500, "Internal Server Error", "text/plain", "", "500 Internal Server Error")
		return
	}

	// Respond with 201 Created
	sendResponse(conn, 201, "Created", "text/plain", "", "201 Created Successfully")
}
