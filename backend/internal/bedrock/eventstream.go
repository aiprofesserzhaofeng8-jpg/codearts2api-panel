// SPDX-License-Identifier: LGPL-3.0-only
// Decoder extracted from Sub2API bedrock_stream.go; see UPSTREAM.md.
package bedrock

import (
	"bufio"
	"codearts2api/internal/provider"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
)

type bedrockEventStreamDecoder struct {
	reader *bufio.Reader
}

// AWS EventStream 协议上限（与 AWS SDK 的 eventstream 解码器一致）。帧长度来自
// 上游，必须在分配前校验，否则单个伪造的 prelude 即可申请约 4 GiB。
const (
	bedrockEventStreamMaxHeadersLength = 128 << 10
	bedrockEventStreamMaxPayloadLength = 16 << 20
)

func newBedrockEventStreamDecoder(r io.Reader) *bedrockEventStreamDecoder {
	return &bedrockEventStreamDecoder{
		reader: bufio.NewReaderSize(r, 64*1024),
	}
}

// Decode 读取下一个 EventStream 帧并返回 chunk 类型事件的 payload
func (d *bedrockEventStreamDecoder) Decode() ([]byte, error) {
	for {
		// 读取 prelude: total_length(4) + headers_length(4) + prelude_crc(4) = 12 bytes
		prelude := make([]byte, 12)
		if _, err := io.ReadFull(d.reader, prelude); err != nil {
			return nil, err
		}

		// 验证 prelude CRC（AWS EventStream 使用标准 CRC32 / IEEE）
		preludeCRC := bedrockReadUint32(prelude[8:12])
		if crc32.Checksum(prelude[0:8], crc32IEEETable) != preludeCRC {
			return nil, fmt.Errorf("eventstream prelude CRC mismatch")
		}

		totalLength := bedrockReadUint32(prelude[0:4])
		headersLength := bedrockReadUint32(prelude[4:8])

		if totalLength < 16 { // minimum: 12 prelude + 4 message_crc
			return nil, fmt.Errorf("invalid eventstream frame: total_length=%d", totalLength)
		}
		if headersLength > bedrockEventStreamMaxHeadersLength || headersLength > totalLength-16 {
			return nil, fmt.Errorf("invalid eventstream frame: headers_length=%d total_length=%d", headersLength, totalLength)
		}
		if totalLength-16-headersLength > bedrockEventStreamMaxPayloadLength {
			return nil, fmt.Errorf("invalid eventstream frame: payload too large, total_length=%d", totalLength)
		}

		// 读取 headers + payload + message_crc
		remaining := int(totalLength) - 12
		if remaining <= 0 {
			continue
		}
		data := make([]byte, remaining)
		if _, err := io.ReadFull(d.reader, data); err != nil {
			return nil, err
		}

		// 验证 message CRC（覆盖 prelude + headers + payload）
		messageCRC := bedrockReadUint32(data[len(data)-4:])
		h := crc32.New(crc32IEEETable)
		_, _ = h.Write(prelude)
		_, _ = h.Write(data[:len(data)-4])
		if h.Sum32() != messageCRC {
			return nil, fmt.Errorf("eventstream message CRC mismatch")
		}

		// 解析 headers
		headers := data[:headersLength]
		payload := data[headersLength : len(data)-4] // 去掉 message_crc

		// 从 headers 中提取 :event-type
		eventType := extractEventStreamHeaderValue(headers, ":event-type")

		// 只处理 chunk 事件
		if eventType == "chunk" {
			// payload 是完整的 JSON，包含 bytes 字段
			return payload, nil
		}

		// 检查异常事件
		exceptionType := extractEventStreamHeaderValue(headers, ":exception-type")
		if exceptionType != "" {
			return nil, streamException(exceptionType)
		}

		messageType := extractEventStreamHeaderValue(headers, ":message-type")
		if messageType == "exception" || messageType == "error" {
			return nil, streamException(eventType)
		}

		// 跳过其他事件类型（如 initial-response）
	}
}

// extractEventStreamHeaderValue 从 EventStream headers 二进制数据中提取指定 header 的字符串值
// EventStream header 格式：
//
//	[name_length: 1 byte][name: variable][value_type: 1 byte][value: variable]
//
// value_type = 7 表示 string 类型，前 2 bytes 为长度
func extractEventStreamHeaderValue(headers []byte, targetName string) string {
	pos := 0
	for pos < len(headers) {
		if pos >= len(headers) {
			break
		}
		nameLen := int(headers[pos])
		pos++
		if pos+nameLen > len(headers) {
			break
		}
		name := string(headers[pos : pos+nameLen])
		pos += nameLen

		if pos >= len(headers) {
			break
		}
		valueType := headers[pos]
		pos++

		switch valueType {
		case 7: // string
			if pos+2 > len(headers) {
				return ""
			}
			valueLen := int(bedrockReadUint16(headers[pos : pos+2]))
			pos += 2
			if pos+valueLen > len(headers) {
				return ""
			}
			value := string(headers[pos : pos+valueLen])
			pos += valueLen
			if name == targetName {
				return value
			}
		case 0: // bool true
			if name == targetName {
				return "true"
			}
		case 1: // bool false
			if name == targetName {
				return "false"
			}
		case 2: // byte
			pos++
			if name == targetName {
				return ""
			}
		case 3: // short
			pos += 2
			if name == targetName {
				return ""
			}
		case 4: // int
			pos += 4
			if name == targetName {
				return ""
			}
		case 5: // long
			pos += 8
			if name == targetName {
				return ""
			}
		case 6: // bytes
			if pos+2 > len(headers) {
				return ""
			}
			valueLen := int(bedrockReadUint16(headers[pos : pos+2]))
			pos += 2 + valueLen
		case 8: // timestamp
			pos += 8
		case 9: // uuid
			pos += 16
		default:
			return "" // 未知类型，无法继续解析
		}
	}
	return ""
}

// crc32IEEETable is the CRC32 / IEEE table used by AWS EventStream.
var crc32IEEETable = crc32.MakeTable(crc32.IEEE)

func bedrockReadUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func bedrockReadUint16(b []byte) uint16 {
	return uint16(b[0])<<8 | uint16(b[1])
}

// Error classification uses only protocol header names, never upstream bodies.
func streamException(code string) error {
	status := 502
	switch strings.ToLower(code) {
	case "throttlingexception":
		status = 429
	case "validationexception":
		status = 400
	case "accessdeniedexception":
		status = 403
	case "modeltimeoutexception":
		status = 504
	case "serviceunavailableexception":
		status = 503
	}
	return &provider.Error{Status: status, Message: "Bedrock 上游流返回异常"}
}
