# go-intervals-icu

一个轻量的 Intervals.icu API Go 客户端，基于 `openapi-spec.json` 生成。

## 安装

```bash
go get github.com/NenoSann/go-intervals-icu
```

## 快速开始

```go
package main

import (
    "context"
    "log"

    intervalsicu "github.com/NenoSann/go-intervals-icu"
)

func main() {
    client, err := intervalsicu.NewClient("YOUR_API_KEY", "ATHLETE_ID")
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // 获取默认 athlete 信息
    athlete, err := client.GetAthleteInfo(ctx)
    if err != nil {
        log.Fatal(err)
    }
    _ = athlete

    // 更新默认 athlete
    _, err = client.UpdateAthleteInfo(ctx, intervalsicu.AthleteUpdateDTO{
        // ... fields
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

## 生成的 API 方法

所有 `openapi-spec.json` 中带 `operationId` 的路径都会生成对应方法，命名规则为驼峰形式。
示例：

- `getAthlete` -> `Client.GetAthlete`
- `updateAthlete` -> `Client.UpdateAthlete`

对应代码在 `client_gen.go` 中。

## 便捷方法

为了减少传参，一些常用方法提供了默认 `athlete_id` 的封装：

- `Client.GetAthleteInfo(ctx)`
- `Client.UpdateAthleteInfo(ctx, body)`

你可以继续按需添加更多封装在 `convenience.go`。

## 重新生成

当 `openapi-spec.json` 更新后，执行：

```bash
go run ./cmd/gen
```

会重新生成：

- `types_gen.go`
- `client_gen.go`

## 认证方式

使用 API Key（Basic Auth）：

- 用户名固定为 `API_KEY`
- 密码为你在 Intervals.icu `Settings` 中获取的 API Key

`NewClient` 会自动为每个请求设置。
