# 基准测试

> 方法：`go test -run '^$' -bench . -benchmem -benchtime=200x .`

## 环境

- Windows 11 / AMD Ryzen 5 7600 / NVMe；
- Go 1.26.5，默认配置（fsync 开启、无加密）；
- 对象 1 KiB；List 场景 1000 个对象。

## 结果

| 基准 | 耗时/op | 内存/op | 分配/op |
| --- | --- | --- | --- |
| BenchmarkPutSmall（1 KiB 写入+fsync） | 3.65 ms | 46 KB | 114 |
| BenchmarkGetSmall（1 KiB 读取） | 0.19 ms | 7.8 KB | 62 |
| BenchmarkList1000（1000 对象枚举） | 35.8 ms | 3.3 MB | 22 069 |
| BenchmarkPutConcurrent（4 KiB，同键并发） | 7.69 ms | 47 KB | 117 |
| BenchmarkGetConcurrent（4 KiB，同键并发） | 0.038 ms | 8.7 KB | 64 |

## 解读

- 写入耗时主要由 fsync 与元数据两次原子提交构成，属预期；
- 读取为毫秒级以下，可支撑常规文件服务；
- 并发读取约 26K ops/s（单机 12 核，受 fsync 与同键写锁影响）；
- List 目前为全量扫描元数据后内存排序，对象量大时线性增长；
  自用规模（万级以内）可接受，后续如需海量枚举再引入索引。

## 复现

```powershell
go test -run '^$' -bench 'BenchmarkPutSmall|BenchmarkGetSmall|BenchmarkList1000' -benchmem -benchtime=200x .
```
