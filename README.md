# ses-ip-pool-mng

基于 Go + AWS SDK v2 的 SES Dedicated IP 池管理 Demo，提供 HTTP API。

## 功能

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/pools` | 列出账户中所有 dedicated IP 池名称 |
| GET | `/pools?region=&configset=` | 按 region / configset 过滤池名称 |
| GET | `/pools/{name}/ips?region=` | 获取指定池中的 IP 列表 |
| POST | `/pools/{name}/ips` | 将账户内已有的 dedicated IP 移入指定池 |

## 环境要求

- Go 1.24+
- 有效的 AWS 凭证（见下方说明）
- IAM 权限：`ses:ListDedicatedIpPools`、`ses:GetDedicatedIps`、`ses:GetConfigurationSet`、`ses:PutDedicatedIpInPool`

## AWS 凭证配置

SDK 按以下顺序自动查找凭证，优先级从高到低：

### 1. 环境变量（推荐用于临时调试）

```bash
export AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
export AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
export AWS_SESSION_TOKEN=...        # 使用临时凭证（STS/AssumeRole）时需要
export AWS_REGION=us-east-1
```

### 2. 共享凭证文件 `~/.aws/credentials`

```ini
[default]
aws_access_key_id     = AKIAIOSFODNN7EXAMPLE
aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY

[prod]
aws_access_key_id     = AKIAI44QH8DHBEXAMPLE
aws_secret_access_key = je7MtGbClwBF/2Zp9Utk/h3yCo8nvbEXAMPLEKEY
```

配置文件 `~/.aws/config` 中指定默认 region：

```ini
[default]
region = us-east-1

[profile prod]
region = ap-east-1
```

切换 profile：

```bash
export AWS_PROFILE=prod
```

### 3. IAM Role（EC2 / ECS / EKS）

部署在 AWS 计算资源上时，绑定对应 IAM Role 即可，无需配置任何密钥文件。

### 4. AWS SSO（`aws sso login`）

```bash
aws configure sso
aws sso login --profile my-sso-profile
export AWS_PROFILE=my-sso-profile
```

## 启动

```bash
go run main.go
# Listening on :8080
```

指定端口（可在 main.go 中修改，或自行扩展为环境变量）。

## API 使用示例

### 获取所有 IP 池

```bash
curl http://localhost:8080/pools
```

```json
{
  "pools": ["pool-transactional", "pool-marketing"],
  "region": "us-east-1"
}
```

### 按 region 过滤

```bash
curl "http://localhost:8080/pools?region=ap-east-1"
```

### 按 configset 过滤（返回该配置集绑定的池）

```bash
curl "http://localhost:8080/pools?configset=transactional-config"
```

```json
{
  "configset": "transactional-config",
  "pools": ["pool-transactional"],
  "region": "us-east-1"
}
```

### 同时指定 region 和 configset

```bash
curl "http://localhost:8080/pools?region=ap-east-1&configset=transactional-config"
```

### 获取指定池的 IP 列表

```bash
curl http://localhost:8080/pools/pool-transactional/ips
```

```json
{
  "pool": "pool-transactional",
  "ips": [
    {
      "Ip": "198.51.100.1",
      "WarmupPercentage": 100,
      "WarmupStatus": "DONE",
      "PoolName": "pool-transactional"
    }
  ]
}
```

### 将已有 dedicated IP 移入指定池

```bash
curl -X POST http://localhost:8080/pools/pool-transactional/ips \
     -H 'Content-Type: application/json' \
     -d '{"ip": "198.51.100.2", "region": "us-east-1"}'
```

```json
{
  "ip": "198.51.100.2",
  "message": "IP moved to pool",
  "pool": "pool-transactional"
}
```

> **注意**：`POST /pools/{name}/ips` 调用的是 `PutDedicatedIpInPool`，
> 作用是将账户内**已有的** dedicated IP 移至目标池，而非向 AWS 申请新 IP。
> 如需申请额外的 dedicated IP，请通过 AWS 控制台或 Support 提交请求。

## 错误响应格式

所有错误统一返回 JSON：

```json
{ "error": "错误描述" }
```
