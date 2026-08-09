// Package proto 定义 filex 自研对象存储协议的常量与线格式。
package proto

// 协议端点前缀。
const BasePath = "/filex/v1"

// 协议头。
const (
	HeaderSHA256    = "X-Filex-SHA256"
	HeaderMetadata  = "X-Filex-Metadata"
	HeaderSize      = "X-Filex-Size"
	HeaderRequestID = "X-Filex-Request-ID"
	HeaderCreatedAt = "X-Filex-Created-At"
)
