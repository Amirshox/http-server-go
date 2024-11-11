package main

import (
	"bufio"
	"flag"
	"fmt"
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

	var response string
	if path == "/" {
		response = "HTTP/1.1 200 OK\r\n" +
			"Content-Type: text/plain\r\n" +
			"Content-Length: 20\r\n" +
			"\r\n" +
			"Hello, this is a 200!"
	} else if path == "/user-agent" {
		userAgent, exists := headers["User-Agent"]
		if !exists {
			userAgent = "No User-Agent found"
		}
		contentLength := strconv.Itoa(len(userAgent))
		response = "HTTP/1.1 200 OK\r\n" +
			"Content-Type: text/plain\r\n" +
			"Content-Length: " + contentLength + "\r\n" +
			"\r\n" + userAgent
	} else if strings.HasPrefix(path, "/echo/") {
		variable := strings.TrimPrefix(path, "/echo/")
		response = "HTTP/1.1 200 OK\r\n" +
			"Content-Type: text/plain\r\n" +
			"Content-Length: " + fmt.Sprint(len(variable)) + "\r\n" +
			"\r\n" + variable
	} else if strings.HasPrefix(path, "/files/") {
		filename := strings.TrimPrefix(path, "/files/")
		serveFile(conn, filename)
	} else {
		response = "HTTP/1.1 404 Not Found\r\n" +
			"Content-Type: text/plain\r\n" +
			"Content-Length: 13\r\n" +
			"\r\n" + "404 Not Found"
	}

	_, err = conn.Write([]byte(response))
	if err != nil {
		fmt.Println("Error writing response:", err.Error())
	}
}

func serveFile(conn net.Conn, filename string) {
	filePath := filepath.Join(filesDir, filename)

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			response := "HTTP/1.1 404 Not Found\r\n\r\n"
			conn.Write([]byte(response))
		} else {
			fmt.Println("Error opening file:", err.Error())
		}
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		fmt.Println("Error stating file:", err.Error())
		return
	}
	contentLength := strconv.FormatInt(fileInfo.Size(), 10)

	responseHeaders := "HTTP/1.1 200 OK\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"Content-Length: " + contentLength + "\r\n\r\n"

	_, err = conn.Write([]byte(responseHeaders))
	if err != nil {
		fmt.Println("Error writing headers:", err.Error())
		return
	}

	buffer := make([]byte, 1024)
	for {
		n, err := file.Read(buffer)
		if err != nil {
			break
		}
		conn.Write(buffer[:n])
	}
}
