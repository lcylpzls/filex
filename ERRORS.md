# filex 错误码表

| 错误码 | 含义 | Kind | 建议 HTTP |
| --- | --- | --- | --- |
| `filex_invalid_argument` | 参数非法 | invalid_argument | 400 |
| `filex_invalid_config` | 配置非法 | invalid_argument | 400 |
| `filex_invalid_bucket` | 桶名非法 | invalid_argument | 400 |
| `filex_invalid_key` | 键名非法 | invalid_argument | 400 |
| `filex_invalid_metadata` | 元数据非法 | invalid_argument | 400 |
| `filex_invalid_range` | 范围非法 | invalid_argument | 400 |
| `filex_internal` | 服务器内部错误 | internal | 500 |
| `filex_unauthorized` | 未认证 | unauthorized | 401 |
| `filex_forbidden` | 无权限 | forbidden | 403 |
| `filex_not_modified` | 对象未修改（304 语义） | conflict | 304 |
| `filex_precondition_failed` | 前置条件不满足（412 语义） | conflict | 412 |
| `filex_cancelled` | 操作已取消 | cancelled | 499 |
| `filex_bucket_exists` | 桶已存在 | already_exists | 409 |
| `filex_bucket_not_found` | 桶不存在 | not_found | 404 |
| `filex_bucket_not_empty` | 桶非空 | conflict | 409 |
| `filex_object_not_found` | 对象不存在 | not_found | 404 |
| `filex_object_too_large` | 对象超过大小上限 | invalid_argument | 400 |
| `filex_checksum_mismatch` | SHA256 校验失败 | data_loss | 500 |
| `filex_metadata_corrupt` | 元数据损坏 | data_loss | 500 |
| `filex_storage_failed` | 存储 IO 失败 | unavailable | 503 |
| `filex_upload_not_found` | 分片上传会话不存在 | not_found | 404 |
| `filex_upload_invalid` | 分片上传参数非法 | invalid_argument | 400 |
| `filex_upload_incomplete` | 分片上传不完整 | invalid_argument | 400 |
| `filex_version_not_found` | 对象版本不存在 | not_found | 404 |
| `filex_quota_exceeded` | 配额超限 | quota_exceeded | 429 |
