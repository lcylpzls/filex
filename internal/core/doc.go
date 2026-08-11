// Package filex 提供自用对象存储核心引擎：本地盘后端、原子写入、
// SHA256 完整性校验、对象元数据与分页枚举，错误语义统一走 errx，
// 日志与指标可注入 logx / metricsx 生态。
//
// filex 不兼容 S3 / OSS / WebDAV 等第三方对象存储协议；
// 传输协议从 v0.2.0 起由 server / client 子包提供。
package core
