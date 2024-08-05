package _go

import (
	"bytes"
	"fmt"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"net"
	"os"
)

func main() {
	// 建立 TCP 连接
	conn, err := net.Dial("tcp", "www.example.com:443")
	if err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		return
	}
	defer conn.Close()

	// 创建 HTTP/2 Framer
	framer := http2.NewFramer(conn, conn)

	// 发送 SETTINGS 帧
	if err := framer.WriteSettings(http2.Setting{ID: http2.SettingInitialWindowSize, Val: 65535}); err != nil {
		fmt.Printf("Failed to write settings: %v\n", err)
		return
	}

	// 模拟多个文件数据
	files := [][]byte{
		bytes.Repeat([]byte("a"), 64*1024), // 64 KB
		bytes.Repeat([]byte("b"), 128*1024), // 128 KB
		bytes.Repeat([]byte("c"), 256*1024), // 256 KB
	}

	for i, file := range files {
		streamID := uint32(1 + 2*i)

		// 创建 HEADERS 帧
		headers := []hpack.HeaderField{
			{Name: ":method", Value: "POST"},
			{Name: ":path", Value: fmt.Sprintf("/upload/file%d", i+1)},
			{Name: ":scheme", Value: "https"},
			{Name: ":authority", Value: "www.example.com"},
			{Name: "content-length", Value: fmt.Sprintf("%d", len(file))},
		}
		var buf bytes.Buffer
		hpackEncoder := hpack.NewEncoder(&buf)
		for _, hf := range headers {
			hpackEncoder.WriteField(hf)
		}

		// 发送 HEADERS 帧
		if err := framer.WriteHeaders(http2.HeadersFrameParam{
			StreamID:      streamID,
			EndHeaders:    true,
			BlockFragment: buf.Bytes(),
		}); err != nil {
			fmt.Printf("Failed to write headers: %v\n", err)
			return
		}

		// 发送 DATA 帧
		for offset := 0; offset < len(file); offset += http2.DefaultMaxFrameSize {
			end := offset + http2.DefaultMaxFrameSize
			if end > len(file) {
				end = len(file)
			}
			data := file[offset:end]
			endStream := end == len(file)
			if err := framer.WriteData(streamID, endStream, data); err != nil {
				fmt.Printf("Failed to write data: %v\n", err)
				return
			}
		}
	}

	fmt.Println("Files uploaded successfully")
}

