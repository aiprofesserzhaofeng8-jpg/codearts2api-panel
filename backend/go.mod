module codearts2api

go 1.27.0

// 固定一个已知可用的工具链版本，保证本地与 CI 构建一致。
// 若本机 Go 版本更低，go 命令会自动下载该工具链。
toolchain go1.27.3

require github.com/aws/aws-sdk-go-v2 v1.41.5

require github.com/aws/smithy-go v1.24.2 // indirect
